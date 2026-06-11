import { Hono } from "hono";
import { cors } from "hono/cors";
import { streamSSE } from "hono/streaming";
import { serve } from "@hono/node-server";
import { randomUUID } from "node:crypto";

import { getToken, getApiBase, deviceLogin } from "./auth";
import { curlRequest, curlStream } from "./curl";
import {
  translateRequest, translateResponse, translateStreamChunk,
  type StreamContext,
} from "./translate";
import { parseProviders, resolveModel, getAllModelIds, generateSettings, requiresToken, getCopilotProvider } from "./router";

const app = new Hono();
app.use(cors());

const PORT = parseInt(process.env.PORT || "3456");
const OUT_DIR = process.env.OUT_DIR || "./out";

const BASE_HEADERS: Record<string, string> = {
  "copilot-integration-id": "vscode-chat",
  "user-agent": "GitHubCopilotChat/0.26.7",
  "editor-version": "vscode/1.104.1",
  "editor-plugin-version": "copilot-chat/0.26.7",
};

// ---- Routes ----
app.get("/", (c) => c.text("OneCC Proxy — OK"));
app.get("/health", (c) => c.json({ status: "ok" }));

app.get("/v1/models", (c) => c.json({
  object: "list",
  data: getAllModelIds().map(id => ({ id, object: "model", created: 1, owned_by: "router" })),
}));

// ---- Copilot handler ----
async function copilotHandler(c: any, resolved: { model: string }) {
  const token = await getToken();
  const body = await c.req.json();
  body.model = resolved.model;
  const openaiReq = translateRequest(body);

  const headers = {
    ...BASE_HEADERS,
    "authorization": `Bearer ${token}`,
    "content-type": "application/json",
    "x-request-id": randomUUID(),
  };

  if (!openaiReq.stream) {
    const res = await curlRequest(`${getApiBase()}/chat/completions`, { method: "POST", headers, body: JSON.stringify(openaiReq) });
    if (res.status !== 200) return c.json({ error: { message: res.body, type: "api_error" } }, 500);
    return c.json(translateResponse(JSON.parse(res.body), body.model));
  }

  return streamSSE(c, async (stream) => {
    const ctx: StreamContext = { messageStartSent: false, messageId: `msg_${Date.now()}`, model: body.model, contentBlockIndex: 0, contentBlockOpen: false, toolCalls: {} };
    const { stream: curlOut } = curlStream(`${getApiBase()}/chat/completions`, { method: "POST", headers, body: JSON.stringify({ ...openaiReq, stream: true }) });
    let buffer = "";
    for await (const chunk of curlOut) {
      buffer += chunk;
      const lines = buffer.split("\n"); buffer = lines.pop() || "";
      for (const line of lines) {
        const t = line.trim(); if (!t || !t.startsWith("data: ")) continue;
        const d = t.slice(6); if (d === "[DONE]") break;
        try { for (const ev of translateStreamChunk(JSON.parse(d), ctx)) await stream.writeSSE({ data: JSON.stringify(ev) }); } catch { /* skip */ }
      }
    }
  });
}

// ---- External Anthropic handler (direct passthrough) ----
async function externalHandler(c: any, resolved: { provider: { baseUrl: string; apiKey: string }; model: string }) {
  const body = await c.req.json();
  body.model = resolved.model;

  const headers: Record<string, string> = { "Content-Type": "application/json", "x-api-key": resolved.provider.apiKey };
  const base = resolved.provider.baseUrl.replace(/\/$/, "");
  const stream = body.stream;

  const res = await fetch(`${base}/messages`, { method: "POST", headers, body: JSON.stringify(body) });
  if (!res.ok) return c.json({ error: { message: await res.text(), type: "api_error" } }, res.status);
  if (!stream) return c.json(await res.json());

  return streamSSE(c, async (s) => {
    const reader = res.body?.getReader(); if (!reader) return;
    const decoder = new TextDecoder(); let buf = "";
    while (true) {
      const { done, value } = await reader.read(); if (done) break;
      buf += decoder.decode(value, { stream: true });
      const lines = buf.split("\n"); buf = lines.pop() || "";
      for (const l of lines) { if (l.trim()) await s.writeSSE({ data: l.trim() }); }
    }
    if (buf.trim()) await s.writeSSE({ data: buf.trim() });
  });
}

// ---- Unified messages handler ----
async function messagesHandler(c: any) {
  let body: any;
  try { body = await c.req.json(); } catch { return c.json({ error: "invalid json" }, 400); }

  const fullModel = body.model;
  if (!fullModel) {
    const cp = getCopilotProvider();
    if (cp && cp.models.length > 0) return copilotHandler(c, { model: cp.models[0] });
    return c.json({ error: "no model specified" }, 400);
  }

  const resolved = resolveModel(fullModel);
  if (!resolved) return c.json({ error: `unknown model: ${fullModel}. Available: ${getAllModelIds().join(", ")}` }, 400);

  return resolved.provider.prefix === "cp"
    ? copilotHandler(c, resolved)
    : externalHandler(c, resolved);
}

app.post("/messages", messagesHandler);
app.post("/v1/messages", messagesHandler);

// ---- Startup ----
async function main() {
  const providers = parseProviders();
  const prefixes = providers.map(p => p.prefix).join(", ");
  console.log("Providers:", prefixes || "none");
  console.log("Models:", getAllModelIds().join(", "));

  // Start server immediately, don't block on auth
  serve({ fetch: app.fetch, port: PORT }, (info) => {
    console.log(`\n🚀 OneCC Proxy → http://localhost:${info.port}`);
    console.log(`   /v1/messages  |  /health  |  /v1/models\n`);
  });

  // Background: auto device login if needed
  if (await requiresToken()) {
    try {
      await deviceLogin();
    } catch (e: any) {
      console.log("⚠ 自动登录失败:", e.message);
      console.log("   手动获取 token:");
      console.log("   podman run --rm -it -v ./copilot-anthropic-proxy/github_token:/root/.local/share/copilot-api/github_token ghcr.io/ericc-ch/copilot-api:latest bun run auth.js\n");
    }
  }
  try { generateSettings(OUT_DIR); } catch {}
}

main();

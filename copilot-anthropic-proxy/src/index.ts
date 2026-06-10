import { Hono } from "hono";
import { cors } from "hono/cors";
import { streamSSE } from "hono/streaming";
import { readFile } from "node:fs/promises";

import { getToken, getApiBase } from "./auth";
import {
  translateRequest, translateResponse, translateStreamChunk,
  setAllowedModels, getAllowedModels, type StreamContext,
} from "./translate";

const app = new Hono();
app.use(cors());

// ---------- Config ----------
const PORT = parseInt(process.env.PORT || "4142");
const MODELS_CONF = process.env.MODELS_CONF || "./models.conf";

// Headers matching Cherry Studio's exact Copilot config
const BASE_HEADERS: Record<string, string> = {
  "Copilot-Integration-Id": "vscode-chat",
  "User-Agent": "GitHubCopilotChat/0.26.7",
  "Editor-Version": "vscode/1.104.1",
  "Editor-Plugin-Version": "copilot-chat/0.26.7",
  "editor-version": "vscode/1.104.1",
  "editor-plugin-version": "copilot-chat/0.26.7",
  "copilot-vision-request": "true",
};

// ---------- Load model list ----------
async function loadModelList() {
  try {
    const content = await readFile(MODELS_CONF, "utf-8");
    const models = content
      .split("\n")
      .map(l => l.replace(/#.*/, "").trim())
      .filter(l => l.length > 0);
    setAllowedModels(models);
    console.log("Allowed models:", models);
  } catch {
    console.log("No models.conf found");
  }
}

// ---------- Routes ----------
app.get("/", (c) => c.text("Copilot Anthropic Proxy — OK"));

app.get("/health", (c) => c.json({ status: "ok" }));

app.get("/v1/models", (c) => {
  return c.json({
    object: "list",
    data: getAllowedModels().map(id => ({
      id, object: "model", created: 1, owned_by: "copilot",
    })),
  });
});

// Shared handler for both /messages and /v1/messages
async function messagesHandler(c: any) {
  const token = await getToken();
  const body = await c.req.json();

  const openaiReq = translateRequest(body);
  const headers = {
    ...BASE_HEADERS,
    "Authorization": `Bearer ${token}`,
    "Content-Type": "application/json",
  };

  if (!openaiReq.stream) {
    // Non-streaming
    const res = await fetch(`${getApiBase()}/chat/completions`, {
      method: "POST",
      headers,
      body: JSON.stringify(openaiReq),
    });

    if (!res.ok) {
      const err = await res.text();
      return c.json({ error: { message: err, type: "api_error" } }, 500);
    }

    const data = await res.json();
    const anthropicRes = translateResponse(data, body.model);
    return c.json(anthropicRes);
  }

  // Streaming
  return streamSSE(c, async (stream) => {
    const ctx: StreamContext = {
      messageStartSent: false,
      messageId: `msg_${Date.now()}`,
      model: body.model,
      contentBlockIndex: 0,
      contentBlockOpen: false,
      toolCalls: {},
    };

    const res = await fetch(`${getApiBase()}/chat/completions`, {
      method: "POST",
      headers,
      body: JSON.stringify({ ...openaiReq, stream: true }),
    });

    if (!res.ok) {
      await stream.writeSSE({ data: JSON.stringify({ type: "error", error: await res.text() }) });
      return;
    }

    const reader = res.body?.getReader();
    if (!reader) return;

    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || !trimmed.startsWith("data: ")) continue;
        const dataStr = trimmed.slice(6);
        if (dataStr === "[DONE]") break;

        try {
          const chunk = JSON.parse(dataStr);
          const events = translateStreamChunk(chunk, ctx);
          for (const event of events) {
            await stream.writeSSE({ data: JSON.stringify(event) });
          }
        } catch { /* skip malformed */ }
      }
    }

    // Flush remaining
    if (buffer.trim() && buffer.trim().startsWith("data: ") && buffer.trim() !== "data: [DONE]") {
      try {
        const chunk = JSON.parse(buffer.trim().slice(6));
        const events = translateStreamChunk(chunk, ctx);
        for (const event of events) {
          await stream.writeSSE({ data: JSON.stringify(event) });
        }
      } catch { /* skip */ }
    }
  });
}

// Route registrations
app.post("/messages", messagesHandler);
app.post("/v1/messages", messagesHandler);

// ---------- Startup ----------
async function main() {
  await loadModelList();

  console.log(`\n🚀 Copilot Anthropic Proxy listening on http://localhost:${PORT}`);
  console.log(`   Endpoint: POST /v1/messages`);
  console.log(`   Health:   GET /health\n`);
}

main();

export default {
  port: PORT,
  fetch: app.fetch,
};

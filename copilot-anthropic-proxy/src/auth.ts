import { readFile, access, writeFile } from "node:fs/promises";
import { ProxyAgent, setGlobalDispatcher } from "undici";

// Route all fetch() through the proxy
if (process.env.https_proxy || process.env.http_proxy) {
  const uri = process.env.https_proxy || process.env.http_proxy!;
  setGlobalDispatcher(new ProxyAgent({ uri, requestTls: { rejectUnauthorized: false } }));
}

let cachedToken: string | null = null;
let cachedApiBase: string | null = null;

const COPILOT_CLIENT_ID = "Iv1.b507a08c87ecfe98";
const UA = "GitHubCopilotChat/0.26.7";

async function getGitHubToken(): Promise<string> {
  const path = process.env.GITHUB_TOKEN_FILE || process.env.GITHUB_TOKEN;
  if (path && !path.startsWith("gh")) {
    return (await readFile(path, "utf-8")).trim();
  }
  if (path) return path;
  throw new Error("No GITHUB_TOKEN available");
}

export async function getToken(): Promise<string> {
  if (cachedToken) {
    try {
      const res = await fetch(`${getApiBase()}/models`, {
        headers: { "Authorization": `Bearer ${cachedToken}` },
        signal: AbortSignal.timeout(3000),
      });
      if (res.ok) return cachedToken;
    } catch { /* expired */ }
  }

  const gh = await getGitHubToken();
  const res = await fetch("https://api.github.com/copilot_internal/v2/token", {
    headers: {
      "Authorization": `token ${gh}`,
      "Accept": "application/json",
      "User-Agent": UA,
      "editor-version": "vscode/1.104.1",
      "Copilot-Integration-Id": "vscode-chat",
      "Editor-Version": "vscode/1.104.1",
      "Editor-Plugin-Version": "copilot-chat/0.26.7",
      "copilot-vision-request": "true",
    },
  });
  if (!res.ok) throw new Error(`Token request failed: ${res.status}`);
  const data = await res.json() as { token: string; endpoints: { api: string } };
  cachedToken = data.token;
  cachedApiBase = data.endpoints.api;
  return cachedToken;
}

export function getApiBase(): string {
  return cachedApiBase || "https://api.githubcopilot.com";
}

export async function checkTokenAvailable(): Promise<boolean> {
  const path = process.env.GITHUB_TOKEN_FILE;
  if (!path) return false;
  try {
    await access(path);
    const content = await readFile(path, "utf-8").then(c => c.trim());
    return content.length > 0;
  } catch {
    return false;
  }
}

// ---- Device login ----
export async function deviceLogin(): Promise<string> {
  const tokenFile = process.env.GITHUB_TOKEN_FILE!;

  const res1 = await fetch("https://github.com/login/device/code", {
    method: "POST",
    headers: { "Accept": "application/json", "User-Agent": UA },
    body: new URLSearchParams({ client_id: COPILOT_CLIENT_ID, scope: "read:user" }),
    signal: AbortSignal.timeout(10000),
  });
  if (!res1.ok) throw new Error(`Device code request failed: ${res1.status}`);
  const dc = await res1.json() as { device_code: string; user_code: string; verification_uri: string; interval: number };

  console.log("\n🔑 请打开以下链接完成 GitHub 设备授权：");
  console.log(`   ${dc.verification_uri}`);
  console.log(`   输入验证码: ${dc.user_code}\n`);

  // Poll for completion
  const interval = (dc.interval || 5) * 1000;
  while (true) {
    await new Promise(r => setTimeout(r, interval));
    const res2 = await fetch("https://github.com/login/oauth/access_token", {
      method: "POST",
      headers: { "Accept": "application/json", "User-Agent": UA },
      body: new URLSearchParams({
        client_id: COPILOT_CLIENT_ID,
        device_code: dc.device_code,
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
      }),
    });
    const data = await res2.json() as { access_token?: string; error?: string };
    if (data.access_token) {
      await writeFile(tokenFile, data.access_token + "\n");
      console.log("✅ GitHub 授权成功\n");
      return data.access_token;
    }
    if (data.error === "authorization_pending") continue;
    if (data.error === "slow_down") { await new Promise(r => setTimeout(r, 5000)); continue; }
    throw new Error(`Device login failed: ${data.error}`);
  }
}


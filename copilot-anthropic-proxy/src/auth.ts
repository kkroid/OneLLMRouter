import { readFile, access, writeFile } from "node:fs/promises";
import { curlRequest } from "./curl";

let cachedToken: string | null = null;
let cachedExp = 0;

const COPILOT_CLIENT_ID = "Iv1.b507a08c87ecfe98";
const UA = "GitHubCopilotChat/0.26.7";
// Fixed endpoint — the individual.* subdomain hides Claude models.
const API_BASE = "https://api.githubcopilot.com";

async function getGitHubToken(): Promise<string> {
  const path = process.env.GITHUB_TOKEN_FILE || process.env.GITHUB_TOKEN;
  if (path && !path.startsWith("gh")) {
    return (await readFile(path, "utf-8")).trim();
  }
  if (path) return path;
  throw new Error("No GITHUB_TOKEN available");
}

export async function getToken(): Promise<string> {
  // Copilot token embeds exp=<unix>; refresh a bit early.
  if (cachedToken && Date.now() / 1000 < cachedExp - 120) return cachedToken;

  const gh = await getGitHubToken();
  const res = await curlRequest("https://api.github.com/copilot_internal/v2/token", {
    headers: {
      "Authorization": `token ${gh}`,
      "Accept": "application/json",
      "User-Agent": UA,
      "Editor-Version": "vscode/1.104.1",
      "Editor-Plugin-Version": "copilot-chat/0.26.7",
      "Copilot-Integration-Id": "vscode-chat",
    },
  });
  if (res.status !== 200) throw new Error(`Token request failed: ${res.status} ${res.body.slice(0, 200)}`);
  const data = JSON.parse(res.body) as { token: string; expires_at?: number };
  cachedToken = data.token;
  // token string contains exp=<unix>; prefer that, fall back to expires_at
  const m = data.token.match(/exp=(\d+)/);
  cachedExp = m ? parseInt(m[1]) : (data.expires_at || Date.now() / 1000 + 1500);
  return cachedToken;
}

export function getApiBase(): string {
  return API_BASE;
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

  const res1 = await curlRequest("https://github.com/login/device/code", {
    method: "POST",
    headers: { "Accept": "application/json", "User-Agent": UA, "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ client_id: COPILOT_CLIENT_ID, scope: "read:user" }).toString(),
  });
  if (res1.status !== 200) throw new Error(`Device code request failed: ${res1.status}`);
  const dc = JSON.parse(res1.body) as { device_code: string; user_code: string; verification_uri: string; interval: number };

  console.log("\n🔑 请打开以下链接完成 GitHub 设备授权：");
  console.log(`   ${dc.verification_uri}`);
  console.log(`   输入验证码: ${dc.user_code}\n`);

  const interval = (dc.interval || 5) * 1000;
  while (true) {
    await new Promise(r => setTimeout(r, interval));
    const res2 = await curlRequest("https://github.com/login/oauth/access_token", {
      method: "POST",
      headers: { "Accept": "application/json", "User-Agent": UA, "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        client_id: COPILOT_CLIENT_ID,
        device_code: dc.device_code,
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
      }).toString(),
    });
    const data = JSON.parse(res2.body) as { access_token?: string; error?: string };
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


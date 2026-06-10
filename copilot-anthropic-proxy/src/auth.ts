import { readFile } from "node:fs/promises";

let cachedToken: string | null = null;
let cachedApiBase: string | null = null;

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
      "User-Agent": "GitHubCopilotChat/0.26.7",
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


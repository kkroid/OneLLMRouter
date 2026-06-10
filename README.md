# OneCCRouter

基于 [9router](https://github.com/decolua/9router) 的 AI 模型路由网关，将 GitHub Copilot Claude 模型 + DeepSeek 模型统一暴露为 Anthropic-compatible API，供 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) CLI 等工具使用。

## 架构

```
Claude Code CLI
      │
      ▼
  localhost:3456  ───  9router (Provider Router)
      │
      ├── ds/deepseek-v4-pro     ──▶  api.deepseek.com/anthropic
      ├── ds/deepseek-v4-flash   ──▶  api.deepseek.com/anthropic
      ├── cp/claude-opus-4.8     ──▶  copilot-anthropic:4142  ──▶  Copilot API (via proxy 1082)
      └── cp/claude-fable-5      ──▶  copilot-anthropic:4142  ──▶  Copilot API (via proxy 1082)
```

| 组件 | 说明 |
|------|------|
| **9router** | AI 模型路由网关，统一 Anthropic-compatible 入口 (`:3456`) |
| **copilot-anthropic** | 将 Copilot OpenAI 格式翻译为 Anthropic 格式的代理 (`:4142`) |
| **register-providers** | 一次性启动脚本，自动向 9router 注册 provider 节点和连接 |

## 可用模型

| 前缀 | 模型 ID | 来源 |
|------|--------|------|
| `ds/` | `deepseek-v4-pro` | DeepSeek API |
| `ds/` | `deepseek-v4-flash` | DeepSeek API |
| `cp/` | `claude-opus-4.8` | GitHub Copilot |
| `cp/` | `claude-fable-5` | GitHub Copilot |

## 前置条件

- **Podman** 或 **Docker** + Docker Compose
- **代理 `127.0.0.1:1082`** — GitHub Copilot 对 Claude 模型有 IP 区域限制，必须通过该代理访问（Clash/V2Ray 等）
- **DeepSeek API Key** — 从 [platform.deepseek.com](https://platform.deepseek.com) 获取
- **GitHub Device Token** — 用于 Copilot API 认证（见下方步骤）

## 部署步骤

### 1. 克隆项目

```bash
git clone <repo-url>
cd OneCCRouter
```

### 2. 配置环境变量

```bash
cp .env.example .env
```

编辑 `.env` 填入真实 Key：

```env
DEEPSEEK_API_KEY=sk-xxxxxxxx
GITHUB_TOKEN=ghu_xxxxxxxx
```

### 3. 获取 GitHub Copilot Token

> **注意**：Copilot API 需要的是 **设备 OAuth Token**（`ghu_` 前缀），不是 GitHub Personal Access Token（`ghp_` 前缀）。两者是不同的认证体系，PAT 无法用于 Copilot API。

使用 copilot-api 完成设备认证（一次性）：

```bash
podman run --rm -it \
  -v ./copilot-anthropic-proxy/github_token:/root/.local/share/copilot-api/github_token \
  ghcr.io/ericc-ch/copilot-api:latest \
  bun run auth.js
```

按提示打开 GitHub 验证页面，完成后 token 会自动保存到 `copilot-anthropic-proxy/github_token`。

### 4. 启动服务

```bash
podman compose up -d
```

首次启动会自动 pull 镜像、构建 proxy、注册 provider，约 30 秒完成。

### 5. 验证

```bash
# 测试 Copilot Claude
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: x" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":50,"messages":[{"role":"user","content":"say hi"}]}'

# 测试 DeepSeek
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: x" \
  -d '{"model":"ds/deepseek-v4-pro","max_tokens":50,"messages":[{"role":"user","content":"say hi"}]}'
```

## 在 Claude Code 中使用

在 Claude Code 配置中指向 9router：

```json
{
  "apiKey": "x",
  "baseUrl": "http://localhost:3456/v1",
  "model": "cp/claude-opus-4.8"
}
```

或以 DeepSeek 模型运行：

```json
{
  "apiKey": "x",
  "baseUrl": "http://localhost:3456/v1",
  "model": "ds/deepseek-v4-pro"
}
```

## 管理

```bash
# 查看状态
podman compose ps

# 查看日志
podman compose logs -f

# 重启
podman compose restart

# 添加新模型 — 编辑 copilot-anthropic-proxy/models.conf 后重启
podman compose restart copilot-anthropic
```

9router Web 管理界面: [http://localhost:3456](http://localhost:3456)（默认密码 `123456`）

## 项目结构

```
.
├── copilot-anthropic-proxy/   # Copilot → Anthropic 格式转换代理
│   ├── src/
│   │   ├── index.ts           # Hono 服务入口
│   │   ├── auth.ts            # Copilot token 管理
│   │   ├── translate.ts       # Anthropic ↔ OpenAI 格式翻译
│   │   └── types.ts           # 类型定义
│   ├── models.conf            # 允许的模型列表
│   ├── github_token           # Copilot 设备 token（gitignore）
│   ├── Dockerfile
│   └── package.json
├── docker-compose.yml         # 容器编排
├── register-providers.sh      # 9router provider 自动注册
├── .env.example               # 环境变量模板
└── .env                       # 实际环境变量（gitignore）
```

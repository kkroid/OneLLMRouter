# OneCCRouter

单容器 AI 模型路由网关。将 GitHub Copilot Claude 模型 + 任意 Anthropic-compatible API 统一暴露为单一 Anthropic 接口，供 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 等工具使用。

**167 MB，3 秒启动，零外部依赖。**

## 架构

```
Claude Code CLI
      │
      ▼
  localhost:3456  ───  OneCC Proxy（单容器）
      │
      ├── cp/claude-*  ──▶  Copilot API（Anthropic ↔ OpenAI 翻译）
      └── ds/* | 任意  ──▶  Anthropic-compatible API（直通）
```

## 可用模型

由 `.env` 中的 `PROVIDER_<N>_*` 变量定义：

| 前缀 | 模型 ID | 说明 |
|------|--------|------|
| `cp/` | `claude-opus-4.8` | GitHub Copilot |
| `cp/` | `claude-fable-5` | GitHub Copilot |
| `ds/` | `deepseek-v4-pro` | 示例：DeepSeek |
| `ds/` | `deepseek-v4-flash` | 示例：DeepSeek |

> 添加新 provider：`.env` 中加 `PROVIDER_3_*` 等变量，重启即生效。

## 前置条件

- **Podman** 或 **Docker** + Docker Compose
- **代理 `127.0.0.1:1082`** — GitHub Copilot Claude 模型的 IP 区域限制（Clash/V2Ray）
- **各 provider 的 API Key**

## 部署步骤

### 1. 克隆 & 配置

```bash
git clone <repo-url> && cd OneCCRouter
cp .env.example .env
```

编辑 `.env`：

```env
PROVIDER_1_NAME=Copilot Claude
PROVIDER_1_PREFIX=cp
PROVIDER_1_API_KEY=not-needed
PROVIDER_1_MODELS=claude-opus-4.8,claude-fable-5

PROVIDER_2_NAME=DeepSeek
PROVIDER_2_PREFIX=ds
PROVIDER_2_BASE_URL=https://api.deepseek.com/anthropic
PROVIDER_2_API_KEY=sk-your-key
PROVIDER_2_MODELS=deepseek-v4-pro,deepseek-v4-flash
```

### 2. 启动

```bash
podman compose up
```

首次启动时若未配置 Copilot token，会自动弹出设备授权链接，打开网页输入验证码即可。之后会将 token 持久化到本地文件，后续启动无需重复授权。

### 3. 验证

```bash
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

## Claude Code 配置

将 `out/claude-code-settings.json` 复制到 Claude Code 配置目录，或直接设置：

```json
{ "apiKey": "x", "baseUrl": "http://localhost:3456/v1", "model": "cp/claude-opus-4.8" }
```

## 管理

```bash
podman compose ps           # 状态
podman compose logs -f      # 日志
podman compose up -d        # 重启（修改 .env 后）
```

## 项目结构

```
.
├── copilot-anthropic-proxy/
│   ├── src/
│   │   ├── index.ts        # 服务入口 + 路由
│   │   ├── router.ts       # Provider 注册 + 模型解析
│   │   ├── auth.ts         # Copilot token
│   │   ├── translate.ts    # Anthropic ↔ OpenAI
│   │   └── types.ts        # 类型
│   ├── models.conf         # Copilot 模型白名单
│   ├── github_token        # 设备 token（gitignore）
│   ├── Dockerfile
│   └── package.json
├── docker-compose.yml      # 单容器编排
├── out/                    # 生成的 Claude Code 配置
├── .env.example
└── .env                    # Provider 配置（gitignore）
```

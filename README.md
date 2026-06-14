# OneCCRouter

**个人 AI 模型路由网关** — 将 GitHub Copilot Claude 模型 + 任意 Anthropic-compatible API 统一暴露为单一 Anthropic 接口，供 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 等工具使用。

**7.8 MB 单文件，零运行时依赖。**

## 架构

```
Claude Code CLI / VS Code / 其他工具
              │
              ▼  Anthropic Messages API (localhost)
    ┌─────────────────────────┐
    │   onecc-router (Go)     │  ← 单二进制守护进程
    │   · HTTP proxy          │
    │   · 协议翻译             │
    └─────────────────────────┘
```

## 可用模型

由 `onecc-router.yaml` 中的 `providers` 配置定义：

| 前缀 | 模型 ID | 说明 |
|------|--------|------|
| `cp/` | `claude-opus-4.8` | GitHub Copilot |
| `cp/` | `claude-fable-5` | GitHub Copilot |
| `ds/` | `deepseek-v4-pro[1m]` | DeepSeek（示例） |
| `ds/` | `deepseek-v4-flash[1m]` | DeepSeek（示例） |

> 添加新 provider：在 yaml 的 `providers:` 下添加新条目，重启生效。

## 快速开始

### 1. 编译

```bash
git clone https://github.com/kkroid/OneCCRouter.git && cd OneCCRouter
pwsh build.ps1 -Version "1.0.0"
```

产物在 `dist/onecc-router-v1.0.0.exe`。

### 2. 配置

```bash
cp onecc-router.example.yaml onecc-router.yaml
# 编辑 onecc-router.yaml，填入你的 API Key
```

```yaml
server:
  host: "127.0.0.1"
  http_port: 3456

log:
  level: "info"
  dir: "~/.onecc/logs"
  max_age_days: 30

proxy:
  socks5: "127.0.0.1:1082"

providers:
  - name: "GitHub Copilot"
    prefix: "cp"
    api_key: "not-needed"
    models:
      - "claude-opus-4.8"
      - "claude-fable-5"

  - name: "DeepSeek"
    prefix: "ds"
    base_url: "https://api.deepseek.com/anthropic"
    api_key: "sk-your-deepseek-key"
    proxy: false           # 国内直连，不走代理
    models:
      - "deepseek-v4-pro[1m]"
      - "deepseek-v4-flash[1m]"

model_slots:
  default: "ds/deepseek-v4-pro[1m]"
  opus: "cp/claude-opus-4.8"
  sonnet: "ds/deepseek-v4-pro[1m]"
  haiku: "ds/deepseek-v4-flash[1m]"
  fable: "cp/claude-fable-5"
```

### 3. 启动

```bash
.\dist\onecc-router-v1.0.0.exe
```

启动时会打印 Claude Code 的 `settings.json`，直接复制使用。如果配置了 Copilot 但未登录，会自动弹出 GitHub 设备授权流程。Token 保存在 `~/.onecc/github_token`。

### 4. 验证

```bash
# 健康检查
curl http://localhost:3456/health

# 模型列表
curl http://localhost:3456/v1/models

# 非流式推理
curl -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 流式推理
curl -N -X POST http://localhost:3456/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"cp/claude-opus-4.8","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

## Claude Code 配置

启动时自动打印，或手动设置：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:3456",
    "ANTHROPIC_AUTH_TOKEN": "x",
    "ANTHROPIC_MODEL": "ds/deepseek-v4-pro[1m]",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "cp/claude-opus-4.8",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "ds/deepseek-v4-pro[1m]",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "ds/deepseek-v4-flash[1m]",
    "ANTHROPIC_DEFAULT_FABLE_MODEL": "cp/claude-fable-5"
  },
  "theme": "dark",
  "skipWorkflowUsageWarning": true
}
```

## CLI 命令

```bash
onecc-router                # 启动守护进程（无 token 自动引导登录）
onecc-router --daemon       # 后台运行
onecc-router status         # 检查运行状态
onecc-router install        # 注册开机自启
onecc-router uninstall      # 取消开机自启
onecc-router version        # 查看版本
```

## 项目结构

```
OneCCRouter/
├── cmd/onecc-router/main.go           # CLI 入口
├── internal/
│   ├── auth/                          # GitHub device OAuth + token 管理
│   ├── config/                        # YAML 配置加载
│   ├── log/                           # slog + 按日滚动
│   ├── proxy/                         # HTTP 代理 (Copilot + External)
│   ├── router/                        # Provider 解析 + 模型路由
│   └── translate/                     # Anthropic ↔ OpenAI 协议翻译
├── onecc-router.example.yaml          # 配置模板
├── build.ps1                          # 编译脚本
└── go.mod
```

## 配置参考

### onecc-router.yaml

```yaml
server:
  host: "127.0.0.1"
  http_port: 3456

log:
  level: "info"
  dir: "~/.onecc/logs"
  max_age_days: 30

proxy:
  socks5: "127.0.0.1:1082"

providers:
  - name: "GitHub Copilot"
    prefix: "cp"
    # cp 前缀无需 api_key (OAuth), 默认走代理
    api_key: "not-needed"
    models: ["claude-opus-4.8", "claude-fable-5"]

  - name: "DeepSeek"
    prefix: "ds"
    base_url: "https://api.deepseek.com/anthropic"
    api_key: "sk-your-key"
    proxy: false
    models: ["deepseek-v4-pro[1m]", "deepseek-v4-flash[1m]"]
```

每个 provider 可设置 `proxy`：`true` 走代理，`false` 直连，不填则继承全局设置。Copilot API 有 IP 区域限制必须走代理，国内服务如 DeepSeek 直连更快。

### onecc-router.yaml

model_slots:
  default: "ds/deepseek-v4-pro[1m]"
  opus: "cp/claude-opus-4.8"
  sonnet: "ds/deepseek-v4-pro[1m]"
  haiku: "ds/deepseek-v4-flash[1m]"
  fable: "cp/claude-fable-5"
```

## 日志

JSON 格式，按天滚动，保留 30 天，文件路径 `~/.onecc/logs/onecc-router-2026-06-12.log`：

```json
{"time":"2026-06-12T10:30:00+08:00","level":"INFO","msg":"request","request_id":"a1b2c3d4","method":"POST","path":"/v1/messages","status":200,"duration_ms":1234,"model":"cp/claude-opus-4.8","provider":"cp","stream":true,"ttfb_ms":650}
```

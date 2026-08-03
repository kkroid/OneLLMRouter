# OneLLMRouter

[English](README.en.md) | 简体中文 | [更新日志](CHANGELOG.zh-CN.md)

**个人 AI 模型路由网关** — 将可配置的 Anthropic、OpenAI Chat Completions 和 OpenAI Responses 供应商统一暴露为标准接口，供 [Claude Code](https://docs.anthropic.com/en/docs/claude-code)、Codex 等工具使用。

提供两种发布形式：无运行时依赖的 Go 便携版，以及带 Qt 系统托盘和安装程序的桌面版。

## 架构

```
Claude Code CLI     OpenAI 兼容工具
(Anthropic API)     (OpenAI API)
       │                  │
       ▼                  ▼
 /anthropic/v1/*    /openai/v1/*
       │                  │
       └──────┬───────────┘
              ▼
    ┌─────────────────────────┐
    │   onellm-router (Go)     │  ← 单二进制守护进程
    │   · HTTP proxy          │
    │   · 协议翻译             │
    │   · Anthropic ↔ OpenAI  │
    └─────────────────────────┘
```

协议翻译层采用轻量 Core IR：先将 Anthropic Messages 或 OpenAI Chat Completions 映射到内部中间表示，再输出目标协议，便于稳定处理文本、图片、工具调用和流式事件。

## API 端点

| 格式 | 端点 | Base URL |
|------|------|----------|
| **Anthropic** | `/anthropic/v1/messages` | `http://localhost:3456/anthropic` |
| **Anthropic** 模型列表 | `/anthropic/v1/models` | |
| **OpenAI** | `/openai/v1/chat/completions` | `http://localhost:3456/openai` |
| **OpenAI** 模型列表 | `/openai/v1/models` | |
| **OpenAI Responses** | `/openai/v1/responses` | `http://localhost:3456/openai/v1` |
| **Codex** 模型目录 | `/openai/models` | |
| 兼容（旧） | `/v1/messages` | `http://localhost:3456` |
| 健康检查 | `/health` | |

> **Claude Code** 的 `ANTHROPIC_BASE_URL` 设为 `http://localhost:3456/anthropic`（会自动追加 `/v1/messages`）
> **OpenAI 兼容工具** 的 base URL 设为 `http://localhost:3456/openai`（会自动追加 `/v1/chat/completions`）

## 可用模型

由 `onellm-router.yaml` 中的 `providers` 配置定义：

| 前缀 | 模型 ID | 说明 |
|------|--------|------|
| `ds/` | `deepseek-v4-pro[1m]` | DeepSeek（示例） |
| `ds/` | `deepseek-v4-flash[1m]` | DeepSeek（示例） |

> 添加新 provider：在 yaml 的 `providers:` 下添加新条目，重启生效。

## 快速开始

### 1. 编译

源码构建需要 Go 1.25+ 和 PowerShell 7。

```bash
git clone https://github.com/kkroid/OneLLMRouter.git && cd OneLLMRouter
pwsh build.ps1
```

便携版产物在 `dist/onellm-router-v1.4.0.exe`。

构建桌面安装包还需要 Qt 6.8.3（MSVC 2022 x64）、CMake、MSVC 2022 和 Inno Setup 6：

```powershell
$env:QT_ROOT = "C:\Qt\6.8.3\msvc2022_64"
pwsh .\build.ps1 -Installer
```

安装包输出到 `dist/OneLLMRouter-1.4.0-setup.exe`。安装程序按用户安装到 `%LOCALAPPDATA%\Programs\OneLLMRouter`，不会覆盖已有的 `%USERPROFILE%\.onellm\onellm-router.yaml`。桌面版提供中英文系统托盘、开机自启、状态检查和安全升级；便携版仍保持单个 Go 可执行文件。

### 2. 配置

```bash
cp onellm-router.example.yaml onellm-router.yaml
# 编辑 onellm-router.yaml，填入你的 API Key
```

```yaml
server:
  host: "127.0.0.1"
  http_port: 3456

log:
  level: "info"
  dir: "~/.onellm/logs"
  max_age_days: 30

proxy:
  socks5: "127.0.0.1:1082"

retry:
  enabled: true
  max_attempts: 15
  status_codes: [408, 409, 425, 429, 500, 502, 503, 504]
  initial_delay: 1s
  max_delay: 30s
  max_elapsed: 5m
  jitter: 0.2
  honor_retry_after: true

codex:
  overwrite_catalog: true  # 默认同时覆盖 ~/.codex/model-catalog.json
  # 按 provider/ 后的基础模型名匹配；未知模型默认 low/medium/high/xhigh
  models:
    gpt-5.5:
      default_reasoning_level: medium
      supported_reasoning_levels: [low, medium, high, xhigh]
    gpt-5.6-sol:
      default_reasoning_level: low
      supported_reasoning_levels: [low, medium, high, xhigh, max, ultra]
    gpt-5.6-terra:
      default_reasoning_level: medium
      supported_reasoning_levels: [low, medium, high, xhigh, max, ultra]
    gpt-5.6-luna:
      default_reasoning_level: medium
      supported_reasoning_levels: [low, medium, high, xhigh, max]

providers:
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
  opus: "ds/deepseek-v4-pro[1m]"
  sonnet: "ds/deepseek-v4-pro[1m]"
  haiku: "ds/deepseek-v4-flash[1m]"
  fable: "ds/deepseek-v4-flash[1m]"
```

### 3. 启动

```bash
.\dist\onellm-router-v1.4.0.exe
```

启动时会打印 Claude Code 的 `settings.json`，可直接用于配置客户端。

### 4. 验证

```bash
# 健康检查
curl http://localhost:3456/health

# 模型列表（Anthropic 格式）
curl http://localhost:3456/anthropic/v1/models

# 模型列表（OpenAI 格式）
curl http://localhost:3456/openai/v1/models

# --- Anthropic 格式 ---

# 非流式推理
curl -X POST http://localhost:3456/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 流式推理
curl -N -X POST http://localhost:3456/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'

# --- OpenAI 格式 ---

# 非流式推理
curl -X POST http://localhost:3456/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 流式推理
curl -N -X POST http://localhost:3456/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro[1m]","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'

# --- OpenAI Responses / Codex 格式 ---
# 将模型名替换为已配置的 Responses provider/model

curl -N -X POST http://localhost:3456/openai/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"c78/gpt-5.6-sol","input":"hello","stream":true}'
```

## Claude Code 配置

启动时自动打印，或手动设置（注意 `ANTHROPIC_BASE_URL` 带 `/anthropic` 路径）：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:3456/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "x",
    "ANTHROPIC_MODEL": "ds/deepseek-v4-pro[1m]",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "ds/deepseek-v4-pro[1m]",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "ds/deepseek-v4-pro[1m]",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "ds/deepseek-v4-flash[1m]",
    "ANTHROPIC_DEFAULT_FABLE_MODEL": "ds/deepseek-v4-flash[1m]"
  },
  "theme": "dark",
  "skipWorkflowUsageWarning": true
}
```

## OpenAI 兼容工具配置

对于使用 OpenAI API 格式的工具（如 Continue、Aider、Cursor 等），将 base URL 指向 `/openai` 端点：

```json
{
  "provider": "openai",
  "apiKey": "x",
  "baseUrl": "http://localhost:3456/openai",
  "model": "ds/deepseek-v4-pro[1m]"
}
```

## Codex CLI 配置

为 Codex provider 配置 OneLLMRouter 的 Responses 地址，并让 `model_catalog_json` 指向 OneLLMRouter 自动生成的目录文件。下面以 `c78/gpt-5.6-sol` 为例，实际使用时替换为目录中已有的模型：

```toml
model = "c78/gpt-5.6-sol"
model_provider = "onellm"
model_catalog_json = "C:/Users/<you>/.codex/model-catalog.json"

[model_providers.onellm]
name = "OneLLMRouter"
base_url = "http://127.0.0.1:3456/openai/v1"
wire_api = "responses"
requires_openai_auth = true
```

启动 OneLLMRouter 后会始终生成 `~/.onellm/model-catalog.json`。默认配置 `codex.overwrite_catalog: true` 还会覆盖 `~/.codex/model-catalog.json`，Codex 的 `/model` 因而可以列出 `provider/model` 形式的模型。设置为 `false` 时只更新 OneLLMRouter 自己的目录文件。

每个 Responses provider 使用 `responses_base_url`，OneLLMRouter 会在请求上游前移除模型 ID 中的 `provider/` 前缀。例如本地选择 `c78/gpt-5.6-sol`，上游收到的模型名是 `gpt-5.6-sol`。

## CLI 命令

```bash
onellm-router                # 启动守护进程
onellm-router serve          # 显式启动守护进程
onellm-router --daemon       # 后台运行
onellm-router status         # 检查运行状态
onellm-router install        # 注册开机自启
onellm-router uninstall      # 取消开机自启
onellm-router version        # 查看版本
```

### 内部桌面契约

桌面托盘通过以下只读命令加载并校验配置，输出不会包含 API key 或 provider secret：

```bash
onellm-router --config <path> config-info --json
```

JSON 固定包含 `service`、绝对 `config_path`、`host`、`http_port`、`log_dir`、`proxy_socks5`、`bell`、`onellm_catalog_path` 和 `codex_catalog_path`。`/health` 提供 `service`、`pid`、版本、端口、模型数、绝对 `config_path` 和代理地址，且不会为健康检查访问上游。桌面托盘仅在端口和配置路径都匹配时附着到已有实例。

桌面父进程使用 `onellm-router serve --tray-child --config <path>` 启动自己拥有的 core 子进程。此内部标志会保留 stdin，在收到独立的 `shutdown` 行或父进程关闭控制管道时优雅退出；它不是 `--daemon` 的通用替代。Go 便携版不包含系统托盘，桌面交互统一由 `onellm-router-tray.exe` 提供。

## 项目结构

```
OneLLMRouter/
├── cmd/onellm-router/main.go           # CLI 入口
├── internal/
│   ├── catalog/                       # 多 provider 模型发现 + Codex catalog
│   ├── config/                        # YAML 配置加载
│   ├── log/                           # slog + 按日滚动
│   ├── proxy/                         # HTTP 代理与协议适配
│   ├── router/                        # Provider 解析 + 模型路由
│   └── translate/                     # Anthropic ↔ OpenAI 协议翻译
├── desktop/                           # Qt 6 托盘、状态图标与测试
├── installer/                         # Inno Setup 安装程序
├── onellm-router.example.yaml          # 配置模板
├── build.ps1                          # 便携版与桌面版构建脚本
└── go.mod
```

## 致谢与参考

OneLLMRouter 1.3.2 的协议转换层重构参考了 [moon-bridge](https://github.com/ZhiYi-R/moon-bridge) 的 Core IR 设计思路：先将不同协议映射到内部中间表示，再由协议适配器输出目标格式。本项目没有直接复制 moon-bridge 的完整功能面，当前仍聚焦于 Anthropic Messages 与 OpenAI Chat Completions 的轻量互转。

## 配置参考

### onellm-router.yaml

```yaml
server:
  host: "127.0.0.1"
  http_port: 3456

log:
  level: "info"
  dir: "~/.onellm/logs"
  max_age_days: 30

proxy:
  socks5: "127.0.0.1:1082"

retry:
  enabled: true
  max_attempts: 15
  status_codes: [408, 409, 425, 429, 500, 502, 503, 504]
  initial_delay: 1s
  max_delay: 30s
  max_elapsed: 5m
  jitter: 0.2
  honor_retry_after: true

providers:
  - name: "DeepSeek"
    prefix: "ds"
    base_url: "https://api.deepseek.com/anthropic"
    api_key: "sk-your-key"
    proxy: false
    models: ["deepseek-v4-pro[1m]", "deepseek-v4-flash[1m]"]
```

`retry` 是全局上游重试策略，默认启用。一次模型请求最多调用上游 15 次，错误恢复预算最多 5 分钟，任意两次尝试间最多等待 30 秒。`status_codes` 严格控制需要重试的 HTTP 状态；默认重试 `408/409/425/429/500/502/503/504`，不包含 `403`。配置者可按上游实际行为增删状态码；显式设置为空列表 `[]` 时不重试任何 HTTP 状态。传输错误、超时和非流式响应体读取错误仍按统一策略重试。

每个 provider 可设置 `proxy`：`true` 走代理，`false` 直连，不填则继承全局设置。需要跨境访问的供应商通常走代理，国内服务可按网络情况直连。

### model_slots

```yaml
model_slots:
  default: "ds/deepseek-v4-pro[1m]"
  opus: "ds/deepseek-v4-pro[1m]"
  sonnet: "ds/deepseek-v4-pro[1m]"
  haiku: "ds/deepseek-v4-flash[1m]"
  fable: "ds/deepseek-v4-flash[1m]"
```

## 日志

JSON 格式，按天滚动，保留 30 天，文件路径 `~/.onellm/logs/onellm-router-2026-06-12.log`：

```json
{"time":"2026-07-31T10:30:00+08:00","level":"INFO","msg":"request","request_id":"a1b2c3d4","method":"POST","path":"/anthropic/v1/messages","status":200,"duration_ms":1234,"model":"ds/deepseek-v4-pro[1m]","provider":"ds","stream":true,"ttfb_ms":650,"upstream_attempts":3,"retry_elapsed_ms":1012,"last_upstream_status":502,"last_failure_kind":"http"}
```

每次上游失败、重试后恢复、最终失败和请求取消都会使用同一个 `request_id` 写入结构化日志。符合当前重试配置但达到次数或时间上限时记录 `upstream retry exhausted`；不符合重试配置时记录 `upstream retry skipped`。错误摘要会限制长度并屏蔽 API key、Authorization 和 Bearer credential。

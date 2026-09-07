# OneLLMRouter

[English](README.en.md) | 简体中文 | [更新日志](CHANGELOG.zh-CN.md)

**Claude Code 和 Codex 的本地多供应商入口** — 只需配置一次供应商，就能直接在熟悉的工具内切换模型，无需反复修改客户端配置。OneLLMRouter 将 Anthropic、OpenAI Chat Completions 和 OpenAI Responses 供应商统一暴露为本地标准接口。

提供两种发布形式：无运行时依赖的 Go 便携版，以及带 Qt 系统托盘和安装程序的桌面版。

## 核心体验

1. 配置一次 API 供应商和模型。
2. 让 Claude Code、Codex 或 OpenAI 兼容工具始终指向同一个本地入口。
3. 在工具自己的模型列表中直接切换供应商，OneLLMRouter 在后台处理协议、模型名、代理和可控重试。

## 架构

```
Claude Code          OpenAI 兼容工具              Codex
Anthropic Messages   Chat Completions             Responses
       │                    │                         │
       ▼                    ▼                         ▼
/anthropic/v1/messages  /openai/v1/chat/completions  /openai/v1/responses
       │                    │                         │
       └──────────────┬─────┴──────────────┬──────────┘
                      ▼
           ┌──────────────────────────────┐
           │      onellm-router (Go)      │  ← 单二进制守护进程
           │  · 路由、代理、重试、Usage    │
           │  · Messages ↔ Chat Completions│
           │  · Responses 直通             │
           └──────────────────────────────┘
                      │
                      ▼
              已配置的 Providers
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
| `ds/` | `deepseek-v4-pro-1m` | DeepSeek Anthropic（上游 `deepseek-v4-pro[1m]`） |
| `ds/` | `deepseek-v4-flash-1m` | DeepSeek Anthropic（上游 `deepseek-v4-flash[1m]`） |

> 添加新 provider：在 yaml 的 `providers:` 下添加新条目，重启生效。

Anthropic 客户端模型 ID 使用不含方括号的别名（例如 `deepseek-v4-flash-1m`），避免 Claude Code 在请求前标准化掉 `[1m]`；`upstream_model` 保留并发送上游要求的完整模型名。

## 快速开始

### 1. 编译

源码构建需要 Go 1.26+ 和 PowerShell 7。

```bash
git clone https://github.com/kkroid/OneLLMRouter.git && cd OneLLMRouter
pwsh build.ps1
```

便携版产物在 `dist/onellm-router-v1.5.2.exe`。

构建桌面安装包还需要 Qt 6.8.3（MSVC 2022 x64）、CMake、MSVC 2022 和 Inno Setup 6：

```powershell
$env:QT_ROOT = "C:\Qt\6.8.3\msvc2022_64"
pwsh .\build.ps1 -Installer
```

安装包输出到 `dist/OneLLMRouter-1.5.2-setup.exe`。安装程序按用户安装到 `%LOCALAPPDATA%\Programs\OneLLMRouter`，不会覆盖已有的 `%USERPROFILE%\.onellm\onellm-router.yaml`。首次安装后从开始菜单启动；升级运行中的托盘时由 Windows Restart Manager 恢复一次。桌面版提供中英文系统托盘、开机自启、状态检查和安全升级；发生上游重试时，托盘图标临时变为黄色并显示正在重试的模型。便携版仍保持单个 Go 可执行文件。

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
    openai_base_url: "https://api.deepseek.com"
    responses_base_url: "https://api.deepseek.com"
    api_key: "sk-your-deepseek-key"
    proxy: false           # 国内直连，不走代理
    models:
      - id: "deepseek-v4-pro-1m"
        endpoints: [anthropic]
        upstream_model: "deepseek-v4-pro[1m]"
      - id: "deepseek-v4-flash-1m"
        endpoints: [anthropic]
        upstream_model: "deepseek-v4-flash[1m]"
      - id: "deepseek-v4-pro"
        endpoints: [openai, responses]
      - id: "deepseek-v4-flash"
        endpoints: [openai, responses]

model_slots:
  default: "ds/deepseek-v4-pro-1m"
  opus: "ds/deepseek-v4-pro-1m"
  sonnet: "ds/deepseek-v4-pro-1m"
  haiku: "ds/deepseek-v4-flash-1m"
  fable: "ds/deepseek-v4-flash-1m"
```

### 3. 启动

```bash
.\dist\onellm-router-v1.5.2.exe
```

启动时会打印 Claude Code 的环境配置。也可以使用桌面 Clients 页面，或使用下面的 `client claude` 命令受控写入。

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
  -d '{"model":"ds/deepseek-v4-pro-1m","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 流式推理
curl -N -X POST http://localhost:3456/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro-1m","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'

# --- OpenAI 格式 ---

# 非流式推理
curl -X POST http://localhost:3456/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'

# 流式推理
curl -N -X POST http://localhost:3456/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"ds/deepseek-v4-pro","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hello"}]}'

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
    "ANTHROPIC_MODEL": "ds/deepseek-v4-pro-1m",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "ds/deepseek-v4-pro-1m",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "ds/deepseek-v4-pro-1m",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "ds/deepseek-v4-flash-1m",
    "ANTHROPIC_DEFAULT_FABLE_MODEL": "ds/deepseek-v4-flash-1m"
  }
}
```

桌面 Clients 页面和 Core 命令可以检查、应用和恢复 `~/.claude/settings.json`：

```bash
onellm-router client claude status --json
onellm-router client claude apply --json
onellm-router client claude restore --json
```

应用操作只合并上面七个 `env` 键，保留其他顶层字段和 `env` 字段；修改已有文件前会创建同目录的 `settings.json.bak`，恢复时精确还原该备份。`ANTHROPIC_AUTH_TOKEN` 固定为连接本地 Router 的非秘密占位值 `x`，不会写入上游 Provider API Key。主题、权限、hooks、MCP、Skill、Prompt 和其他 Claude 偏好不由 OneLLMRouter 管理。

## OpenAI 兼容工具配置

对于使用 OpenAI API 格式的工具（如 Continue、Aider、Cursor 等），将 base URL 指向 `/openai` 端点：

```json
{
  "provider": "openai",
  "apiKey": "x",
  "baseUrl": "http://localhost:3456/openai",
  "model": "ds/deepseek-v4-pro-1m"
}
```

## Codex CLI 配置

为 Codex provider 配置 OneLLMRouter 的 Responses 地址，并让 `model_catalog_json` 指向 OneLLMRouter 自动生成的目录文件。下面以 `c78/gpt-5.6-sol` 为例，实际使用时替换为目录中已有的模型：

```toml
model = "c78/gpt-5.6-sol"
model_provider = "onellm"
model_catalog_json = "C:/Users/<you>/.onellm/model-catalog.json"

[model_providers.onellm]
name = "OneLLMRouter"
base_url = "http://localhost:3456/openai/v1"
wire_api = "responses"
requires_openai_auth = true
```

启动 OneLLMRouter 后会始终生成 `~/.onellm/model-catalog.json`。默认配置 `codex.overwrite_catalog: true` 还会覆盖 `~/.codex/model-catalog.json`，Codex 的 `/model` 因而可以列出 `provider/model` 形式的模型。设置为 `false` 时只更新 OneLLMRouter 自己的目录文件。

桌面 Clients 页面和 Core 命令会只读解析 `~/.codex/config.toml`，展示配置/catalog 路径、有效模型和 Provider、同步状态、模型数及显示用来源标识 `OneLLMRouter`，并提供可复制预览和受控 catalog 同步：

```bash
onellm-router client codex status --json
onellm-router client codex preview --model c78/gpt-5.6-sol --json
onellm-router client codex catalog-apply --json
```

v1.5.1 不写入、备份或恢复 Codex `config.toml`，也不提供原始 TOML 或 catalog JSON 编辑器。`catalog-apply` 只重新生成 OneLLMRouter catalog；仅当 `codex.overwrite_catalog: true` 时才同步旧的 Codex catalog 路径。

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
onellm-router stats day      # 查看 UTC 日 Token Usage
onellm-router stats week     # 查看 ISO 周 Token Usage
onellm-router stats month    # 查看 UTC 月 Token Usage
onellm-router stats range START END  # 查看包含起止日期的 UTC 区间 Usage
```

`stats day/week/month` 支持可选时间标签，`stats range` 接受 `YYYY-MM-DD` 格式的起止日期并包含两端；所有统计命令都支持 `--json`。结果按 Provider、请求模型和上游模型分组，分别展示 input、output、cache read、cache write 和 reasoning token。上游没有返回的字段会标记为未知，不会伪装成零。一次客户端请求共享稳定的 `request_id`，每次上游尝试使用从 1 开始的 `upstream_attempt`，包括失败、重试耗尽、客户端取消和服务关闭。

### 桌面配置与 Usage

Qt 桌面提供 Providers、Clients 和 Usage 页面，模型配置与手动发现位于对应 Provider 下。Provider/模型修改会立即进入界面草稿，由 Core 校验并原子写回；旧 API Key 不会显示，保存后托盘会优雅重启其持有的 Core，附着到外部 Core 时保持只读。Clients 页面提供上述 Claude 受控合并/恢复和 Codex 只读状态/预览/catalog 同步；Usage 页面提供今天、本月和包含起止日期的自定义区间，并在选择完成后自动读取 Core 统计。v1.5.1 不提供 MCP、Skill、Prompt 管理、Auto Failover、原始客户端文件编辑器或无关偏好编辑器。

### 平台能力矩阵

| 能力 | Windows | Linux | macOS |
| --- | --- | --- | --- |
| Go Core 源码构建与测试 | 支持 | 支持 | 支持 |
| Qt 桌面源码构建与离屏测试 | 支持 | 支持 | 支持 |
| 前台运行 Core 服务 | 支持 | 支持 | 支持 |
| 便携发行产物 | Windows x64 `.exe` | 未发布 | 未发布 |
| 桌面安装包 | 按用户安装的 Inno Setup | 未交付 | 未交付 |
| 便携版 `--daemon`、`install`、`uninstall` | 支持 | 不支持 | 不支持 |
| 桌面开机自启与应用重启集成 | 支持 | 不支持 | 不支持 |

发布流水线会在三个系统上编译并测试 Go 和 Qt，但 v1.5.1 只发布 Windows x64 便携版和 Setup 安装包。Linux/macOS 当前是可从源码构建的跨平台基础，不代表完整的原生安装和生命周期体验。

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
│   ├── claudeconfig/                  # Claude Code 配置状态与受控合并
│   ├── codexconfig/                   # Codex 配置状态与 catalog 同步
│   ├── config/                        # YAML 配置加载
│   ├── log/                           # slog + 按日滚动
│   ├── proxy/                         # HTTP 代理与协议适配
│   ├── router/                        # Provider 解析 + 模型路由
│   ├── translate/                     # Anthropic ↔ OpenAI 协议翻译
│   ├── upstream/                      # 有界重试、取消与错误脱敏
│   └── usage/                         # Usage 采集、存储与统计
├── desktop/                           # Qt 6 Providers/Clients/Usage、托盘与测试
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
    openai_base_url: "https://api.deepseek.com"
    responses_base_url: "https://api.deepseek.com"
    api_key: "sk-your-key"
    proxy: false
    models:
      - id: "deepseek-v4-pro-1m"
        endpoints: [anthropic]
        upstream_model: "deepseek-v4-pro[1m]"
      - id: "deepseek-v4-flash-1m"
        endpoints: [anthropic]
        upstream_model: "deepseek-v4-flash[1m]"
      - id: "deepseek-v4-pro"
        endpoints: [openai, responses]
      - id: "deepseek-v4-flash"
        endpoints: [openai, responses]
```

`retry` 是全局上游重试策略，默认启用。一次模型请求最多调用上游 15 次，错误恢复预算最多 5 分钟，任意两次尝试间最多等待 30 秒。`status_codes` 严格控制需要重试的 HTTP 状态；默认重试 `408/409/425/429/500/502/503/504`，不包含 `403`。配置者可按上游实际行为增删状态码；显式设置为空列表 `[]` 时不重试任何 HTTP 状态。传输错误、超时和非流式响应体读取错误仍按统一策略重试。Responses 流在尚未产生输出时如果收到 `server_is_overloaded`、`slow_down` 或明确的模型容量错误，会在内部按 `503` 交给同一策略判断；已经产生输出的流不会重放。配置不允许重试或重试耗尽时，客户端收到最后一次上游原始 `200 + SSE` 容量失败，而不是内部分类使用的 503。

每个 provider 可设置 `proxy`：`true` 走代理，`false` 直连，不填则继承全局设置。需要跨境访问的供应商通常走代理，国内服务可按网络情况直连。

`models` 中的每项都必须使用对象形式，并通过 `endpoints` 明确声明适用的 `anthropic`、`openai` 或 `responses` 上游线路；可用 `upstream_model` 指定实际发送给该线路的模型名。配置了模型时以配置为准；未配置时才查询对应线路的上游模型目录。

### model_slots

```yaml
model_slots:
  default: "ds/deepseek-v4-pro-1m"
  opus: "ds/deepseek-v4-pro-1m"
  sonnet: "ds/deepseek-v4-pro-1m"
  haiku: "ds/deepseek-v4-flash-1m"
  fable: "ds/deepseek-v4-flash-1m"
```

## 日志

JSON 格式，按天滚动，保留 30 天，文件路径 `~/.onellm/logs/onellm-router-2026-06-12.log`：

```json
{"time":"2026-07-31T10:30:00+08:00","level":"INFO","msg":"request","request_id":"a1b2c3d4","method":"POST","path":"/anthropic/v1/messages","status":200,"duration_ms":1234,"model":"ds/deepseek-v4-pro-1m","provider":"ds","stream":true,"ttfb_ms":650,"upstream_attempts":3,"retry_elapsed_ms":1012,"last_upstream_status":502,"last_failure_kind":"http"}
```

每次上游失败、重试后恢复、最终失败和请求取消都会使用同一个 `request_id` 写入结构化日志。符合当前重试配置但达到次数或时间上限时记录 `upstream retry exhausted`；不符合重试配置时记录 `upstream retry skipped`。日志中的错误摘要会限制长度并屏蔽 API key、Authorization 和 Bearer credential。原生协议直通路由会向客户端返回最后一次完整的上游失败响应；传输失败、过大的错误体和协议翻译仍返回 OneLLMRouter 生成的错误。

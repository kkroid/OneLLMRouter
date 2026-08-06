# OneLLMRouter

English | [简体中文](README.md) | [Changelog](CHANGELOG.md)

OneLLMRouter is a personal AI model routing gateway. It exposes configurable Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses providers through stable local endpoints for tools such as Claude Code and Codex.

Two distributions are available:

- A portable Go executable with no runtime dependencies.
- A Windows desktop package with a Qt system tray and per-user Setup installer.

## Architecture

```text
Claude Code CLI           OpenAI-compatible tools
Anthropic API             Chat Completions / Responses
       |                            |
       v                            v
 /anthropic/v1/*             /openai/v1/*
       |                            |
       +-------------+--------------+
                     v
             onellm-router (Go)
              routing, protocol
             translation, retries
                     |
                     v
            configured providers
```

The translation layer uses a compact internal representation before emitting Anthropic or OpenAI payloads. This keeps text, images, tool calls, and streaming events consistent across protocols.

## Endpoints

| Format | Endpoint | Client base URL |
|---|---|---|
| Anthropic Messages | `/anthropic/v1/messages` | `http://localhost:3456/anthropic` |
| Anthropic models | `/anthropic/v1/models` | |
| OpenAI Chat Completions | `/openai/v1/chat/completions` | `http://localhost:3456/openai` |
| OpenAI models | `/openai/v1/models` | |
| OpenAI Responses | `/openai/v1/responses` | `http://localhost:3456/openai/v1` |
| Codex model catalog | `/openai/models` | |
| Health | `/health` | |

## Build

The portable build requires Go 1.25+ and PowerShell 7:

```powershell
git clone https://github.com/kkroid/OneLLMRouter.git
Set-Location OneLLMRouter
pwsh .\build.ps1
```

The result is `dist/onellm-router-v1.4.1.exe`.

Building the desktop Setup package also requires Qt 6.8.3 for MSVC 2022 x64, CMake, MSVC 2022, and Inno Setup 6:

```powershell
$env:QT_ROOT = "C:\Qt\6.8.3\msvc2022_64"
pwsh .\build.ps1 -Installer
```

The installer is written to `dist/OneLLMRouter-1.4.1-setup.exe`. It installs per-user under `%LOCALAPPDATA%\Programs\OneLLMRouter` and never overwrites an existing `%USERPROFILE%\.onellm\onellm-router.yaml`.

## Configuration

Copy the template and add your provider credentials:

```powershell
Copy-Item .\onellm-router.example.yaml .\onellm-router.yaml
```

Minimal example:

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
  overwrite_catalog: true
  models:
    gpt-5.6-sol:
      default_reasoning_level: low
      supported_reasoning_levels: [low, medium, high, xhigh, max, ultra]

providers:
  - name: "Example Provider"
    prefix: "example"
    base_url: "https://api.example.com/anthropic"
    responses_base_url: "https://api.example.com"
    openai_base_url: "https://api.example.com"
    api_key: "sk-your-key"
    proxy: true
    models: ["gpt-5.6-sol"]

model_slots:
  default: "example/gpt-5.6-sol"
  opus: "example/gpt-5.6-sol"
  sonnet: "example/gpt-5.6-sol"
  haiku: "example/gpt-5.6-sol"
  fable: "example/gpt-5.6-sol"
```

Each provider can expose one or more protocol-specific base URLs:

- `base_url` for Anthropic Messages.
- `openai_base_url` for OpenAI Chat Completions.
- `responses_base_url` for OpenAI Responses and Codex.

Set `proxy: true` or `false` on a provider to override the global SOCKS5 setting. If omitted, the provider inherits the global proxy configuration.

Configured provider models take precedence over upstream discovery. When `models` is omitted, OneLLMRouter queries that provider's protocol-specific model endpoint.

## Run

```powershell
.\dist\onellm-router-v1.4.1.exe
```

The service prints the Claude Code environment block at startup. The main CLI commands are:

```text
onellm-router serve          Start the router explicitly
onellm-router --daemon       Start in the background
onellm-router status         Check local status
onellm-router install        Register portable autostart
onellm-router uninstall      Remove portable autostart
onellm-router version        Print the version
```

## Claude Code

Use the local Anthropic base URL and a configured `provider/model` identifier:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:3456/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "x",
    "ANTHROPIC_MODEL": "example/gpt-5.6-sol",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "example/gpt-5.6-sol",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "example/gpt-5.6-sol",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "example/gpt-5.6-sol",
    "ANTHROPIC_DEFAULT_FABLE_MODEL": "example/gpt-5.6-sol"
  }
}
```

## Codex

Point a Codex provider at OneLLMRouter's Responses endpoint:

```toml
model = "example/gpt-5.6-sol"
model_provider = "onellm"
model_catalog_json = "C:/Users/<you>/.codex/model-catalog.json"

[model_providers.onellm]
name = "OneLLMRouter"
base_url = "http://127.0.0.1:3456/openai/v1"
wire_api = "responses"
requires_openai_auth = true
```

At startup, OneLLMRouter always writes `~/.onellm/model-catalog.json`. With the default `codex.overwrite_catalog: true`, it also replaces `~/.codex/model-catalog.json`, so Codex `/model` can list `provider/model` entries. Set the option to `false` to leave the Codex file untouched.

The local provider prefix is removed before an inference request is sent upstream. For example, selecting `example/gpt-5.6-sol` sends `gpt-5.6-sol` to the provider.

## Retry Behavior

The global retry policy applies only to model inference requests. HTTP statuses are matched strictly against `retry.status_codes`; the default list does not contain `403`. An explicit empty list disables HTTP-status retries while transport, timeout, and buffered response-body read failures remain retryable.

Retries stop at the first of these boundaries: success, `max_attempts`, `max_elapsed`, client cancellation, or service shutdown. Streaming requests may retry only before a successful upstream response header is accepted. Once a successful stream starts, OneLLMRouter never replays it because doing so could duplicate text or tool calls.

Providers may charge for failed or ambiguous attempts. OneLLMRouter cannot guarantee provider-side idempotency.

## Windows Desktop

The Qt tray displays router health, version, model count, configured port, and local SOCKS5 reachability. It chooses English or Simplified Chinese from the system locale.

The tray controls only a core process that it started itself. A matching externally started router is attached read-only, while an unrelated listener is reported as a port conflict. Stop and restart are graceful; the application does not enumerate or terminate processes by image name.

Setup upgrades preserve configuration, API keys, logs, and generated catalogs. Windows Restart Manager closes and restarts a running tray while binaries are replaced. The optional start-on-login task registers only the tray, which then owns its core child.

## Logging

Logs are JSON lines under `~/.onellm/logs`, rotate daily, and are retained for 30 days by default. Request records include a request ID, model, provider, status, duration, streaming timing, retry attempts, and the final upstream failure category.

Upstream error summaries are bounded and redact configured API keys, Authorization values, Bearer credentials, and common API-key fields.

## Project Layout

```text
cmd/onellm-router/   Go CLI and service lifecycle
internal/catalog/    Multi-provider discovery and Codex catalogs
internal/config/     YAML configuration
internal/proxy/      HTTP endpoints and protocol adapters
internal/router/     Provider and model resolution
internal/translate/  Anthropic/OpenAI translation
internal/upstream/   Retry execution and credential redaction
desktop/             Qt tray and tests
installer/           Inno Setup definition
tools/               Release and safety tests
```

The protocol translation work introduced in 1.3.2 was informed by the Core IR approach used by [moon-bridge](https://github.com/ZhiYi-R/moon-bridge). OneLLMRouter remains focused on a compact personal routing gateway rather than reproducing that project's complete feature set.

## License

Apache License 2.0. See [LICENSE](LICENSE).

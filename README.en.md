# OneLLMRouter

English | [简体中文](README.md) | [Changelog](CHANGELOG.md)

OneLLMRouter is a local multi-provider entry point for Claude Code and Codex. Configure providers once, then switch models directly inside the tools you already use without repeatedly editing client configuration. It exposes Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses providers through stable local endpoints.

Two distributions are available:

- A portable Go executable with no runtime dependencies.
- A Windows desktop package with a Qt system tray and per-user Setup installer.

## Core experience

1. Configure your API providers and models once.
2. Point Claude Code, Codex, or an OpenAI-compatible tool at one stable local endpoint.
3. Switch providers from the tool's own model list while OneLLMRouter handles protocol translation, model names, proxying, and controlled retries in the background.

## Architecture

```text
Claude Code          OpenAI-compatible tools       Codex
Anthropic Messages   Chat Completions              Responses
       |                    |                         |
       v                    v                         v
/anthropic/v1/messages  /openai/v1/chat/completions  /openai/v1/responses
       |                    |                         |
       +--------------+-----+--------------+----------+
                      v
           +------------------------------+
           |      onellm-router (Go)      |
           | routing, proxy, retry, usage |
           | Messages <-> Chat Completions|
           | Responses passthrough        |
           +------------------------------+
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

The portable build requires Go 1.26+ and PowerShell 7:

```powershell
git clone https://github.com/kkroid/OneLLMRouter.git
Set-Location OneLLMRouter
pwsh .\build.ps1
```

The result is `dist/onellm-router-v1.5.1.exe`.

Building the desktop Setup package also requires Qt 6.8.3 for MSVC 2022 x64, CMake, MSVC 2022, and Inno Setup 6:

```powershell
$env:QT_ROOT = "C:\Qt\6.8.3\msvc2022_64"
pwsh .\build.ps1 -Installer
```

The installer is written to `dist/OneLLMRouter-1.5.1-setup.exe`. It installs per-user under `%LOCALAPPDATA%\Programs\OneLLMRouter` and never overwrites an existing `%USERPROFILE%\.onellm\onellm-router.yaml`. Start it from the Start menu after a first installation; upgrades restore an already-running tray once through Windows Restart Manager.

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
    models:
      - id: "claude-model[1m]"
        endpoints: [anthropic]
      - id: "responses-model"
        endpoints: [openai, responses]

model_slots:
  default: "example/claude-model[1m]"
  opus: "example/claude-model[1m]"
  sonnet: "example/claude-model[1m]"
  haiku: "example/claude-model[1m]"
  fable: "example/claude-model[1m]"
```

Each provider can expose one or more protocol-specific base URLs:

- `base_url` for Anthropic Messages.
- `openai_base_url` for OpenAI Chat Completions.
- `responses_base_url` for OpenAI Responses and Codex.

Set `proxy: true` or `false` on a provider to override the global SOCKS5 setting. If omitted, the provider inherits the global proxy configuration.

Every `models` entry must use object form and explicitly declare its `anthropic`, `openai`, or `responses` upstream routes with `endpoints`. Use `upstream_model` when the exact upstream name differs from the client-visible ID. Configured provider models take precedence over upstream discovery. When `models` is omitted, OneLLMRouter queries that provider's protocol-specific model endpoint.

## Run

```powershell
.\dist\onellm-router-v1.5.1.exe
```

The service prints the Claude Code environment block at startup. The desktop Clients page or the `client claude` commands can apply it through a controlled merge. The main CLI commands are:

```text
onellm-router serve          Start the router explicitly
onellm-router --daemon       Start in the background
onellm-router status         Check local status
onellm-router install        Register portable autostart
onellm-router uninstall      Remove portable autostart
onellm-router version        Print the version
onellm-router stats day      Show UTC daily token usage
onellm-router stats week     Show ISO-week token usage
onellm-router stats month    Show UTC monthly token usage
onellm-router stats range START END  Show an inclusive UTC date range
```

`stats day/week/month` accept an optional period label, while `stats range` requires inclusive `YYYY-MM-DD` start and end dates; every stats command supports `--json`. Usage is grouped by provider, requested model, and upstream model, with input, output, cache-read, cache-write, and reasoning tokens kept separate. Missing upstream fields are reported as unknown rather than zero. Records use one stable request ID with a one-based upstream attempt number, including failed, exhausted, cancelled, and service-shutdown attempts.

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

The desktop Clients page and Core commands can inspect, apply, and restore `~/.claude/settings.json`:

```text
onellm-router client claude status --json
onellm-router client claude apply --json
onellm-router client claude restore --json
```

Apply merges only the seven `env` keys shown above and preserves every other top-level and `env` field. Before replacing an existing file it writes the exact previous bytes to the sibling `settings.json.bak`; restore puts those bytes back. `ANTHROPIC_AUTH_TOKEN` is always the non-secret local placeholder `x`, never an upstream provider key. OneLLMRouter does not manage themes, permissions, hooks, MCP, Skills, Prompts, or other Claude preferences.

## Codex

Point a Codex provider at OneLLMRouter's Responses endpoint:

```toml
model = "example/gpt-5.6-sol"
model_provider = "onellm"
model_catalog_json = "C:/Users/<you>/.onellm/model-catalog.json"

[model_providers.onellm]
name = "OneLLMRouter"
base_url = "http://localhost:3456/openai/v1"
wire_api = "responses"
requires_openai_auth = true
```

At startup, OneLLMRouter always writes `~/.onellm/model-catalog.json`. With the default `codex.overwrite_catalog: true`, it also replaces `~/.codex/model-catalog.json`, so Codex `/model` can list `provider/model` entries. Set the option to `false` to leave the Codex file untouched.

The desktop Clients page and Core commands parse `~/.codex/config.toml` read-only and report config/catalog paths, the effective model and provider, synchronization state, model count, and the display-only `OneLLMRouter` source tag. They also provide a copyable preview and controlled catalog synchronization:

```text
onellm-router client codex status --json
onellm-router client codex preview --model example/gpt-5.6-sol --json
onellm-router client codex catalog-apply --json
```

v1.5.1 never writes, backs up, or restores Codex `config.toml`, and it has no raw TOML or catalog JSON editor. `catalog-apply` regenerates only the OneLLMRouter catalog and writes the legacy Codex catalog path only when `codex.overwrite_catalog: true`.

The local provider prefix is removed before an inference request is sent upstream. For example, selecting `example/gpt-5.6-sol` sends `gpt-5.6-sol` to the provider.

## Retry Behavior

The global retry policy applies only to model inference requests. HTTP statuses are matched strictly against `retry.status_codes`; the default list does not contain `403`. An explicit empty list disables HTTP-status retries while transport, timeout, and buffered response-body read failures remain retryable. Before a Responses stream has produced output, known `server_is_overloaded`, `slow_down`, or model-capacity SSE failures are classified internally as `503` and passed through the same policy. A stream is never replayed after output has started. If the policy skips the retry or retries are exhausted, the client receives the final original upstream `200 + SSE` capacity failure rather than the internal 503 classification.

Retries stop at the first of these boundaries: success, `max_attempts`, `max_elapsed`, client cancellation, or service shutdown. Streaming requests normally stop retrying when a successful upstream response header is accepted; the Responses capacity preflight is the narrow exception described above. Once output has started, OneLLMRouter never replays the stream because doing so could duplicate text or tool calls.

Providers may charge for failed or ambiguous attempts. OneLLMRouter cannot guarantee provider-side idempotency.

## Windows Desktop

The Qt desktop provides Providers, Clients, and Usage pages, with model configuration and manual discovery scoped to each Provider. Provider/model edits immediately update the UI draft and use Core validation plus atomic configuration updates; existing API keys are never displayed, and a successful save gracefully restarts the Core owned by the tray. An externally managed Core remains read-only. Clients provides the controlled Claude merge/restore and read-only Codex status/preview/catalog sync described above. Usage automatically reads Core statistics for Today, This month, or an inclusive custom date range. v1.5.1 has no MCP/Skill/Prompt management, Auto Failover, raw client-file editor, or unrelated preference editor. The tray also displays router health, version, model count, configured port, and local SOCKS5 reachability. It chooses English or Simplified Chinese from the system locale.

The tray controls only a core process that it started itself. A matching externally started router is attached read-only, while an unrelated listener is reported as a port conflict. Stop and restart are graceful; the application does not enumerate or terminate processes by image name.

Setup upgrades preserve configuration, API keys, logs, and generated catalogs. Windows Restart Manager closes and restarts a running tray while binaries are replaced. The optional start-on-login task registers only the tray, which then owns its core child.

## Platform Support

| Capability | Windows | Linux | macOS |
| --- | --- | --- | --- |
| Go Core source build and test | Yes | Yes | Yes |
| Qt desktop source build and offscreen tests | Yes | Yes | Yes |
| Foreground Core service | Yes | Yes | Yes |
| Portable release artifact | Windows x64 `.exe` | Not published | Not published |
| Desktop installer/package | Inno Setup, per-user | Not shipped | Not shipped |
| Portable `--daemon`, `install`, `uninstall` | Supported | Unsupported | Unsupported |
| Desktop autostart and application restart integration | Supported | Unsupported | Unsupported |

The release workflow compiles and tests Go and Qt on all three operating systems, but v1.5.1 publishes only the Windows x64 portable executable and Setup installer. Linux and macOS support is a source-build foundation, not a complete native installation or lifecycle experience.

## Logging

Logs are JSON lines under `~/.onellm/logs`, rotate daily, and are retained for 30 days by default. Request records include a request ID, model, provider, status, duration, streaming timing, retry attempts, and the final upstream failure category.

Upstream error summaries in logs are bounded and redact configured API keys, Authorization values, Bearer credentials, and common API-key fields. Native protocol routes return the final complete upstream failure response to the client unchanged; transport failures, oversized error bodies, and protocol translation still use OneLLMRouter-generated errors.

## Project Layout

```text
cmd/onellm-router/   Go CLI and service lifecycle
internal/catalog/    Multi-provider discovery and Codex catalogs
internal/claudeconfig/ Claude Code status and controlled settings merge
internal/codexconfig/ Codex status and catalog synchronization
internal/config/     YAML configuration
internal/proxy/      HTTP endpoints and protocol adapters
internal/router/     Provider and model resolution
internal/translate/  Anthropic/OpenAI translation
internal/upstream/   Retry execution and credential redaction
internal/usage/      Usage collection, storage, and statistics
desktop/             Qt Providers/Clients/Usage, tray, and tests
installer/           Inno Setup definition
tools/               Release and safety tests
```

The protocol translation work introduced in 1.3.2 was informed by the Core IR approach used by [moon-bridge](https://github.com/ZhiYi-R/moon-bridge). OneLLMRouter remains focused on a compact personal routing gateway rather than reproducing that project's complete feature set.

## License

Apache License 2.0. See [LICENSE](LICENSE).

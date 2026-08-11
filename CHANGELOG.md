# Changelog

All notable user-facing changes to OneLLMRouter are documented here.

## [1.5.0] - 2026-08-11

### Added

- Added per-attempt Usage collection for Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses across direct, translated, streaming, and non-streaming paths. Records preserve unknown token fields and link retries through a stable request ID plus one-based upstream attempt number.
- Added `stats day`, `stats week`, and `stats month` table/JSON reports grouped by provider, requested model, and upstream model, plus Qt Providers, Clients, and Usage pages backed by Core configuration and statistics contracts. Model management is available under Providers.
- Added a Qt Clients page and Core commands for controlled Claude Code managed-key merge, one-level backup, and exact restore. Unrelated Claude preferences and upstream provider keys remain untouched.
- Added read-only Codex TOML status, copyable configuration preview, deterministic catalog synchronization, and the display-only `OneLLMRouter` source tag. v1.5.0 does not write `config.toml` or expose raw TOML/catalog editing.
- Added Windows, Linux, and macOS Go and Qt build/test gates. Release artifacts remain Windows x64 only; Linux/macOS installers, autostart, daemonization, and application restart integration are not shipped.

### Changed

- Preserved the existing same-provider retry defaults and the no-replay boundary after streaming output begins. Usage now records successful, exhausted, client-cancelled, and service-shutdown attempts without changing retry parameters.
- Kept MCP/Skill/Prompt management, Auto Failover, raw client-file editing, and unrelated preference editing outside the v1.5.0 scope.

### Fixed

- OpenAI Responses streams now retry pre-output model-capacity failures through the configured upstream retry policy, return the final original SSE failure when retries do not recover, and never replay output that has already started.
- Native Anthropic, OpenAI Chat Completions, and OpenAI Responses routes now return the final upstream HTTP error status, body, and end-to-end headers without wrapping them in a OneLLMRouter error. Transport failures and protocol translation still use router-generated errors.

## [1.4.1] - 2026-08-06

### Fixed

- OpenAI-compatible Chat Completions direct forwarding now preserves `response_format`, `thinking`, strict tool definitions, and future provider-specific fields while rewriting only the routed model name.

## [1.4.0] - 2026-08-03

### Added

- Added a Qt 6 desktop tray for Windows with English and Simplified Chinese status text, colored status icons, configuration and log shortcuts, proxy reachability, and owned-core start, stop, and restart actions.
- Added a per-user Inno Setup package with optional start-on-login, configuration preservation, app-local Qt and MSVC runtime files, and safe upgrades through Windows Restart Manager.
- Added stable `/health` identity fields plus a non-secret `config-info --json` contract for desktop discovery.
- Added one global, bounded upstream retry policy for Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses requests. HTTP retry status codes are explicitly configurable and exclude `403` by default.
- Added structured retry attempt, recovery, cancellation, skipped, and exhausted logging with credential redaction.
- Added a pinned GitHub Actions release pipeline that builds and verifies the portable executable and Windows Setup package.

### Changed

- The portable Go executable no longer embeds a native tray. Desktop process management now belongs exclusively to `onellm-router-tray.exe`.
- Model inference redirects are handled as upstream responses so retry behavior remains explicit and configuration-driven.
- Unknown Codex models now receive valid fallback instructions and reasoning presets without inheriting incompatible model messages.

### Removed

- Removed all GitHub Copilot authentication, token storage, provider behavior, UI, and configuration support. A provider prefix such as `cp` is now an ordinary user-defined prefix with no built-in meaning.

### Fixed

- Hardened tray ownership checks so an externally started router is read-only and unrelated listeners are never terminated.
- Fixed tray-child shutdown, restart cancellation, startup failure handling, port conflicts, legacy autostart migration, and running-installer upgrades.
- Fixed retry cancellation and timeout boundaries so client disconnects and service shutdown stop pending work without producing misleading upstream errors.

[1.5.0]: https://github.com/kkroid/OneLLMRouter/compare/v1.4.1...v1.5.0
[1.4.1]: https://github.com/kkroid/OneLLMRouter/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/kkroid/OneLLMRouter/compare/v1.3.2...v1.4.0

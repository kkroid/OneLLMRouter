# Changelog

All notable user-facing changes to OneLLMRouter are documented here.

## [1.4.0] - Unreleased

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

[1.4.0]: https://github.com/kkroid/OneLLMRouter/compare/v1.3.2...v1.4.0

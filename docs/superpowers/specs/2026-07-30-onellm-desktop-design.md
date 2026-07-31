# OneLLMRouter Desktop Design

## Decision

Keep OneLLMRouter and OneProxy as independent open-source products. Add a Qt desktop shell and a Windows installer to OneLLMRouter, while integrating with OneProxy through the existing SOCKS5 boundary rather than merging repositories, processes, or configuration files.

The target release is assumed to be `v1.4.0`, on Windows 10/11 x64 with Qt 6.8.3, MSVC 2022, CMake, and Inno Setup 6.

## Product Shape

Publish two OneLLMRouter distributions:

- Portable: the existing small Go executable, without a native tray.
- Desktop: a Setup executable containing the Go core, a Qt tray application, Qt runtime files, and the configuration template.

The installed runtime has two processes:

```text
onellm-router-tray.exe
        | QProcess + localhost /health
        v
onellm-router-core.exe
        | configured SOCKS5 boundary
        v
OneProxy, another proxy, or direct provider access
```

The Qt process owns presentation and process supervision. The Go process owns configuration, authentication, catalog generation, routing, translation, logging, and the HTTP server.

## Repository Boundaries

OneProxy remains a generic network proxy product. OneLLMRouter does not load `oneproxy.dll`, parse OneProxy configuration, start `sing-box`, or assume that OneProxy is installed.

OneLLMRouter reports the configured SOCKS5 address. The Qt shell checks whether that local address accepts TCP connections and displays it as the outbound proxy state. This works with OneProxy and with alternative proxy products.

A combined suite installer is explicitly deferred until external users demonstrate that installing both products together is a common workflow.

## Core Lifecycle

The Go executable gains a tray-child mode intended only for a Qt-owned child process:

```text
onellm-router-core.exe serve --tray-child --config <absolute-path>
```

In tray-child mode, the core remains a foreground child without detaching from the console. It uses the existing graceful HTTP shutdown path after either a standalone `shutdown` line or EOF on standard input, so a normal tray exit or parent-process failure does not orphan the core.

The health response gains stable machine-readable identity fields:

```json
{
  "status": "ok",
  "service": "onellm-router",
  "pid": 1234,
  "version": "1.4.0",
  "http_port": 3456,
  "models": 2,
  "config_path": "C:/Users/user/.onellm/onellm-router.yaml",
  "proxy_socks5": "127.0.0.1:1082"
}
```

The core also provides a read-only `config-info --json` command. It validates the selected YAML and prints only non-secret fields needed by the tray: resolved configuration path, host, HTTP port, log directory, SOCKS5 address, and Codex catalog paths. It never prints API keys.

## Ownership Rules

Before starting a core, the tray reads `config-info`, checks the configured port, and classifies the result:

- No listener: start a new child and mark it owned.
- Matching OneLLMRouter `/health` with the same port and configuration path: attach read-only and mark it external.
- Any other listener or incompatible health payload: report a port conflict and do not start anything.

The tray may send `shutdown` or restart only a `QProcess` it started in the current session. It must never terminate by image name, enumerate and kill matching processes, or stop an externally attached instance. If graceful shutdown times out, it reports the timeout and leaves the process running.

## Tray Experience

The first desktop release includes:

- Green, yellow, and red rounded-square OneLLMRouter icons with the white hexagon glyph.
- Router state, version, port, and model count.
- Configured SOCKS5 address and local reachability.
- Start, graceful stop, and restart for an owned core.
- Read-only attached status for an existing core.
- Open configuration and log directory actions.
- Start-on-login toggle for the Qt tray.
- State-change notifications without repeated notification spam.
- English and Simplified Chinese strings selected from the system locale.

The first release does not include a full settings editor, model editor, log viewer, web dashboard, automatic provider failover, or OneProxy lifecycle controls.

When adopting the desktop release, the installer removes the portable
`OneLLMRouter` startup value. A directly launched tray also migrates that
legacy opt-in to the dedicated `OneLLMRouter Desktop` value. Disabling
start-on-login removes both values so the portable core and desktop tray
cannot compete for the configured port after sign-in.

## Installation

Install per-user to `%LOCALAPPDATA%\Programs\OneLLMRouter` without administrator privileges. Store mutable state in `%USERPROFILE%\.onellm`.
The desktop package carries the x64 MSVC runtime DLLs app-local, so a clean
Windows installation does not need a separate VC++ Redistributable install.

Setup copies `onellm-router.example.yaml` to `%USERPROFILE%\.onellm\onellm-router.yaml` only when the destination does not exist. Upgrades and uninstall never remove or overwrite user configuration, API keys, logs, or generated catalogs.

The optional start-on-login task registers only `onellm-router-tray.exe`, with an explicit absolute `--config` argument. The tray then owns the core it starts. Existing CLI-based auto-start remains supported for portable users, but a tray that discovers that instance attaches read-only.

The installer never kills a running process. If an installed executable is locked during upgrade, Setup stops and asks the user to close that installation manually.

## Development Safety

The production instance and its configured port are protected throughout implementation:

- Do not run `taskkill`, `Stop-Process`, process-name termination, or equivalent commands.
- Do not call the current `install` or `uninstall` commands during development tests.
- Unit tests use `httptest` or dependency fakes.
- Process integration tests create a temporary directory and reserve a dynamic loopback port.
- Every spawned process is recorded by its process object and PID; cleanup targets only that object.
- A test fails before startup if its chosen address matches the production configuration.
- Installer end-to-end tests run on a clean CI runner or Windows Sandbox, never over the active local installation.

## Release Quality

The release workflow builds the Go core, Qt tray, Qt deployment tree, and Inno Setup package from source. It must not rely on committed executables, DLLs, Qt plugins, or generated build directories.

Release verification covers Go unit and race tests, Qt unit tests, an isolated-port core/tray smoke test, silent install/uninstall on a clean runner, upgrade preservation of user configuration, portable version output, build metadata, and SHA256 hashes for both artifacts.

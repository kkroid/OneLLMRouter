# OneLLMRouter Desktop Installer and Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce reproducible portable and per-user Windows Setup artifacts for OneLLMRouter `v1.4.0`, with optional tray auto-start and no dependency on committed binaries or a bundled OneProxy.

**Architecture:** Extend the PowerShell build to compile Go and Qt from source, deploy Qt with `windeployqt`, and invoke Inno Setup. Install immutable files under Local AppData and mutable configuration under `~/.onellm`; build and installer validation run only on isolated ports or disposable CI runners.

**Tech Stack:** PowerShell 7, Go 1.25+, CMake, MSVC 2022, Qt 6.8.3, `windeployqt`, Inno Setup 6, GitHub Actions Windows runner.

---

## Safety Gate

The build script and installer must contain no `Get-Process | Stop-Process`, `taskkill`, process-name enumeration, or automatic shutdown of an existing router. Local installer execution is deferred until it can run in Windows Sandbox; CI uses a fresh hosted runner.

### Task 1: Reproducible Desktop Build

**Files:**
- Modify: `build.ps1`
- Modify: `desktop/CMakeLists.txt`
- Modify: `.gitignore`

- [ ] **Step 1: Define build parameters without machine-specific constants**

Use this public parameter contract:

```powershell
param(
    [switch]$Clean,
    [switch]$TestOnly,
    [switch]$Desktop,
    [switch]$Installer,
    [string]$Version = "1.4.0",
    [string]$QtRoot = $env:QT_ROOT,
    [string]$CMake = "cmake",
    [string]$InnoSetup = "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
)
```

`-Installer` implies `-Desktop`. Validate required tools with `Get-Command` or `Test-Path` and report the missing prerequisite. Do not silently fall back to paths from OneProxy.

- [ ] **Step 2: Build distinct portable and installed-core artifacts**

```powershell
$portable = Join-Path $OutDir "onellm-router-v$Version.exe"
$stage = Join-Path $PSScriptRoot "desktop\stage"
$core = Join-Path $stage "onellm-router-core.exe"

if (Test-Path -LiteralPath $stage) {
    $resolvedStage = (Resolve-Path -LiteralPath $stage).Path
    $expectedStage = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "desktop\stage"))
    if ($resolvedStage -ne $expectedStage) { throw "refusing to clean unexpected stage path: $resolvedStage" }
    Remove-Item -LiteralPath $resolvedStage -Recurse -Force
}
New-Item -ItemType Directory -Path $stage -Force | Out-Null

go build -trimpath -ldflags="-s -w -X main.version=$Version" -o $portable ./cmd/onellm-router/
if ($LASTEXITCODE -ne 0) { throw "portable core build failed" }
Copy-Item -LiteralPath $portable -Destination $core -Force
```

The stage directory is generated and ignored. The portable executable remains independently usable.

- [ ] **Step 3: Build and deploy Qt from source**

```powershell
& $CMake -S desktop -B desktop/build -DCMAKE_PREFIX_PATH="$QtRoot" -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=ON -DONELLM_VERSION="$Version"
if ($LASTEXITCODE -ne 0) { throw "Qt configure failed" }
& $CMake --build desktop/build --config Release
if ($LASTEXITCODE -ne 0) { throw "Qt build failed" }
ctest --test-dir desktop/build -C Release --output-on-failure
if ($LASTEXITCODE -ne 0) { throw "Qt tests failed" }
Copy-Item desktop/build/onellm-router-tray.exe $stage -Force
& "$QtRoot\bin\windeployqt.exe" "$stage\onellm-router-tray.exe" --release --no-translations
if ($LASTEXITCODE -ne 0) { throw "Qt deployment failed" }
```

Adjust the executable source path for multi-config MSVC generators by resolving `Release/onellm-router-tray.exe` first and the single-config path second.

- [ ] **Step 4: Add artifact completeness checks**

Require `onellm-router-core.exe`, `onellm-router-tray.exe`, `Qt6Core.dll`, `Qt6Gui.dll`, `Qt6Widgets.dll`, `Qt6Network.dll`, and `platforms/qwindows.dll`. Fail the build if any is absent.

- [ ] **Step 5: Verify build-only behavior and commit**

```powershell
pwsh -NoProfile -File build.ps1 -Version 1.4.0 -Desktop -QtRoot $env:QT_ROOT
git status --short
git add build.ps1 desktop/CMakeLists.txt .gitignore
git commit -m "build: produce portable and Qt desktop artifacts"
```

Expected: generated build/stage files are ignored, no process is launched, and no active port is contacted.

### Task 2: Per-User Inno Setup Package

**Files:**
- Create: `installer/onellm-router.iss`
- Modify: `build.ps1`

- [ ] **Step 1: Define a stable per-user application identity**

```iss
#ifndef AppVersion
  #define AppVersion "1.4.0"
#endif
#define AppName "OneLLMRouter"
#define AppExeName "onellm-router-tray.exe"

[Setup]
AppId={{B4D79E0E-89A7-4F6A-BE3B-9C04E17D6D32}
AppName={#AppName}
AppVersion={#AppVersion}
DefaultDirName={localappdata}\Programs\OneLLMRouter
DefaultGroupName=OneLLMRouter
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=OneLLMRouter-{#AppVersion}-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=no
RestartApplications=no
```

Use this AppId for all future desktop upgrades. Do not reuse OneProxy's AppId.

- [ ] **Step 2: Package source-built stage files and preserve user data**

```iss
[Files]
Source: "..\desktop\stage\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\onellm-router.example.yaml"; DestDir: "{%USERPROFILE}\.onellm"; DestName: "onellm-router.yaml"; Flags: onlyifdoesntexist uninsneveruninstall

[Dirs]
Name: "{%USERPROFILE}\.onellm"
Name: "{%USERPROFILE}\.onellm\logs"

[Icons]
Name: "{group}\OneLLMRouter"; Filename: "{app}\onellm-router-tray.exe"; Parameters: "--config ""{%USERPROFILE}\.onellm\onellm-router.yaml"""
Name: "{group}\Uninstall OneLLMRouter"; Filename: "{uninstallexe}"
```

Do not add any `[UninstallDelete]` rule for `.onellm`, Codex catalog files, logs, or configuration.

- [ ] **Step 3: Add optional start-on-login owned by Setup**

```iss
[Tasks]
Name: "autostart"; Description: "Start OneLLMRouter when I sign in"; GroupDescription: "Startup:"; Flags: checkedonce

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "OneLLMRouter Desktop"; ValueData: """{app}\onellm-router-tray.exe"" --config ""{%USERPROFILE}\.onellm\onellm-router.yaml"""; Tasks: autostart; Flags: uninsdeletevalue

[Run]
Filename: "{app}\onellm-router-tray.exe"; Parameters: "--config ""{%USERPROFILE}\.onellm\onellm-router.yaml"""; Description: "Launch OneLLMRouter"; Flags: nowait postinstall skipifsilent
```

The installer must not delete or rewrite the legacy `OneLLMRouter` CLI Run value. If both start, PID locking and read-only attachment prevent duplicate service ownership; migration can be offered later with explicit user consent.

- [ ] **Step 4: Compile through the build script**

```powershell
& $InnoSetup "/DAppVersion=$Version" (Join-Path $PSScriptRoot "installer\onellm-router.iss")
if ($LASTEXITCODE -ne 0) { throw "installer build failed" }
```

Assert that `dist/OneLLMRouter-1.4.0-setup.exe` exists and is non-empty.

- [ ] **Step 5: Compile without installing and commit**

```powershell
pwsh -NoProfile -File build.ps1 -Version 1.4.0 -Installer -QtRoot $env:QT_ROOT
git add installer/onellm-router.iss build.ps1
git commit -m "feat: add per-user Windows setup"
```

Expected: Setup is produced but not executed on the development workstation.

### Task 3: Isolated Core Smoke Test

**Files:**
- Create: `tools/desktop-smoke.ps1`

- [ ] **Step 1: Reserve a dynamic loopback port and reject protected ports**

```powershell
param(
    [Parameter(Mandatory=$true)][string]$Tray,
    [int[]]$ProtectedPorts = @(3456, 3457)
)

$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$listener.Start()
$port = ([Net.IPEndPoint]$listener.LocalEndpoint).Port
$listener.Stop()
if ($ProtectedPorts -contains $port) {
    throw "dynamic test port $port is protected"
}
```

- [ ] **Step 2: Generate an isolated configuration**

Create a temporary directory with `New-Item` and write a YAML containing one configured test Responses model, `codex.overwrite_catalog: false`, a log directory below the temp root, no SOCKS5 proxy, and the dynamic port. Set the child process `USERPROFILE` to a temp home so catalog generation cannot write to the real `~/.onellm` or `~/.codex`.

The test provider is deliberately non-routable but requires no startup discovery because its model is configured:

```yaml
server:
  host: "127.0.0.1"
  http_port: TEST_PORT
log:
  level: "info"
  dir: "TEMP_LOG_DIR"
  max_age_days: 1
proxy:
  socks5: ""
codex:
  overwrite_catalog: false
providers:
  - name: "Desktop Smoke Test"
    prefix: "smoke"
    responses_base_url: "http://127.0.0.1:9"
    api_key: "test-only"
    proxy: false
    models: ["gpt-5.6-sol"]
```

- [ ] **Step 3: Start only the recorded tray object**

Use `System.Diagnostics.ProcessStartInfo` with `UseShellExecute = false` and arguments `--config <temp-config> --smoke-test <temp-result-json>`. Set `USERPROFILE` on that process to the temporary home, store the returned tray process object, and never search for it by name. The tray must locate `onellm-router-core.exe` beside itself and start it through the production `RouterProcess` path.

- [ ] **Step 4: Verify joint tray/core identity and graceful shutdown**

Poll only `http://127.0.0.1:$port/health` while the smoke process is active. Assert `service == onellm-router` and `http_port == $port`. Wait up to 45 seconds for the tray to exit zero, then parse the result JSON and assert `ownership == owned`, `healthy == true`, and its PID matches the health payload observed on the dynamic port. Finally verify that the dynamic port no longer accepts connections, proving the tray requested graceful core shutdown.

If the smoke test times out, query only the dynamic test port. A fallback cleanup may terminate the exact tray process object created by the script and the exact core PID returned by that dynamic port after verifying its `service` identity. Never query a protected port and never call `taskkill` or `Stop-Process`.

- [ ] **Step 5: Run and commit the smoke test**

```powershell
pwsh -NoProfile -File tools/desktop-smoke.ps1 -Tray .\desktop\stage\onellm-router-tray.exe -ProtectedPorts 3456,3457
git add tools/desktop-smoke.ps1
git commit -m "test: add isolated desktop tray smoke test"
```

Expected: the tray starts and gracefully stops its temporary core; the existing instance remains untouched throughout.

### Task 4: Source-Reproducible GitHub Release Workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Define pinned build prerequisites**

Create a `windows-2022` job that checks out source, installs Go 1.25.x, Qt 6.8.3 for `win64_msvc2022_64` with `jurplel/install-qt-action@v4`, and Inno Setup 6.

- [ ] **Step 2: Run all quality gates before packaging**

```yaml
- name: Test Go
  shell: pwsh
  run: |
    go test -count=1 ./...
    go test -race -count=1 ./...
    go vet ./...
    go mod verify

- name: Build desktop and installer
  shell: pwsh
  run: |
    $qtRoot = [IO.Path]::GetFullPath((Join-Path $env:Qt6_DIR "..\..\.."))
    pwsh -NoProfile -File build.ps1 -Version $env:RELEASE_VERSION -Installer -QtRoot $qtRoot
```

Derive `RELEASE_VERSION` by removing the leading `v` from `github.ref_name`; reject tags that do not match `v<major>.<minor>.<patch>`.

- [ ] **Step 3: Run isolated smoke and Setup preservation tests**

Run `tools/desktop-smoke.ps1` against `desktop/stage/onellm-router-tray.exe`. On the disposable runner, silently install Setup with autostart and post-install launch disabled, replace the generated user configuration with a marker, run the installer again, and verify the marker is unchanged. Silently uninstall and verify `%USERPROFILE%\.onellm\onellm-router.yaml` still exists.

- [ ] **Step 4: Publish checksums and both artifacts**

```powershell
Get-FileHash -Algorithm SHA256 dist\onellm-router-v$env:RELEASE_VERSION.exe, dist\OneLLMRouter-$env:RELEASE_VERSION-setup.exe |
    ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } |
    Set-Content -Encoding ascii dist\SHA256SUMS.txt
```

Upload the portable executable, Setup executable, and checksum file as workflow artifacts. Attach them to a GitHub Release only for a pushed version tag.

- [ ] **Step 5: Validate workflow syntax and commit**

Run `git diff --check`, inspect the workflow paths against actual build outputs, then:

```powershell
git add .github/workflows/release.yml
git commit -m "ci: build reproducible desktop releases"
```

### Task 5: Final Release Acceptance

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Document both distributions and OneProxy boundary**

Document portable and Setup installation separately. Explain that OneProxy is recommended for the maintainer's deployment but is not bundled or required; any reachable SOCKS5 proxy can satisfy `proxy.socks5`.

- [ ] **Step 2: Run the complete fresh verification**

```powershell
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go mod verify
cmake --build desktop/build --config Release
ctest --test-dir desktop/build -C Release --output-on-failure
pwsh -NoProfile -File tools/desktop-smoke.ps1 -Tray .\desktop\stage\onellm-router-tray.exe -ProtectedPorts 3456,3457
git diff --check
```

Expected: every command exits zero and the production instance remains healthy on its original port.

- [ ] **Step 3: Verify final artifacts without executing Setup locally**

```powershell
.\dist\onellm-router-v1.4.0.exe version
go version -m .\dist\onellm-router-v1.4.0.exe
Get-FileHash -Algorithm SHA256 .\dist\onellm-router-v1.4.0.exe, .\dist\OneLLMRouter-1.4.0-setup.exe
git status --short --branch
```

Expected: the portable version is `1.4.0`, VCS metadata is clean for a release build, both hashes are recorded, and only intentional source/document changes remain.

- [ ] **Step 4: Commit release documentation**

```powershell
git add README.md CLAUDE.md
git commit -m "docs: document OneLLMRouter desktop setup"
```

Do not tag, push, install, or replace the running production binary until the user explicitly approves release deployment.

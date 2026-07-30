# OneLLMRouter Desktop Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give a future Qt tray a read-only discovery surface and a safe, owned-child lifecycle without changing or stopping the currently running OneLLMRouter instance.

**Architecture:** Keep the existing Go executable and HTTP server intact. Add secret-free configuration inspection, stable health identity, and an opt-in `--tray-child` standard-input control path; retain the native tray until the Qt replacement passes its own acceptance tests.

**Tech Stack:** Go 1.25+, Cobra, `net/http`, `encoding/json`, existing configuration and catalog packages.

---

## Safety Gate

Do not execute `onellm-router install`, `onellm-router uninstall`, `taskkill`, `Stop-Process`, or any process-name cleanup during this plan. All commands below are unit-test or build commands and do not bind the production port.

### Task 1: Secret-Free Configuration Inspection

**Files:**
- Create: `cmd/onellm-router/config_info.go`
- Create: `cmd/onellm-router/config_info_test.go`
- Modify: `cmd/onellm-router/main.go`

- [ ] **Step 1: Write a failing pure-data test**

Add a test that constructs a `config.Config` with a fake API key and verifies the JSON includes runtime paths and network settings but excludes the key:

```go
func TestBuildConfigInfoOmitsSecrets(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", HTTPPort: 45678},
		Log:    config.LogConfig{Dir: `C:\tmp\onellm-test\logs`},
		Proxy:  config.ProxyConfig{Socks5: "127.0.0.1:1082"},
		Providers: []config.ProviderConfig{{
			Prefix: "test", APIKey: "must-not-appear", Models: []string{"model"},
		}},
	}

	got := buildConfigInfo(cfg, `C:\tmp\onellm-test\config.yaml`, `C:\tmp\home`)
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("must-not-appear")) {
		t.Fatalf("config info leaked API key: %s", data)
	}
	if got.HTTPPort != 45678 || got.ProxySOCKS5 != "127.0.0.1:1082" {
		t.Fatalf("unexpected config info: %+v", got)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```powershell
go test -count=1 ./cmd/onellm-router -run TestBuildConfigInfoOmitsSecrets
```

Expected: compilation fails because `buildConfigInfo` is undefined.

- [ ] **Step 3: Add the data type and pure builder**

Implement only non-secret fields:

```go
type configInfo struct {
	Service           string `json:"service"`
	ConfigPath        string `json:"config_path"`
	Host              string `json:"host"`
	HTTPPort          int    `json:"http_port"`
	LogDir            string `json:"log_dir"`
	ProxySOCKS5       string `json:"proxy_socks5"`
	Bell              bool   `json:"bell"`
	OneLLMCatalogPath string `json:"onellm_catalog_path"`
	CodexCatalogPath  string `json:"codex_catalog_path"`
}

func buildConfigInfo(cfg *config.Config, path, home string) configInfo {
	bell := cfg.Server.Bell == nil || *cfg.Server.Bell
	options := codexCatalogOptions(home, cfg.Codex.OverwriteCatalog)
	return configInfo{
		Service: "onellm-router", ConfigPath: path,
		Host: cfg.Server.Host, HTTPPort: cfg.Server.HTTPPort,
		LogDir: cfg.Log.Dir, ProxySOCKS5: cfg.Proxy.Socks5, Bell: bell,
		OneLLMCatalogPath: options.OneLLMPath, CodexCatalogPath: options.CodexPath,
	}
}
```

- [ ] **Step 4: Add the read-only Cobra command**

Register `config-info` on the root command and make `--json` required for the machine-readable contract:

```go
func configInfoCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "config-info", Short: "Validate config and print non-secret runtime settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !asJSON {
				return fmt.Errorf("config-info currently requires --json")
			}
			path, err := filepath.Abs(configPath())
			if err != nil { return fmt.Errorf("resolve config path: %w", err) }
			cfg, err := config.Load(path)
			if err != nil { return fmt.Errorf("load config: %w", err) }
			if err := cfg.Validate(); err != nil { return fmt.Errorf("validate config: %w", err) }
			home, err := os.UserHomeDir()
			if err != nil { return fmt.Errorf("resolve user home: %w", err) }
			return json.NewEncoder(cmd.OutOrStdout()).Encode(buildConfigInfo(cfg, path, home))
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
```

- [ ] **Step 5: Run tests and commit**

Run `gofmt` on the two files, then:

```powershell
go test -count=1 ./cmd/onellm-router -run 'TestBuildConfigInfo|TestConfigInfo'
git add cmd/onellm-router/config_info.go cmd/onellm-router/config_info_test.go cmd/onellm-router/main.go
git commit -m "feat: expose safe desktop config metadata"
```

Expected: tests pass and the command output contains no provider credentials.

### Task 2: Stable Health Identity

**Files:**
- Create: `cmd/onellm-router/health.go`
- Create: `cmd/onellm-router/health_test.go`
- Modify: `cmd/onellm-router/main.go`

- [ ] **Step 1: Write the health contract test**

Use `httptest` and assert the stable identity required by the tray:

```go
func TestHealthPayloadIdentifiesRouter(t *testing.T) {
	payload := buildHealthPayload("1.4.0", 1234, 3456, 7, true, "127.0.0.1:1082")
	if payload.Service != "onellm-router" || payload.Status != "ok" {
		t.Fatalf("identity = %+v", payload)
	}
	if payload.PID != 1234 || payload.Models != 7 || payload.ProxySOCKS5 != "127.0.0.1:1082" {
		t.Fatalf("runtime fields = %+v", payload)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run `go test -count=1 ./cmd/onellm-router -run TestHealthPayloadIdentifiesRouter`.

Expected: compilation fails because `buildHealthPayload` is undefined.

- [ ] **Step 3: Add the explicit response type**

```go
type healthPayload struct {
	Status        string `json:"status"`
	Service       string `json:"service"`
	PID           int    `json:"pid"`
	Version       string `json:"version"`
	HTTPPort      int    `json:"http_port"`
	Models        int    `json:"models"`
	CopilotToken  bool   `json:"copilot_token"`
	ProxySOCKS5   string `json:"proxy_socks5,omitempty"`
}

func buildHealthPayload(version string, pid, port, models int, token bool, proxyAddress string) healthPayload {
	return healthPayload{
		Status: "ok", Service: "onellm-router", PID: pid, Version: version,
		HTTPPort: port, Models: models, CopilotToken: token, ProxySOCKS5: proxyAddress,
	}
}
```

Replace the untyped `/health` map with this payload, passing `os.Getpid()` and `cfg.Proxy.Socks5`. Do not make outbound requests from `/health`.

- [ ] **Step 4: Verify compatibility and commit**

Run:

```powershell
gofmt -w cmd/onellm-router/health.go cmd/onellm-router/health_test.go cmd/onellm-router/main.go
go test -count=1 ./cmd/onellm-router
git add cmd/onellm-router/health.go cmd/onellm-router/health_test.go cmd/onellm-router/main.go
git commit -m "feat: add stable router health identity"
```

Expected: existing `status` and tray health fields remain present and all command tests pass.

### Task 3: Owned Tray-Child Control

**Files:**
- Create: `cmd/onellm-router/tray_child.go`
- Create: `cmd/onellm-router/tray_child_test.go`
- Modify: `cmd/onellm-router/main.go`

- [ ] **Step 1: Write shutdown and EOF tests**

```go
func TestWatchTrayControlStopsOnlyForShutdown(t *testing.T) {
	called := 0
	watchTrayControl(strings.NewReader("ignored\nshutdown\n"), func() { called++ })
	if called != 1 { t.Fatalf("stop calls = %d, want 1", called) }
}

func TestWatchTrayControlIgnoresEOF(t *testing.T) {
	called := 0
	watchTrayControl(strings.NewReader(""), func() { called++ })
	if called != 0 { t.Fatalf("stop called on EOF") }
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run `go test -count=1 ./cmd/onellm-router -run TestWatchTrayControl`.

Expected: compilation fails because `watchTrayControl` is undefined.

- [ ] **Step 3: Implement the narrow standard-input protocol**

```go
func watchTrayControl(input io.Reader, stop func()) {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		if strings.EqualFold(strings.TrimSpace(scanner.Text()), "shutdown") {
			stop()
			return
		}
	}
}
```

Add a persistent `--tray-child` flag. When true, do not detach and do not start the native tray; instead start `go watchTrayControl(os.Stdin, stop)` after `stop` is initialized. Default behavior remains unchanged in this phase.

Register `serveCmd()` as an explicit root subcommand while retaining the root command's existing default serve behavior:

```go
rootCmd.AddCommand(serveCmd())
```

Add a command test proving both `onellm-router --tray-child --config <path>` and `onellm-router serve --tray-child --config <path>` resolve to the same serve handler. The Qt tray uses the explicit `serve` form.

- [ ] **Step 4: Test flag behavior without launching a server**

Extract this decision and test it as pure logic:

```go
func shouldStartNativeTray(trayChild bool) bool { return !trayChild }

func TestTrayChildSuppressesNativeTray(t *testing.T) {
	if shouldStartNativeTray(true) { t.Fatal("tray child started native tray") }
}
```

- [ ] **Step 5: Verify and commit**

```powershell
gofmt -w cmd/onellm-router/tray_child.go cmd/onellm-router/tray_child_test.go cmd/onellm-router/main.go
go test -count=1 ./cmd/onellm-router
git add cmd/onellm-router/tray_child.go cmd/onellm-router/tray_child_test.go cmd/onellm-router/main.go
git commit -m "feat: add safe tray-owned child mode"
```

Expected: unit tests pass without binding any TCP port or touching any running process.

### Task 4: Core Compatibility Verification

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Document the internal desktop contract**

Add `config-info --json` and `--tray-child` to the development documentation. Mark `--tray-child` as an internal desktop integration flag, not a general replacement for `--daemon`.

- [ ] **Step 2: Correct the remaining Go version inconsistency**

Change the `CLAUDE.md` technical-stack row from `Go 1.22+` to `Go 1.25+` so it agrees with `go.mod` and the later technical convention.

- [ ] **Step 3: Run the complete non-invasive quality gate**

```powershell
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go mod verify
git diff --check
pwsh -NoProfile -File build.ps1 -Version 1.4.0
```

Expected: every command exits zero. Do not launch the built executable on the production configuration or port.

- [ ] **Step 4: Inspect the artifact and commit docs**

```powershell
.\dist\onellm-router-v1.4.0.exe version
go version -m .\dist\onellm-router-v1.4.0.exe
git add README.md CLAUDE.md
git commit -m "docs: describe desktop core integration"
```

Expected: version output is `1.4.0`; build metadata names the current revision. The artifact remains local until all three desktop plans are complete.

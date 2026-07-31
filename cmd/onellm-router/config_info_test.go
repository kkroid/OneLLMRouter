package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestBuildConfigInfoOmitsSecrets(t *testing.T) {
	bell := false
	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", HTTPPort: 45678, Bell: &bell},
		Log:    config.LogConfig{Dir: `C:\tmp\onellm-test\logs`},
		Proxy:  config.ProxyConfig{Socks5: "127.0.0.1:1082"},
		Providers: []config.ProviderConfig{{
			Prefix: "test", APIKey: "must-not-appear", Models: []string{"model"},
		}},
	}

	configPath := `C:\tmp\onellm-test\config.yaml`
	home := `C:\tmp\home`
	got := buildConfigInfo(cfg, configPath, home)
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("must-not-appear")) {
		t.Fatalf("config info leaked API key: %s", data)
	}
	if got.Service != "onellm-router" || got.ConfigPath != configPath {
		t.Fatalf("unexpected identity: %+v", got)
	}
	if got.Host != "127.0.0.1" || got.HTTPPort != 45678 ||
		got.LogDir != `C:\tmp\onellm-test\logs` ||
		got.ProxySOCKS5 != "127.0.0.1:1082" || got.Bell {
		t.Fatalf("unexpected runtime settings: %+v", got)
	}
	if got.OneLLMCatalogPath != filepath.Join(home, ".onellm", "model-catalog.json") ||
		got.CodexCatalogPath != filepath.Join(home, ".codex", "model-catalog.json") {
		t.Fatalf("unexpected catalog paths: %+v", got)
	}
	if strings.Contains(string(data), "providers") || strings.Contains(string(data), "api_key") {
		t.Fatalf("config info exposed provider data: %s", data)
	}
}

func TestBuildConfigInfoDefaultsBellToTrue(t *testing.T) {
	got := buildConfigInfo(&config.Config{}, `C:\config.yaml`, `C:\home`)
	if !got.Bell {
		t.Fatal("nil bell did not default to true")
	}
}

func TestConfigInfoRequiresJSON(t *testing.T) {
	err := configInfoCmd().Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --json") {
		t.Fatalf("error = %v, want --json requirement", err)
	}
}

func TestConfigInfoCommandPrintsValidatedSecretFreeJSON(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	configFile := filepath.Join(dir, "onellm-router.yaml")
	configData := fmt.Sprintf(`server:
  host: "127.0.0.2"
  http_port: 45679
  bell: false
log:
  dir: '%s'
proxy:
  socks5: "127.0.0.1:1083"
providers:
  - prefix: "test"
    base_url: "https://example.invalid/anthropic"
    api_key: "fake-api-key"
    models: ["model"]
`, logDir)
	if err := os.WriteFile(configFile, []byte(configData), 0600); err != nil {
		t.Fatal(err)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeConfig, err := filepath.Rel(workingDir, configFile)
	if err != nil {
		t.Fatal(err)
	}
	oldCfgFile := cfgFile
	cfgFile = relativeConfig
	t.Cleanup(func() { cfgFile = oldCfgFile })

	var output bytes.Buffer
	cmd := configInfoCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("fake-api-key")) {
		t.Fatalf("config info leaked API key: %s", output.Bytes())
	}

	var got configInfo
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigPath != configFile || !filepath.IsAbs(got.ConfigPath) {
		t.Fatalf("config path = %q, want absolute %q", got.ConfigPath, configFile)
	}
	if got.Host != "127.0.0.2" || got.HTTPPort != 45679 ||
		got.LogDir != logDir || got.ProxySOCKS5 != "127.0.0.1:1083" || got.Bell {
		t.Fatalf("unexpected runtime settings: %+v", got)
	}
	if got.OneLLMCatalogPath != filepath.Join(home, ".onellm", "model-catalog.json") ||
		got.CodexCatalogPath != filepath.Join(home, ".codex", "model-catalog.json") {
		t.Fatalf("unexpected catalog paths: %+v", got)
	}
}

func TestConfigInfoCommandRejectsInvalidConfigWithoutJSON(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(configFile, []byte("providers: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldCfgFile := cfgFile
	cfgFile = configFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	var output bytes.Buffer
	cmd := configInfoCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetOut(&output)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "validate config:") {
		t.Fatalf("error = %v, want validation error", err)
	}
	if json.Valid(bytes.TrimSpace(output.Bytes())) ||
		bytes.Contains(output.Bytes(), []byte(`"service"`)) {
		t.Fatalf("invalid config wrote JSON: %s", output.Bytes())
	}
}

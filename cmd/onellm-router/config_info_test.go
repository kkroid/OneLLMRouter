package main

import (
	"bytes"
	"encoding/json"
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
	if got.HTTPPort != 45678 || got.ProxySOCKS5 != "127.0.0.1:1082" || got.Bell {
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

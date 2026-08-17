package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestConfigGetReturnsOnlyEditableSecretSafeSnapshot(t *testing.T) {
	path := writeCommandConfig(t)
	setCommandConfigPath(t, path)
	var output bytes.Buffer
	cmd := configGetCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	oldKey := "old-" + "secret"
	if bytes.Contains(output.Bytes(), []byte(oldKey)) || bytes.Contains(output.Bytes(), []byte(`"api_key":`)) {
		t.Fatalf("config-get exposed API key: %s", output.Bytes())
	}
	var snapshot config.Snapshot
	if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Providers) != 1 || !snapshot.Providers[0].APIKeySet || snapshot.Codex.Models == nil {
		t.Fatalf("incomplete snapshot: %+v", snapshot)
	}
	if snapshot.Codex.OverwriteCatalog == nil || *snapshot.Codex.OverwriteCatalog {
		t.Fatal("snapshot lost explicit codex.overwrite_catalog false")
	}
	for _, forbidden := range []string{`"server"`, `"log"`, `"retry"`, `"proxy_socks5"`} {
		if bytes.Contains(output.Bytes(), []byte(forbidden)) {
			t.Errorf("config-get exposed non-editable field %s: %s", forbidden, output.Bytes())
		}
	}
}

func writeCommandConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	data := []byte(`server:
  host: 127.0.0.1
  http_port: 3456
log:
  level: info
  dir: ~/.onellm/logs
  max_age_days: 30
providers:
  - name: Alpha
    prefix: alpha
    base_url: https://example.invalid
    api_key: old-secret
    models:
      - {id: model, endpoints: [anthropic]}
codex:
  overwrite_catalog: false
  models:
    model:
      default_reasoning_level: medium
      supported_reasoning_levels: [low, medium]
model_slots:
  default: alpha/model
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setCommandConfigPath(t *testing.T, path string) {
	t.Helper()
	old := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = old })
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestConfigDiscoverModelsUsesStoredKeyWithoutWriting(t *testing.T) {
	secret := "stored-" + "secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("x-api-key"); got != secret {
			t.Errorf("x-api-key = %q, want stored key", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"data":[{"id":"model-b"},{"id":"model-a"}]}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	original := []byte(fmt.Sprintf(`server:
  host: 127.0.0.1
  http_port: 3456
providers:
  - name: Alpha
    prefix: alpha
    base_url: %s
    api_key: %s
    proxy: false
    models: [configured-model]
codex:
  models: {}
model_slots: {}
`, server.URL, secret))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	setCommandConfigPath(t, path)
	existing, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(config.NewSnapshot(existing))
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	cmd := configDiscoverModelsCmd()
	cmd.SetArgs([]string{"--stdin-json", "--provider-index", "0", "--protocol", "anthropic"})
	cmd.SetIn(bytes.NewReader(input))
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result modelDiscoveryResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Provider != "alpha" || len(result.Models) != 2 ||
		result.Models[0] != "model-a" || result.Models[1] != "model-b" {
		t.Fatalf("discovery result = %+v", result)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("model discovery modified the configuration")
	}
	if bytes.Contains(output.Bytes(), []byte(secret)) {
		t.Fatal("model discovery output exposed the stored key")
	}
}

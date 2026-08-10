package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexStatusReturnsSchemaEnvelopeWithoutWritingConfig(t *testing.T) {
	paths := writeCodexCommandFixture(t, false)
	configData := []byte("unknown = \"kept\"\nmodel = \"alpha/model\"\n")
	if err := os.WriteFile(paths.config, configData, 0o600); err != nil {
		t.Fatal(err)
	}

	envelope, err := executeCodexCommand(t, "status", paths)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || envelope.Client != "codex" || envelope.Operation != "status" || !envelope.OK || len(envelope.Errors) != 0 {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	if envelope.Result.ConfigParseState != "valid" || envelope.Result.ConfigSyncState != "different" || envelope.Result.SourceTag != "OneLLMRouter" || envelope.Result.ConfigWriteSupported {
		t.Fatalf("unexpected result: %+v", envelope.Result)
	}
	after, readErr := os.ReadFile(paths.config)
	if readErr != nil || !bytes.Equal(after, configData) {
		t.Fatal("status changed config.toml")
	}
}

func TestCodexPreviewReturnsCopyableSnippetWithoutWriting(t *testing.T) {
	paths := writeCodexCommandFixture(t, false)
	configData := []byte("# do not normalize\nmodel = \"old/model\"\n")
	if err := os.WriteFile(paths.config, configData, 0o600); err != nil {
		t.Fatal(err)
	}

	envelope, err := executeCodexCommand(t, "preview", paths, "--model", "alpha/model")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Result.Snippet == nil {
		t.Fatal("preview omitted snippet")
	}
	want := codexSnippet("alpha/model", paths.onellm, "http://localhost:3456/openai/v1")
	if *envelope.Result.Snippet != want || !strings.Contains(want, `model_catalog_json = "`) || !strings.Contains(want, `name = "OneLLMRouter"`) {
		t.Fatalf("snippet = %q, want %q", *envelope.Result.Snippet, want)
	}
	after, readErr := os.ReadFile(paths.config)
	if readErr != nil || !bytes.Equal(after, configData) {
		t.Fatal("preview changed config.toml")
	}
}

func TestCodexPreviewRejectsUnconfiguredOrNonResponsesModel(t *testing.T) {
	paths := writeCodexCommandFixture(t, false)
	envelope, err := executeCodexCommand(t, "preview", paths, "--model", "alpha/missing")
	if err == nil {
		t.Fatal("preview accepted unconfigured model")
	}
	if envelope.OK || len(envelope.Errors) != 1 || envelope.Errors[0].Code != "invalid_arguments" || envelope.Result.Snippet != nil {
		t.Fatalf("unexpected failure envelope: %+v", envelope)
	}
}

func TestCodexPreviewAcceptsDiscoveredCatalogModel(t *testing.T) {
	paths := writeCodexCommandFixture(t, false)
	routerData, err := os.ReadFile(paths.router)
	if err != nil {
		t.Fatal(err)
	}
	routerData = bytes.Replace(routerData, []byte("    models: [model]\n"), nil, 1)
	if err := os.WriteFile(paths.router, routerData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.onellm), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.onellm, []byte(`{"models":[{"slug":"alpha/discovered"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	envelope, commandErr := executeCodexCommand(t, "preview", paths, "--model", "alpha/discovered")
	if commandErr != nil || !envelope.OK || envelope.Result.Snippet == nil {
		t.Fatalf("preview rejected discovered catalog model: %+v, %v", envelope, commandErr)
	}
}

func TestCodexCatalogApplyWritesSelectedCatalogsOnly(t *testing.T) {
	tests := []struct {
		name             string
		overwrite        bool
		wantCodexWritten bool
	}{
		{name: "OneLLM only", overwrite: false, wantCodexWritten: false},
		{name: "compatibility overwrite", overwrite: true, wantCodexWritten: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := writeCodexCommandFixture(t, test.overwrite)
			configData := []byte("# untouched\n")
			if err := os.WriteFile(paths.config, configData, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.codex, []byte("legacy"), 0o600); err != nil {
				t.Fatal(err)
			}

			envelope, err := executeCodexCommand(t, "catalog-apply", paths)
			if err != nil {
				t.Fatal(err)
			}
			if !envelope.OK || envelope.Result.ModelCount != 1 || len(envelope.Result.WrittenPaths) != 1+boolInt(test.wantCodexWritten) {
				t.Fatalf("unexpected apply result: %+v", envelope)
			}
			oneLLMData, readErr := os.ReadFile(paths.onellm)
			if readErr != nil || !bytes.Contains(oneLLMData, []byte(`"slug":"alpha/model"`)) {
				t.Fatalf("OneLLM catalog = %s, error %v", oneLLMData, readErr)
			}
			codexData, readErr := os.ReadFile(paths.codex)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if test.wantCodexWritten && !bytes.Equal(codexData, oneLLMData) {
				t.Fatal("overwrite compatibility did not synchronize Codex catalog")
			}
			if !test.wantCodexWritten && string(codexData) != "legacy" {
				t.Fatal("disabled overwrite changed legacy Codex catalog")
			}
			after, readErr := os.ReadFile(paths.config)
			if readErr != nil || !bytes.Equal(after, configData) {
				t.Fatal("catalog-apply changed config.toml")
			}
			if _, statErr := os.Stat(paths.config + ".bak"); !os.IsNotExist(statErr) {
				t.Fatalf("catalog-apply created a TOML backup: %v", statErr)
			}
		})
	}
}

func TestCodexCommandFailureIsSecretSafeJSON(t *testing.T) {
	paths := writeCodexCommandFixture(t, false)
	secret := "upstream-" + "secret"
	data, err := os.ReadFile(paths.router)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("fake-key"), []byte(secret), 1)
	if err := os.WriteFile(paths.router, data, 0o600); err != nil {
		t.Fatal(err)
	}

	envelope, commandErr := executeCodexCommand(t, "preview", paths, "--model", "missing")
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if commandErr == nil || bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("failure leaked secret or succeeded: %s, %v", encoded, commandErr)
	}
}

type codexTestPaths struct {
	router string
	config string
	onellm string
	codex  string
}

func writeCodexCommandFixture(t *testing.T, overwrite bool) codexTestPaths {
	t.Helper()
	directory := t.TempDir()
	paths := codexTestPaths{
		router: filepath.Join(directory, "router.yaml"), config: filepath.Join(directory, ".codex", "config.toml"),
		onellm: filepath.Join(directory, ".onellm", "model-catalog.json"), codex: filepath.Join(directory, ".codex", "model-catalog.json"),
	}
	if err := os.MkdirAll(filepath.Dir(paths.config), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `server:
  host: 127.0.0.1
  http_port: 3456
proxy:
  socks5: ""
providers:
  - name: Alpha
    prefix: alpha
    responses_base_url: https://example.invalid/v1
    api_key: fake-key
    models: [model]
codex:
  overwrite_catalog: ` + map[bool]string{false: "false", true: "true"}[overwrite] + `
  models: {}
`
	if err := os.WriteFile(paths.router, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	setCommandConfigPath(t, paths.router)
	return paths
}

func executeCodexCommand(t *testing.T, operation string, paths codexTestPaths, extra ...string) (clientEnvelope, error) {
	t.Helper()
	args := []string{"--json", "--codex-config", paths.config, "--onellm-catalog", paths.onellm, "--codex-catalog", paths.codex}
	args = append(args, extra...)
	var output bytes.Buffer
	cmd := newCodexOperationCmd(operation)
	cmd.SetArgs(args)
	cmd.SetOut(&output)
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	var envelope clientEnvelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode output %q: %v", output.Bytes(), decodeErr)
	}
	return envelope, err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestTomlStringEscapesWindowsPathsAndControls(t *testing.T) {
	got := tomlString("C:\\Users\\name\tfile\n")
	if got != `"C:\\Users\\name\tfile\n"` {
		t.Fatalf("tomlString() = %q", got)
	}
}

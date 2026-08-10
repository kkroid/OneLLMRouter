package codexconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestInspectAcceptsUnknownTOMLAndReportsCurrent(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	oneLLMPath := filepath.Join(directory, "onellm.json")
	codexPath := filepath.Join(directory, "codex.json")
	configData := `# preserved because status never writes
model = "mars/gpt-5.6-sol"
model_provider = "onellm"
model_reasoning_effort = "high"
model_catalog_json = "` + filepath.ToSlash(oneLLMPath) + `"
approval_policy = "never"

[model_providers.onellm]
name = "OneLLMRouter"
base_url = "http://localhost:3456/openai/v1"
wire_api = "responses"
requires_openai_auth = true
unknown_provider_value = "ignored"

[projects."C:\\work"]
trust_level = "trusted"
`
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	catalogData := []byte(`{"models":[{"slug":"mars/gpt-5.6-sol"},{"slug":"c78/gpt-5.6-sol"}]}`)
	for _, path := range []string{oneLLMPath, codexPath} {
		if err := os.WriteFile(path, catalogData, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wantTime := time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(oneLLMPath, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}

	result := Inspect(Options{
		ConfigPath: configPath, OneLLMCatalogPath: oneLLMPath, CodexCatalogPath: codexPath,
		ExpectedBaseURL: "http://localhost:3456/openai/v1",
	})
	if result.ConfigParseState != "valid" || result.ConfigSyncState != "current" {
		t.Fatalf("config states = %s/%s", result.ConfigParseState, result.ConfigSyncState)
	}
	if result.EffectiveModel == nil || *result.EffectiveModel != "mars/gpt-5.6-sol" || result.EffectiveProvider == nil || *result.EffectiveProvider != "mars" {
		t.Fatalf("effective model projection = %+v", result)
	}
	if result.CatalogSyncState != "current" || result.ModelCount != 2 {
		t.Fatalf("catalog projection = %+v", result)
	}
	if !reflect.DeepEqual(result.SourceProviders, []string{"c78", "mars"}) {
		t.Fatalf("source providers = %#v", result.SourceProviders)
	}
	if result.CatalogGeneratedAt == nil || *result.CatalogGeneratedAt != wantTime.Format(time.RFC3339) {
		t.Fatalf("generated at = %v", result.CatalogGeneratedAt)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(after) != configData {
		t.Fatal("status changed config.toml")
	}
}

func TestInspectExpectedConfigAndCatalogStates(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	oneLLMPath := filepath.Join(directory, "onellm.json")
	codexPath := filepath.Join(directory, "codex.json")
	options := Options{ConfigPath: configPath, OneLLMCatalogPath: oneLLMPath, CodexCatalogPath: codexPath}

	absent := Inspect(options)
	if absent.ConfigParseState != "absent" || absent.ConfigSyncState != "absent" || absent.CatalogSyncState != "absent" {
		t.Fatalf("absent states = %+v", absent)
	}
	if err := os.WriteFile(configPath, []byte("model = [1]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oneLLMPath, []byte(`{"models":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := Inspect(options)
	if invalid.ConfigParseState != "invalid" || invalid.ConfigSyncState != "invalid" || invalid.CatalogSyncState != "invalid" {
		t.Fatalf("invalid states = %+v", invalid)
	}

	if err := os.WriteFile(configPath, []byte(`model = "mars/model"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`{"models":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	different := Inspect(options)
	if different.ConfigSyncState != "different" || different.CatalogSyncState != "current" {
		t.Fatalf("different states = %+v", different)
	}
}

func TestCatalogHasModelRequiresValidExactSlug(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(`{"models":[{"slug":"mars/model"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !CatalogHasModel(path, "mars/model") || CatalogHasModel(path, "mars/other") {
		t.Fatal("CatalogHasModel did not require an exact configured slug")
	}
}

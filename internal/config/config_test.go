package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultConfigOverwritesCodexCatalog(t *testing.T) {
	if !DefaultConfig().Codex.OverwriteCatalog {
		t.Fatal("codex catalog overwrite must default to enabled")
	}
}

func TestValidateRequiresProviderEndpointRegardlessOfPrefix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{{
		Name: "ordinary cp prefix", Prefix: "cp", Models: []string{"m1"},
	}}

	if err := cfg.Validate(); err == nil {
		t.Fatal("provider without an API endpoint must fail validation")
	}
}

func TestLoadPreservesExplicitCodexCatalogDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	data := []byte("codex:\n  overwrite_catalog: false\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Codex.OverwriteCatalog {
		t.Fatal("explicit codex catalog overwrite disable was ignored")
	}
}

func TestDefaultConfigIncludesKnownCodexReasoningModels(t *testing.T) {
	want := map[string]CodexModelConfig{
		"gpt-5.5": {
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh"},
		},
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
		},
		"gpt-5.6-terra": {
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
		},
		"gpt-5.6-luna": {
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"},
		},
	}
	if got := DefaultConfig().Codex.Models; !reflect.DeepEqual(got, want) {
		t.Fatalf("Codex model mappings = %#v, want %#v", got, want)
	}
}

func TestLoadOverridesDefaultCodexReasoningModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onellm-router.yaml")
	data := []byte(`codex:
  models:
    gpt-5.6-sol:
      default_reasoning_level: high
      supported_reasoning_levels: [high, xhigh]
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := CodexModelConfig{
		DefaultReasoningLevel:    "high",
		SupportedReasoningLevels: []string{"high", "xhigh"},
	}
	if got := cfg.Codex.Models["gpt-5.6-sol"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("configured mapping = %#v, want %#v", got, want)
	}
}

package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kkroid/onellm-router/internal/catalog"
	"github.com/kkroid/onellm-router/internal/config"
)

func TestCodexCatalogOptionsUseUserDirectoriesAndOverwriteFlag(t *testing.T) {
	userHome := filepath.Join("C:", "Users", "catalog-test")
	options := codexCatalogOptions(userHome, false)

	wantOneLLM := filepath.Join(userHome, ".onellm", "model-catalog.json")
	if options.OneLLMPath != wantOneLLM {
		t.Fatalf("OneLLM path = %q, want %q", options.OneLLMPath, wantOneLLM)
	}
	wantCodex := filepath.Join(userHome, ".codex", "model-catalog.json")
	if options.CodexPath != wantCodex {
		t.Fatalf("Codex path = %q, want %q", options.CodexPath, wantCodex)
	}
	if options.OverwriteCodex {
		t.Fatal("overwrite flag was not preserved")
	}
}

func TestCodexReasoningMappingsConvertConfiguration(t *testing.T) {
	got := codexReasoningMappings(map[string]config.CodexModelConfig{
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium"},
		},
	})
	want := map[string]catalog.ReasoningConfig{
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mappings = %#v, want %#v", got, want)
	}
}

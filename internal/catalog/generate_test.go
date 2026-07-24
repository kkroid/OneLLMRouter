package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kkroid/onellm-router/internal/router"
)

func TestGenerateCodexWritesOneLLMAndCodexCatalogs(t *testing.T) {
	directory := t.TempDir()
	oneLLMPath := filepath.Join(directory, "onellm", "model-catalog.json")
	codexPath := filepath.Join(directory, "codex", "model-catalog.json")
	service := New(nil)
	service.SetReasoningMappings(map[string]ReasoningConfig{
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
		},
	})
	providers := []router.Provider{{
		Prefix:           "mars",
		ResponsesBaseURL: "http://unused",
		Models:           []string{"gpt-5.6-sol"},
	}}

	result, err := service.GenerateCodex(context.Background(), providers, GenerateOptions{
		OneLLMPath:     oneLLMPath,
		CodexPath:      codexPath,
		OverwriteCodex: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ModelCount != 1 {
		t.Fatalf("model count = %d, want 1", result.ModelCount)
	}

	oneLLMData, err := os.ReadFile(oneLLMPath)
	if err != nil {
		t.Fatal(err)
	}
	codexData, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(oneLLMData) != string(codexData) {
		t.Fatal("generated catalogs differ")
	}
	var document struct {
		Models []struct {
			DefaultReasoningLevel    string           `json:"default_reasoning_level"`
			SupportedReasoningLevels []ReasoningLevel `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(codexData, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Models) != 1 || document.Models[0].DefaultReasoningLevel != "low" {
		t.Fatalf("unexpected generated reasoning metadata: %+v", document.Models)
	}
	if levels := document.Models[0].SupportedReasoningLevels; len(levels) != 6 || levels[0].Effort != "low" || levels[5].Effort != "ultra" {
		t.Fatalf("unexpected generated reasoning levels: %+v", levels)
	}
}

func TestGenerateCodexDisabledLeavesCodexCatalogUntouched(t *testing.T) {
	directory := t.TempDir()
	oneLLMPath := filepath.Join(directory, "onellm.json")
	codexPath := filepath.Join(directory, "codex.json")
	if err := os.WriteFile(codexPath, []byte("existing-codex"), 0o600); err != nil {
		t.Fatal(err)
	}

	service := New(nil)
	_, err := service.GenerateCodex(context.Background(), []router.Provider{{
		Prefix:           "mars",
		ResponsesBaseURL: "http://unused",
		Models:           []string{"gpt-5.6-sol"},
	}}, GenerateOptions{
		OneLLMPath:     oneLLMPath,
		CodexPath:      codexPath,
		OverwriteCodex: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oneLLMPath); err != nil {
		t.Fatal(err)
	}
	codexData, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(codexData) != "existing-codex" {
		t.Fatalf("Codex catalog was modified: %q", codexData)
	}
}

func TestGenerateCodexReplacesExistingCatalogs(t *testing.T) {
	directory := t.TempDir()
	oneLLMPath := filepath.Join(directory, "onellm.json")
	codexPath := filepath.Join(directory, "codex.json")
	for _, path := range []string{oneLLMPath, codexPath} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	service := New(nil)
	_, err := service.GenerateCodex(context.Background(), []router.Provider{{
		Prefix:           "mars",
		ResponsesBaseURL: "http://unused",
		Models:           []string{"gpt-5.6-sol"},
	}}, GenerateOptions{OneLLMPath: oneLLMPath, CodexPath: codexPath, OverwriteCodex: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oneLLMPath, codexPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == "old" {
			t.Fatalf("catalog %s was not replaced", path)
		}
	}
}

func TestGenerateCodexSourceFailurePreservesExistingCatalogs(t *testing.T) {
	directory := t.TempDir()
	oneLLMPath := filepath.Join(directory, "onellm.json")
	codexPath := filepath.Join(directory, "codex.json")
	for _, path := range []string{oneLLMPath, codexPath} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	failingSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer failingSource.Close()

	service := New(nil)
	result, err := service.GenerateCodex(context.Background(), []router.Provider{
		{
			Prefix:           "configured",
			ResponsesBaseURL: "http://unused",
			Models:           []string{"gpt-5.6-sol"},
		},
		{
			Prefix:           "unavailable",
			ResponsesBaseURL: failingSource.URL,
			APIKey:           "test",
		},
	}, GenerateOptions{OneLLMPath: oneLLMPath, CodexPath: codexPath, OverwriteCodex: true})
	if err == nil {
		t.Fatal("expected source failure error")
	}
	if result.ModelCount != 1 || len(result.SourceErrors) != 1 {
		t.Fatalf("unexpected generation result: %+v", result)
	}
	for _, path := range []string{oneLLMPath, codexPath} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != "old" {
			t.Fatalf("catalog %s changed after partial discovery", path)
		}
	}
}

func TestGenerateCodexEmptyResultPreservesExistingCatalogs(t *testing.T) {
	directory := t.TempDir()
	oneLLMPath := filepath.Join(directory, "onellm.json")
	codexPath := filepath.Join(directory, "codex.json")
	for _, path := range []string{oneLLMPath, codexPath} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	service := New(nil)
	_, err := service.GenerateCodex(context.Background(), []router.Provider{{
		Prefix:  "anthropic-only",
		BaseURL: "http://unused",
	}}, GenerateOptions{OneLLMPath: oneLLMPath, CodexPath: codexPath, OverwriteCodex: true})
	if err == nil {
		t.Fatal("expected empty catalog error")
	}
	for _, path := range []string{oneLLMPath, codexPath} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != "old" {
			t.Fatalf("catalog %s changed after empty discovery", path)
		}
	}
}

package router

import (
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestResolverExactMatch(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8", "claude-fable-5"}},
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"deepseek-v4-pro"}},
	})

	result := r.Resolve("cp/claude-opus-4.8")
	if result == nil {
		t.Fatal("expected match for cp/claude-opus-4.8")
	}
	if result.Model != "claude-opus-4.8" {
		t.Errorf("expected model claude-opus-4.8, got %s", result.Model)
	}
	if result.Provider.Prefix != "cp" {
		t.Errorf("expected provider cp, got %s", result.Provider.Prefix)
	}
}

func TestResolverPrefixMatch(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"deepseek-v4-pro", "deepseek-v4-flash"}},
	})

	result := r.Resolve("ds")
	if result == nil {
		t.Fatal("expected prefix match for ds")
	}
	if result.Model != "deepseek-v4-pro" {
		t.Errorf("expected first model deepseek-v4-pro, got %s", result.Model)
	}
}

func TestResolverUnknownModel(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8"}},
	})

	result := r.Resolve("nonexistent/model")
	if result != nil {
		t.Error("expected nil for unknown model")
	}
}

func TestResolverAllModelIDs(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"m1", "m2"}},
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"m3"}},
	})

	ids := r.AllModelIDs()
	if len(ids) != 3 {
		t.Errorf("expected 3 models, got %d: %v", len(ids), ids)
	}

	// Check all expected IDs exist
	set := make(map[string]bool)
	for _, id := range ids {
		set[id] = true
	}
	for _, want := range []string{"cp/m1", "cp/m2", "ds/m3"} {
		if !set[want] {
			t.Errorf("expected model %s in list", want)
		}
	}
}

func TestResolverCopilotProvider(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"m3"}},
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8"}},
	})

	cp := r.CopilotProvider()
	if cp == nil {
		t.Fatal("expected copilot provider")
	}
	if cp.Prefix != "cp" {
		t.Errorf("expected cp prefix, got %s", cp.Prefix)
	}
}

func TestResolverNoCopilotProvider(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"m3"}},
	})

	cp := r.CopilotProvider()
	if cp != nil {
		t.Error("expected nil for missing copilot provider")
	}
}

func TestResolverOneMAlias(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"deepseek-v4-pro[1m]", "deepseek-v4-flash[1m]"}},
	})

	// Without [1m] (Claude Code strips it) → passthrough
	result := r.Resolve("ds/deepseek-v4-pro")
	if result == nil {
		t.Fatal("expected match for ds/deepseek-v4-pro (alias)")
	}
	if result.Model != "deepseek-v4-pro" {
		t.Errorf("model should be deepseek-v4-pro (passthrough), got %s", result.Model)
	}

	// With [1m] → passthrough
	result2 := r.Resolve("ds/deepseek-v4-pro[1m]")
	if result2 == nil {
		t.Fatal("expected match for ds/deepseek-v4-pro[1m]")
	}
	if result2.Model != "deepseek-v4-pro[1m]" {
		t.Errorf("model should be deepseek-v4-pro[1m] (passthrough), got %s", result2.Model)
	}
}

func TestFromConfig(t *testing.T) {
	providers := FromConfig([]config.ProviderConfig{
		{Name: "DeepSeek", Prefix: "ds", BaseURL: "https://api.deepseek.com/anthropic", APIKey: "sk-test", Models: []string{"deepseek-v4-pro", "deepseek-v4-flash"}},
		{Name: "Copilot", Prefix: "cp", APIKey: "not-needed", Models: []string{"claude-opus-4.8"}},
	})

	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	var ds *Provider
	for i := range providers {
		if providers[i].Prefix == "ds" {
			ds = &providers[i]
			break
		}
	}
	if ds == nil || ds.Name != "DeepSeek" || ds.BaseURL != "https://api.deepseek.com/anthropic" || ds.APIKey != "sk-test" || len(ds.Models) != 2 {
		t.Errorf("wrong ds provider: %+v", ds)
	}

	var cp *Provider
	for i := range providers {
		if providers[i].Prefix == "cp" {
			cp = &providers[i]
			break
		}
	}
	if cp == nil || cp.Name != "Copilot" || len(cp.Models) != 1 {
		t.Errorf("wrong cp provider: %+v", cp)
	}
}

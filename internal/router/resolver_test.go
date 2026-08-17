package router

import (
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
)

func TestResolverExactMatch(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "cp", Name: "Provider CP", Models: []string{"claude-opus-4.8", "claude-fable-5"}},
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
		{Prefix: "cp", Name: "Provider CP", Models: []string{"claude-opus-4.8"}},
	})

	result := r.Resolve("nonexistent/model")
	if result != nil {
		t.Error("expected nil for unknown model")
	}
}

func TestResolverRejectsUnknownConfiguredSlashModel(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix: "mars",
		Models: []string{"gpt-5.6-sol"},
	}})

	if result := r.Resolve("mars/not-configured"); result != nil {
		t.Fatalf("unexpected configured-list bypass: %+v", result)
	}
}

func TestResolverRejectsUnknownConfiguredLegacyModel(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix: "mars",
		Models: []string{"gpt-5.6-sol"},
	}})

	if result := r.Resolve("mars-not-configured"); result != nil {
		t.Fatalf("unexpected configured-list bypass: %+v", result)
	}
}

func TestResolverAllowsNamespacedDynamicModel(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix:           "mars",
		ResponsesBaseURL: "http://unused",
	}})

	result := r.Resolve("mars/anything")
	if result == nil || result.Provider.Prefix != "mars" || result.Model != "anything" {
		t.Fatalf("unexpected dynamic result: %+v", result)
	}
}

func TestResolverRejectsBareDynamicFallback(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix:  "mars",
		BaseURL: "http://unused",
	}})

	if result := r.Resolve("anything"); result != nil {
		t.Fatalf("unexpected bare dynamic fallback: %+v", result)
	}
}

func TestResolverAllModelIDs(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "cp", Name: "Provider CP", Models: []string{"m1", "m2"}},
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

func TestProviderRequiresConfiguredEndpointRegardlessOfPrefix(t *testing.T) {
	provider := Provider{Prefix: "cp", Models: []string{"claude-opus-4.8"}}
	if provider.SupportsEndpoint(EndpointAnthropic) {
		t.Fatal("cp prefix must not imply Anthropic support")
	}
	if provider.SupportsEndpoint(EndpointOpenAI) {
		t.Fatal("prefix without endpoint must not support Chat Completions")
	}
	if provider.SupportsEndpoint(EndpointResponses) {
		t.Fatal("prefix without endpoint must not support Responses")
	}
}

func TestResolverPreservesOneMModelName(t *testing.T) {
	r := NewResolver([]Provider{
		{Prefix: "ds", Name: "DeepSeek", Models: []string{"deepseek-v4-pro[1m]", "deepseek-v4-flash[1m]"}},
	})

	if result := r.Resolve("ds/deepseek-v4-pro"); result != nil {
		t.Fatalf("unexpected alias match: %+v", result)
	}
	result := r.Resolve("ds/deepseek-v4-pro[1m]")
	if result == nil {
		t.Fatal("expected match for ds/deepseek-v4-pro[1m]")
	}
	if result.Model != "deepseek-v4-pro[1m]" {
		t.Errorf("model should be preserved, got %s", result.Model)
	}
}

func TestResolverUsesEndpointSpecificModelRoute(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix: "ds", BaseURL: "http://unused", ResponsesBaseURL: "http://unused",
		ModelRoutes: []ModelRoute{
			{ID: "deepseek-v4-pro[1m]", Endpoints: []EndpointType{EndpointAnthropic}, UpstreamModel: "deepseek-v4-pro[1m]"},
			{ID: "deepseek-v4-pro", Endpoints: []EndpointType{EndpointResponses}, UpstreamModel: "deepseek-v4-pro"},
		},
	}})

	if result := r.ResolveForEndpoint("ds/deepseek-v4-pro[1m]", EndpointAnthropic); result == nil || result.Model != "deepseek-v4-pro[1m]" {
		t.Fatalf("anthropic route = %+v", result)
	}
	if result := r.ResolveForEndpoint("ds/deepseek-v4-pro", EndpointResponses); result == nil || result.Model != "deepseek-v4-pro" {
		t.Fatalf("responses route = %+v", result)
	}
	if result := r.ResolveForEndpoint("ds/deepseek-v4-pro[1m]", EndpointResponses); result != nil {
		t.Fatalf("anthropic-only model resolved for responses: %+v", result)
	}
}

func TestResolverMapsClientModelToEndpointUpstreamModel(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix: "ds", ResponsesBaseURL: "http://unused",
		ModelRoutes: []ModelRoute{{
			ID: "deepseek-v4-pro[1m]", Endpoints: []EndpointType{EndpointResponses}, UpstreamModel: "deepseek-v4-pro",
		}},
	}})

	result := r.ResolveForEndpoint("ds/deepseek-v4-pro[1m]", EndpointResponses)
	if result == nil || result.Model != "deepseek-v4-pro" {
		t.Fatalf("responses route = %+v", result)
	}
}

func TestResolverPrefersExactModelOverOneMAlias(t *testing.T) {
	r := NewResolver([]Provider{{
		Prefix: "ds", ResponsesBaseURL: "http://unused",
		ModelRoutes: []ModelRoute{
			{ID: "model[1m]", Endpoints: []EndpointType{EndpointResponses}, UpstreamModel: "alias-upstream"},
			{ID: "model", Endpoints: []EndpointType{EndpointResponses}, UpstreamModel: "exact-upstream"},
		},
	}})

	result := r.ResolveForEndpoint("ds/model", EndpointResponses)
	if result == nil || result.Model != "exact-upstream" {
		t.Fatalf("responses route = %+v", result)
	}
}

func TestFromConfig(t *testing.T) {
	providers := FromConfig([]config.ProviderConfig{
		{Name: "DeepSeek", Prefix: "ds", BaseURL: "https://api.deepseek.com/anthropic", APIKey: "sk-test", ModelConfigs: []config.ProviderModelConfig{
			{ID: "deepseek-v4-pro", Endpoints: []string{"anthropic"}},
			{ID: "deepseek-v4-flash", Endpoints: []string{"anthropic"}},
		}},
		{Name: "Provider CP", Prefix: "cp", BaseURL: "https://example.invalid/anthropic", APIKey: "sk-test", ModelConfigs: []config.ProviderModelConfig{
			{ID: "claude-opus-4.8", Endpoints: []string{"anthropic"}},
		}},
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
	if ds == nil || ds.Name != "DeepSeek" || ds.BaseURL != "https://api.deepseek.com/anthropic" || ds.APIKey != "sk-test" || len(ds.ModelRoutes) != 2 {
		t.Errorf("wrong ds provider: %+v", ds)
	}

	var cp *Provider
	for i := range providers {
		if providers[i].Prefix == "cp" {
			cp = &providers[i]
			break
		}
	}
	if cp == nil || cp.Name != "Provider CP" || len(cp.ModelRoutes) != 1 {
		t.Errorf("wrong cp provider: %+v", cp)
	}
}

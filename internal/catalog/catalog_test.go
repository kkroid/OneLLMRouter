package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/kkroid/onellm-router/internal/router"
)

func TestListConfiguredModelsOverrideUpstream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"data":[{"id":"upstream-model"}]}`)
	}))
	defer server.Close()

	service := New(func(*router.Provider) *http.Client { return server.Client() })
	result := service.List(context.Background(), []router.Provider{{
		Prefix:           "c78",
		ResponsesBaseURL: server.URL,
		Models:           []string{"gpt-5.6-sol", "not-real"},
	}}, router.EndpointResponses)

	if calls.Load() != 0 {
		t.Fatalf("configured models must not query upstream, got %d calls", calls.Load())
	}
	if len(result.Errors) != 0 {
		t.Fatalf("configured models returned errors: %+v", result.Errors)
	}
	assertModelIDs(t, result.Models, "c78/gpt-5.6-sol", "c78/not-real")
}

func TestListUsesRequestedProtocolURL(t *testing.T) {
	type source struct {
		endpoint router.EndpointType
		model    string
		path     string
		calls    atomic.Int32
		server   *httptest.Server
	}

	sources := []*source{
		{endpoint: router.EndpointAnthropic, model: "claude-model", path: "/models"},
		{endpoint: router.EndpointOpenAI, model: "chat-model", path: "/models"},
		{endpoint: router.EndpointResponses, model: "responses-model", path: "/v1/models"},
	}
	for _, item := range sources {
		item := item
		item.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			item.calls.Add(1)
			if r.URL.Path != item.path {
				t.Errorf("%s path = %q, want %q", item.endpoint, r.URL.Path, item.path)
			}
			fmt.Fprintf(w, `{"data":[{"id":%q,"created":7}]}`, item.model)
		}))
		defer item.server.Close()
	}

	provider := router.Provider{
		Prefix:           "mixed",
		BaseURL:          sources[0].server.URL,
		OpenAIBaseURL:    sources[1].server.URL,
		ResponsesBaseURL: sources[2].server.URL,
	}
	service := New(func(*router.Provider) *http.Client { return http.DefaultClient })

	for requestedIndex, requested := range sources {
		for _, item := range sources {
			item.calls.Store(0)
		}

		result := service.List(context.Background(), []router.Provider{provider}, requested.endpoint)
		if len(result.Errors) != 0 {
			t.Fatalf("%s discovery errors: %+v", requested.endpoint, result.Errors)
		}
		assertModelIDs(t, result.Models, "mixed/"+requested.model)

		for sourceIndex, item := range sources {
			want := int32(0)
			if sourceIndex == requestedIndex {
				want = 1
			}
			if got := item.calls.Load(); got != want {
				t.Errorf("request %s called %s source %d times, want %d", requested.endpoint, item.endpoint, got, want)
			}
		}
	}
}

func TestListPreservesSuccessfulProvidersWhenAnotherSourceFails(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusBadGateway)
	}))
	defer failing.Close()

	successful := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"gpt-5.6-sol"}]}`)
	}))
	defer successful.Close()

	service := New(func(*router.Provider) *http.Client { return http.DefaultClient })
	result := service.List(context.Background(), []router.Provider{
		{Prefix: "bad", ResponsesBaseURL: failing.URL},
		{Prefix: "good", ResponsesBaseURL: successful.URL},
	}, router.EndpointResponses)

	assertModelIDs(t, result.Models, "good/gpt-5.6-sol")
	if len(result.Errors) != 1 || result.Errors[0].Provider != "bad" {
		t.Fatalf("expected one bad-provider error, got %+v", result.Errors)
	}
}

func TestListPreservesUpstreamCodexReasoningMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"future-model","display_name":"Upstream Future","description":"upstream","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"high","description":"Upstream high"}],"shell_type":"shell_command","visibility":"list","supported_in_api":true,"priority":7,"availability_nux":null,"upgrade":null,"base_instructions":"upstream instructions","supports_reasoning_summaries":true,"support_verbosity":true,"default_verbosity":"low","apply_patch_tool_type":"freeform","truncation_policy":{"mode":"tokens","limit":12345},"supports_parallel_tool_calls":true,"experimental_supported_tools":[],"context_window":654321}]}`)
	}))
	defer server.Close()

	service := New(func(*router.Provider) *http.Client { return server.Client() })
	result := service.List(context.Background(), []router.Provider{{
		Prefix:           "upstream",
		ResponsesBaseURL: server.URL,
	}}, router.EndpointResponses)

	if len(result.Errors) != 0 || len(result.Models) != 1 {
		t.Fatalf("unexpected discovery result: %+v", result)
	}
	model := result.Models[0]
	if model.DefaultReasoningLevel != "high" || len(model.SupportedReasoningLevels) != 1 {
		t.Fatalf("upstream reasoning metadata was lost: %+v", model)
	}
	if model.SupportedReasoningLevels[0].Description != "Upstream high" {
		t.Fatalf("upstream description was lost: %+v", model.SupportedReasoningLevels)
	}
	data, err := MarshalCodex(result.Models)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var contextWindow int64
	if err := json.Unmarshal(document.Models[0]["context_window"], &contextWindow); err != nil {
		t.Fatal(err)
	}
	if contextWindow != 654321 {
		t.Fatalf("upstream full metadata was not preserved: %s", data)
	}
}

func TestListConfiguredReasoningMappingOverridesUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"gpt-5.6-sol","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"high","description":"Upstream"}]}]}`)
	}))
	defer server.Close()

	service := New(func(*router.Provider) *http.Client { return server.Client() })
	service.SetReasoningMappings(map[string]ReasoningConfig{
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium"},
		},
	})
	result := service.List(context.Background(), []router.Provider{{
		Prefix:           "c78",
		ResponsesBaseURL: server.URL,
	}}, router.EndpointResponses)

	if len(result.Errors) != 0 || len(result.Models) != 1 {
		t.Fatalf("unexpected discovery result: %+v", result)
	}
	model := result.Models[0]
	if model.DefaultReasoningLevel != "low" {
		t.Fatalf("configured default = %q, want low", model.DefaultReasoningLevel)
	}
	want := []ReasoningLevel{
		{Effort: "low", Description: "Fast responses with lighter reasoning"},
		{Effort: "medium", Description: "Balances speed and reasoning depth for everyday tasks"},
	}
	if !reflect.DeepEqual(model.SupportedReasoningLevels, want) {
		t.Fatalf("configured levels = %#v, want %#v", model.SupportedReasoningLevels, want)
	}
}

func assertModelIDs(t *testing.T, models []Model, want ...string) {
	t.Helper()
	got := make([]string, len(models))
	for index := range models {
		got[index] = models[index].ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
}

package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kkroid/onellm-router/internal/catalog"
	"github.com/kkroid/onellm-router/internal/router"
)

func TestServeModelList_TotalDiscoveryFailureReturnsBadGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusBadGateway)
	}))
	defer server.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix:           "broken",
		ResponsesBaseURL: server.URL,
	}})
	handler := &Handler{
		Resolver:     resolver,
		ProxyClient:  server.Client(),
		DirectClient: server.Client(),
		Logger:       slog.New(slog.DiscardHandler),
	}

	request := httptest.NewRequest(http.MethodGet, "/openai/models", nil)
	recorder := httptest.NewRecorder()
	handler.ServeModelList(recorder, request, router.EndpointResponses)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
}

func TestServeModelList_ResponsesUsesOnlyResponsesCatalogURL(t *testing.T) {
	var anthropicCalls atomic.Int32
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"claude-only"}]}`))
	}))
	defer anthropic.Close()

	var responsesCalls atomic.Int32
	responses := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responsesCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-5.6-sol"}]}`))
	}))
	defer responses.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix:           "mixed",
		BaseURL:          anthropic.URL,
		ResponsesBaseURL: responses.URL,
	}})
	handler := &Handler{
		Resolver:     resolver,
		ProxyClient:  http.DefaultClient,
		DirectClient: http.DefaultClient,
		Logger:       slog.New(slog.DiscardHandler),
	}

	request := httptest.NewRequest(http.MethodGet, "/openai/models", nil)
	recorder := httptest.NewRecorder()
	handler.ServeModelList(recorder, request, router.EndpointResponses)

	if anthropicCalls.Load() != 0 {
		t.Fatalf("Responses catalog queried Anthropic URL %d times", anthropicCalls.Load())
	}
	if responsesCalls.Load() != 1 {
		t.Fatalf("Responses catalog URL calls = %d, want 1", responsesCalls.Load())
	}
	var response struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Models) != 1 || response.Models[0].Slug != "mixed/gpt-5.6-sol" {
		t.Fatalf("unexpected response catalog: %s", recorder.Body.String())
	}
}

func TestServeModelList_ResponsesReturnsCodexCatalog(t *testing.T) {
	resolver := router.NewResolver([]router.Provider{
		{
			Name:             "Mars",
			Prefix:           "mars",
			ResponsesBaseURL: "http://unused",
			Models:           []string{"gpt-5.6-sol"},
		},
	})
	handler := &Handler{Resolver: resolver, Logger: slog.New(slog.DiscardHandler)}
	handler.catalogService().SetReasoningMappings(map[string]catalog.ReasoningConfig{
		"gpt-5.6-sol": {
			DefaultReasoningLevel:    "low",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
		},
	})

	request := httptest.NewRequest("GET", "/openai/models", nil)
	recorder := httptest.NewRecorder()
	handler.ServeModelList(recorder, request, router.EndpointType("responses"))

	var response struct {
		Models []map[string]json.RawMessage `json:"models"`
		Data   json.RawMessage              `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode model catalog: %v", err)
	}
	if response.Data != nil {
		t.Fatalf("Codex catalog must not use the OpenAI data wrapper: %s", recorder.Body.String())
	}
	if len(response.Models) != 1 {
		t.Fatalf("expected one Codex model, got %d: %s", len(response.Models), recorder.Body.String())
	}

	model := response.Models[0]
	var slug string
	if err := json.Unmarshal(model["slug"], &slug); err != nil {
		t.Fatalf("decode model slug: %v", err)
	}
	if slug != "mars/gpt-5.6-sol" {
		t.Fatalf("expected namespaced model slug, got %q", slug)
	}
	var defaultReasoningLevel string
	if err := json.Unmarshal(model["default_reasoning_level"], &defaultReasoningLevel); err != nil {
		t.Fatalf("decode default reasoning level: %v", err)
	}
	if defaultReasoningLevel != "low" {
		t.Fatalf("default reasoning level = %q, want low", defaultReasoningLevel)
	}
	var reasoningLevels []catalog.ReasoningLevel
	if err := json.Unmarshal(model["supported_reasoning_levels"], &reasoningLevels); err != nil {
		t.Fatalf("decode reasoning levels: %v", err)
	}
	if len(reasoningLevels) != 6 || reasoningLevels[0].Effort != "low" || reasoningLevels[5].Effort != "ultra" {
		t.Fatalf("unexpected reasoning levels: %#v", reasoningLevels)
	}

	requiredFields := []string{
		"display_name",
		"description",
		"default_reasoning_level",
		"supported_reasoning_levels",
		"shell_type",
		"visibility",
		"supported_in_api",
		"priority",
		"availability_nux",
		"upgrade",
		"base_instructions",
		"supports_reasoning_summaries",
		"support_verbosity",
		"default_verbosity",
		"apply_patch_tool_type",
		"truncation_policy",
		"supports_parallel_tool_calls",
		"experimental_supported_tools",
	}
	for _, field := range requiredFields {
		if _, ok := model[field]; !ok {
			t.Errorf("Codex model is missing required field %q", field)
		}
	}
}

func TestServeModelList_SeparatesChatCompletionsAndResponses(t *testing.T) {
	resolver := router.NewResolver([]router.Provider{
		{
			Name:          "DeepSeek",
			Prefix:        "ds",
			OpenAIBaseURL: "http://unused",
			Models:        []string{"deepseek-v4-pro[1m]"},
		},
		{
			Name:             "78Code",
			Prefix:           "c78",
			OpenAIBaseURL:    "http://unused",
			ResponsesBaseURL: "http://unused",
			Models:           []string{"gpt-5.6-sol"},
		},
	})
	handler := &Handler{Resolver: resolver, Logger: slog.New(slog.DiscardHandler)}

	chatRequest := httptest.NewRequest("GET", "/openai/v1/models", nil)
	chatRecorder := httptest.NewRecorder()
	handler.ServeModelList(chatRecorder, chatRequest, router.EndpointOpenAI)

	var chatResponse struct {
		Data   []router.ModelEntry `json:"data"`
		Models json.RawMessage     `json:"models"`
	}
	if err := json.Unmarshal(chatRecorder.Body.Bytes(), &chatResponse); err != nil {
		t.Fatalf("decode Chat Completions model list: %v", err)
	}
	if chatResponse.Models != nil {
		t.Fatalf("Chat Completions model list must use the OpenAI data wrapper: %s", chatRecorder.Body.String())
	}
	if len(chatResponse.Data) != 2 {
		t.Fatalf("expected two Chat Completions models, got %d: %s", len(chatResponse.Data), chatRecorder.Body.String())
	}

	responsesRequest := httptest.NewRequest("GET", "/openai/models", nil)
	responsesRecorder := httptest.NewRecorder()
	handler.ServeModelList(responsesRecorder, responsesRequest, router.EndpointType("responses"))

	var responsesResponse struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(responsesRecorder.Body.Bytes(), &responsesResponse); err != nil {
		t.Fatalf("decode Responses model catalog: %v", err)
	}
	if responsesResponse.Data != nil {
		t.Fatalf("Responses model catalog must use the Codex models wrapper: %s", responsesRecorder.Body.String())
	}
	if len(responsesResponse.Models) != 1 || responsesResponse.Models[0].Slug != "c78/gpt-5.6-sol" {
		t.Fatalf("expected only the Responses model, got: %s", responsesRecorder.Body.String())
	}
}

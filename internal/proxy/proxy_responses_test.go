package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	onellmLog "github.com/kkroid/onellm-router/internal/log"
	"github.com/kkroid/onellm-router/internal/router"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type dataThenErrorReader struct {
	data []byte
	done bool
}

func (reader *dataThenErrorReader) Read(buffer []byte) (int, error) {
	if reader.done {
		return 0, errors.New("upstream stream failed")
	}
	reader.done = true
	return copy(buffer, reader.data), nil
}

// ==================== OpenAI Responses API passthrough (Codex CLI) ====================

func TestResponses_DirectNonStream(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		// Echo body to confirm verbatim forwarding — must include the Responses-only field
		body, _ := io.ReadAll(r.Body)
		var upstreamBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &upstreamBody); err != nil {
			t.Errorf("decode upstream request: %v", err)
			return
		}
		if upstreamBody.Model != "gpt-5" {
			t.Errorf("upstream model = %q, want gpt-5", upstreamBody.Model)
		}
		if !strings.Contains(string(body), `"instructions":"be brief"`) {
			t.Errorf("request body not forwarded verbatim: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_1","object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "oai", Name: "OAI", ResponsesBaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"gpt-5"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"oai/gpt-5","instructions":"be brief","input":"hi"}`
	req := httptest.NewRequest("POST", "/openai/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeResponses(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"object":"response"`) {
		t.Errorf("expected response object, got %s", w.Body.String())
	}
}

func TestResponses_DynamicModelStripsProviderPrefix(t *testing.T) {
	requestModels := make(chan string, 1)
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			requestModels <- "decode-error: " + err.Error()
		} else {
			requestModels <- body.Model
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_dynamic","object":"response","output":[]}`)
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: mockAPI.URL, APIKey: "sk-test",
	}})
	handler := &Handler{
		Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler),
	}
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5.6-sol","input":"hi"}`))
	recorder := httptest.NewRecorder()
	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := <-requestModels; got != "gpt-5.6-sol" {
		t.Fatalf("upstream model = %q, want gpt-5.6-sol", got)
	}
}

func TestResponses_DirectStream(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
		flusher.Flush()
		io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\"}\n\n")
		flusher.Flush()
		io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
		flusher.Flush()
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "oai", Name: "OAI", ResponsesBaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"gpt-5"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"oai/gpt-5","input":"hi","stream":true}`
	req := httptest.NewRequest("POST", "/openai/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeResponses(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	out := w.Body.String()
	// SSE event lines must survive passthrough
	if !strings.Contains(out, "event: response.created") {
		t.Error("stream should preserve event: response.created line")
	}
	if !strings.Contains(out, `"type":"response.output_text.delta"`) {
		t.Error("stream should contain output_text.delta")
	}
	if !strings.Contains(out, "event: response.completed") {
		t.Error("stream should preserve event: response.completed line")
	}
}

func TestResponses_DirectStreamPreservesLargeEvent(t *testing.T) {
	delta := strings.Repeat("x", 300*1024)
	event := "data: " + delta + "\n\n"
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, event)
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix: "oai", ResponsesBaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"gpt-5"},
	}})
	handler := &Handler{
		Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler),
	}

	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi","stream":true}`))
	recorder := httptest.NewRecorder()
	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != event {
		t.Fatalf("large SSE event length = %d, want %d", recorder.Body.Len(), len(event))
	}
}

func TestResponses_DirectStreamReadErrorIsNotSuccess(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(&dataThenErrorReader{
				data: []byte("data: partial\n\n"),
			}),
			Request: request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "oai", ResponsesBaseURL: "http://unused", APIKey: "sk-test", Models: []string{"gpt-5"},
	}})
	handler := &Handler{
		Resolver: resolver, ProxyClient: client, DirectClient: client, Logger: slog.New(slog.DiscardHandler),
	}
	meta := &onellmLog.RequestMeta{}
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi","stream":true}`))
	request = request.WithContext(onellmLog.WithRequestMeta(request.Context(), meta))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if meta.EndReason == "ok" {
		t.Fatalf("stream read error recorded as success: %+v", meta)
	}
	if meta.EndReason != "upstream_error" {
		t.Fatalf("end reason = %q, want upstream_error", meta.EndReason)
	}
}

func TestResponses_DirectStreamHasNoFixedDeadline(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if _, hasDeadline := request.Context().Deadline(); hasDeadline {
			return nil, errors.New("unexpected fixed stream deadline")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: done\n\n")),
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "oai", ResponsesBaseURL: "http://unused", APIKey: "sk-test", Models: []string{"gpt-5"},
	}})
	handler := &Handler{
		Resolver: resolver, ProxyClient: client, DirectClient: client, Logger: slog.New(slog.DiscardHandler),
	}
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi","stream":true}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "data: done\n\n" {
		t.Fatalf("unexpected stream: %q", recorder.Body.String())
	}
}

func TestResponses_ProviderWithoutSupport(t *testing.T) {
	// Provider has no responses_base_url → should 400
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: "http://unused", APIKey: "sk-test", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"ds/m1","input":"hi"}`
	req := httptest.NewRequest("POST", "/openai/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeResponses(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "does not support the Responses API") {
		t.Errorf("expected unsupported error, got %s", w.Body.String())
	}
}

func TestResponses_UnknownModel(t *testing.T) {
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "oai", Name: "OAI", ResponsesBaseURL: "http://unused", APIKey: "sk-test", Models: []string{"gpt-5"}},
	})
	h := &Handler{Resolver: resolver, Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"nope/x","input":"hi"}`
	req := httptest.NewRequest("POST", "/openai/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeResponses(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unknown model") {
		t.Errorf("expected unknown model error, got %s", w.Body.String())
	}
}

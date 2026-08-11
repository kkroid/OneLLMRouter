package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/kkroid/onellm-router/internal/config"
	onellmLog "github.com/kkroid/onellm-router/internal/log"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/kkroid/onellm-router/internal/upstream"
	"github.com/kkroid/onellm-router/internal/usage"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type dataThenErrorReader struct {
	data []byte
	done bool
}

type cancelThenErrorReader struct {
	cancel context.CancelCauseFunc
	data   []byte
	done   bool
}

func (reader *cancelThenErrorReader) Read(buffer []byte) (int, error) {
	if reader.done {
		reader.cancel(upstream.ErrServiceShutdown)
		return 0, errors.New("stream interrupted by service shutdown")
	}
	reader.done = true
	return copy(buffer, reader.data), nil
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

func TestResponsesStreamUsageCollectionPreservesBytes(t *testing.T) {
	stream := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":8,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n"
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, stream)
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{Prefix: "oai", ResponsesBaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"gpt-5"}}})
	writer := &usageRecordWriter{}
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler))
	handler.Usage = usage.NewCollector(writer)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","stream":true,"input":"hi"}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Body.String() != stream {
		t.Fatalf("stream changed:\n%s", recorder.Body.String())
	}
	if len(writer.records) != 1 || writer.records[0].ReasoningTokens == nil || *writer.records[0].ReasoningTokens != 3 || writer.records[0].CacheReadTokens == nil || *writer.records[0].CacheReadTokens != 2 {
		t.Fatalf("records = %+v", writer.records)
	}
}

func TestResponsesNonStreamUsageCollection(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"resp_1","object":"response","output":[],"usage":{"input_tokens":0,"output_tokens":2}}`)
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{Prefix: "oai", ResponsesBaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"gpt-5"}}})
	writer := &usageRecordWriter{}
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler))
	handler.Usage = usage.NewCollector(writer)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi"}`))

	handler.ServeResponses(httptest.NewRecorder(), request)

	if len(writer.records) != 1 || writer.records[0].InputTokens == nil || *writer.records[0].InputTokens != 0 || writer.records[0].OutputTokens == nil || *writer.records[0].OutputTokens != 2 {
		t.Fatalf("records = %+v", writer.records)
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

func TestResponsesStreamServiceShutdownKeepsShutdownCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(&cancelThenErrorReader{
				cancel: cancel,
				data:   []byte("data: partial\n\n"),
			}),
			Request: request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "oai", ResponsesBaseURL: "http://unused", APIKey: "secret", Models: []string{"gpt-5"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	meta := &onellmLog.RequestMeta{}
	ctx = onellmLog.WithRequestMeta(ctx, meta)
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi","stream":true}`)).WithContext(ctx)
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if meta.EndReason != "service_shutdown" {
		t.Fatalf("end reason = %q, want service_shutdown", meta.EndReason)
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

func TestResponsesRetriesWithRebuiltRequest(t *testing.T) {
	var calls int
	var requestBodies []string
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestBodies = append(requestBodies, string(body))
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Errorf("attempt %d Authorization = %q", calls, got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("attempt %d Content-Type = %q", calls, got)
		}
		if calls == 1 {
			w.Header().Set("X-Failed-Attempt", "must-not-leak")
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_recovered","object":"response","output":[]}`)
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: mockAPI.URL, APIKey: "provider-secret",
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5.6-sol","input":"hi"}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if len(requestBodies) != 2 || requestBodies[0] != requestBodies[1] {
		t.Fatalf("request bodies = %#v", requestBodies)
	}
	if strings.Contains(requestBodies[0], "c78/") || !strings.Contains(requestBodies[0], `"model":"gpt-5.6-sol"`) {
		t.Fatalf("provider prefix was not stripped before retries: %s", requestBodies[0])
	}
	if recorder.Header().Get("X-Failed-Attempt") != "" {
		t.Fatalf("response headers = %v", recorder.Header())
	}
}

func TestResponsesRetriesRedirectWithoutFollowing(t *testing.T) {
	var responseCalls int
	var redirectedCalls int
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			redirectedCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		responseCalls++
		if responseCalls == 1 {
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		io.WriteString(w, `{"id":"resp_ok","object":"response","output":[]}`)
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{Prefix: "oai", ResponsesBaseURL: mockAPI.URL, Models: []string{"gpt-5"}}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler),
		newRetryTestExecutorWithStatusCodes(2, []int{http.StatusTemporaryRedirect}))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi"}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || responseCalls != 2 || redirectedCalls != 0 {
		t.Fatalf("status = %d, response calls = %d, redirected calls = %d", recorder.Code, responseCalls, redirectedCalls)
	}
}

func TestResponsesRetriesBufferedBodyReadFailure(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		var body io.ReadCloser = io.NopCloser(strings.NewReader(`{"id":"resp_ok","object":"response","output":[]}`))
		if calls == 1 {
			body = io.NopCloser(&dataThenErrorReader{data: []byte(`{"id":"partial"`)})
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       body,
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{Prefix: "oai", ResponsesBaseURL: "http://unused", Models: []string{"gpt-5"}}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi"}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 || !strings.Contains(recorder.Body.String(), "resp_ok") {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestResponsesStreamRetriesOnlyBeforeSuccessfulHeaders(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("attempt %d Accept = %q", calls, request.Header.Get("Accept"))
		}
		if calls == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{"X-Failed-Attempt": []string{"must-not-leak"}},
				Body:       io.NopCloser(strings.NewReader("temporary")),
				Request:    request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Upstream-Success": []string{"preserved"}},
			Body:       io.NopCloser(&dataThenErrorReader{data: []byte("data: partial\n\n")}),
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{Prefix: "oai", ResponsesBaseURL: "http://unused", Models: []string{"gpt-5"}}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(3))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi","stream":true}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 || recorder.Body.String() != "data: partial\n\n" {
		t.Fatalf("status = %d, calls = %d, body = %q", recorder.Code, calls, recorder.Body.String())
	}
	if recorder.Header().Get("X-Failed-Attempt") != "" || recorder.Header().Get("X-Upstream-Success") != "preserved" {
		t.Fatalf("response headers = %v", recorder.Header())
	}
}

func TestResponsesStreamRetriesCapacityFailureBeforeCommit(t *testing.T) {
	var calls int
	padding := strings.Repeat("x", 48<<10)
	capacityStream :=
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"padding\":\"" + padding + "\"}}\n\n" +
			"event: response.in_progress\ndata: {\"type\":\"response.in_progress\",\"response\":{\"padding\":\"" + padding + "\"}}\n\n" +
			"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}}\n\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(capacityStream)),
				Request:    request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"event: response.created\ndata: {\"type\":\"response.created\"}\n\n" +
					"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"recovered\"}\n\n" +
					"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n",
			)),
			Request: request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: "http://unused", APIKey: "secret", Models: []string{"gpt-5"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	meta := &onellmLog.RequestMeta{}
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5","input":"hi","stream":true}`))
	request = request.WithContext(onellmLog.WithRequestMeta(request.Context(), meta))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"delta":"recovered"`) || strings.Contains(recorder.Body.String(), "currently overloaded") {
		t.Fatalf("unexpected streamed body: %s", recorder.Body.String())
	}
	if meta.UpstreamAttempts != 2 || meta.LastUpstreamStatus != http.StatusServiceUnavailable || meta.LastFailureKind != string(upstream.FailureHTTP) {
		t.Fatalf("retry metadata = %+v", meta)
	}
}

func TestResponsesStreamDoesNotRetryAfterCommitEvent(t *testing.T) {
	commitEvents := map[string]string{
		"output":    "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n",
		"reasoning": "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"partial\"}\n\n",
		"tool":      "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\"}}\n\n",
		"unknown":   "event: response.new_future_event\ndata: {\"type\":\"response.new_future_event\",\"value\":\"keep\"}\n\n",
	}
	capacityEvent := "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"model is at capacity\"}}}\n\n"

	for name, commitEvent := range commitEvents {
		t.Run(name, func(t *testing.T) {
			var calls int
			stream := commitEvent + capacityEvent
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(stream)),
					Request:    request,
				}, nil
			})}
			resolver := router.NewResolver([]router.Provider{{
				Prefix: "c78", ResponsesBaseURL: "http://unused", APIKey: "secret", Models: []string{"gpt-5"},
			}})
			handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(3))
			request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5","input":"hi","stream":true}`))
			recorder := httptest.NewRecorder()

			handler.ServeResponses(recorder, request)

			if recorder.Code != http.StatusOK || calls != 1 || recorder.Body.String() != stream {
				t.Fatalf("status = %d, calls = %d, body = %q", recorder.Code, calls, recorder.Body.String())
			}
		})
	}
}

func TestResponsesStreamHeartbeatsDoNotConsumeProbeEventBudget(t *testing.T) {
	var calls int
	heartbeats := strings.Repeat(": keep-alive\n\n", responsesProbeMaxLifecycleEvents+1)
	capacityStream := heartbeats +
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"model is at capacity\"}}}\n\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		stream := capacityStream
		if calls > 1 {
			stream = "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: "http://unused", APIKey: "secret", Models: []string{"gpt-5"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5","input":"hi","stream":true}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 || strings.Contains(recorder.Body.String(), "server_is_overloaded") {
		t.Fatalf("status = %d, calls = %d, body = %q", recorder.Code, calls, recorder.Body.String())
	}
}

func TestResponsesStreamCapacityReturnsOriginalSSEWhenRetryIsNotConfigured(t *testing.T) {
	const capacityStream = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"slow_down\",\"message\":\"try again later, api_key=secret\"}}\n\n"
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(capacityStream)),
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: "http://unused", APIKey: "secret", Models: []string{"gpt-5"},
	}})
	policy := config.DefaultConfig().Retry
	policy.MaxAttempts = 3
	policy.StatusCodes = []int{http.StatusBadGateway}
	policyExecutor := upstream.NewExecutor(policy)
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), policyExecutor)
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5","input":"hi","stream":true}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if recorder.Body.String() != capacityStream || recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("headers = %v, body = %q", recorder.Header(), recorder.Body.String())
	}
}

func TestResponsesStreamCapacityExhaustionReturnsLastOriginalSSE(t *testing.T) {
	streams := []string{
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"attempt one at capacity\"}}}\n\n",
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"attempt two at capacity\"}}}\n\n",
	}
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		index := calls
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Attempt":    []string{strconv.Itoa(calls)},
			},
			Body:    io.NopCloser(strings.NewReader(streams[index])),
			Request: request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: "http://unused", Models: []string{"gpt-5"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	meta := &onellmLog.RequestMeta{}
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5","input":"hi","stream":true}`))
	request = request.WithContext(onellmLog.WithRequestMeta(request.Context(), meta))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if recorder.Body.String() != streams[1] || recorder.Header().Get("Content-Type") != "text/event-stream" || recorder.Header().Get("X-Attempt") != "2" {
		t.Fatalf("headers = %v, body = %q", recorder.Header(), recorder.Body.String())
	}
	if meta.UpstreamAttempts != 2 || meta.LastUpstreamStatus != http.StatusServiceUnavailable {
		t.Fatalf("retry metadata = %+v", meta)
	}
}

func TestResponsesStreamProbeFailOpenPreservesUnknownEvent(t *testing.T) {
	stream := "event: response.new_future_event\ndata: {\"type\":\"response.new_future_event\",\"value\":\"keep\"}\n\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "c78", ResponsesBaseURL: "http://unused", APIKey: "secret", Models: []string{"gpt-5"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"c78/gpt-5","input":"hi","stream":true}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.String() != stream {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestClassifyResponsesSSEFrameSupportsCRLFAndMultilineData(t *testing.T) {
	frame := "event: response.failed\r\n" +
		"data: {\"type\":\"response.failed\",\r\n" +
		"data: \"response\":{\"error\":{\"code\":\"slow_down\",\"message\":\"try later\"}}}\r\n\r\n"

	decision, summary := classifyResponsesSSEFrame([]byte(frame))

	if decision != responsesProbeCapacity || summary != "try later" {
		t.Fatalf("decision = %d, summary = %q", decision, summary)
	}
}

func TestResponsesPersistentFailurePassesThroughUpstreamError(t *testing.T) {
	const upstreamBody = `{"error":{"message":"unsupported parameter","type":"invalid_request_error","code":"invalid_request_error"}}`
	var calls int
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Request-Id", "responses-request")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{Prefix: "oai", ResponsesBaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"gpt-5"}}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi"}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusForbidden || calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if recorder.Body.String() != upstreamBody || recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("X-Upstream-Request-Id") != "responses-request" {
		t.Fatalf("headers = %v, body = %s", recorder.Header(), recorder.Body.String())
	}
}

func TestResponsesRequestFactoryErrorsStayLocalAndSafe(t *testing.T) {
	const secret = "provider-secret"
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "oai", ResponsesBaseURL: "://" + secret, APIKey: secret, Models: []string{"gpt-5"},
	}})
	handler := NewHandler(resolver, http.DefaultClient, http.DefaultClient, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(`{"model":"oai/gpt-5","input":"hi"}`))
	recorder := httptest.NewRecorder()

	handler.ServeResponses(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) || strings.Contains(recorder.Body.String(), "upstream_retry_") {
		t.Fatalf("unsafe local request error: %s", recorder.Body.String())
	}
}

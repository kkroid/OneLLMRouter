package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kkroid/onellm-router/internal/config"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/kkroid/onellm-router/internal/upstream"
)

// ==================== Test helpers ====================

type testStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *testStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *testStatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func testMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &testStatusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if err := recover(); err != nil {
				http.Error(sw, `{"error":"internal"}`, 500)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

// anthropicSSEPattern verifies the output follows Anthropic SSE spec:
//
//	event: <type>\ndata: <json>\n\n
var anthropicSSEPattern = regexp.MustCompile(`event: (\w+)\ndata: (.+)\n\n`)

// parseSSEEvents parses all SSE events from body and returns a map of event type → count.
func parseSSEEvents(body string) map[string]int {
	counts := map[string]int{}
	matches := anthropicSSEPattern.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		counts[m[1]]++
	}
	return counts
}

// assertAnthropicSSE fails if body doesn't contain the expected Anthropic SSE events.
func assertAnthropicSSE(t *testing.T, body string, events ...string) {
	t.Helper()
	counts := parseSSEEvents(body)
	for _, ev := range events {
		if counts[ev] == 0 {
			t.Errorf("missing required SSE event %q in output (got: %v)", ev, counts)
		}
	}
}

// ==================== Basic error paths ====================

func TestHandler_InvalidJSON(t *testing.T) {
	h := &Handler{Resolver: router.NewResolver([]router.Provider{}), Logger: slog.New(slog.DiscardHandler)}
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_UnknownModel(t *testing.T) {
	h := &Handler{Resolver: router.NewResolver([]router.Provider{
		{Prefix: "ds", Models: []string{"m1"}},
	}), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"unknown/model","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_NoModel(t *testing.T) {
	h := &Handler{Resolver: router.NewResolver([]router.Provider{
		{Prefix: "ds", Models: []string{"m1"}},
	}), Logger: slog.New(slog.DiscardHandler)}
	body := `{"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandler_NonPost(t *testing.T) {
	h := &Handler{Logger: slog.New(slog.DiscardHandler)}
	req := httptest.NewRequest("GET", "/v1/messages", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandler_EmptyBody(t *testing.T) {
	h := &Handler{Logger: slog.New(slog.DiscardHandler)}
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_NonStreamThroughMiddleware(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DeepSeek", BaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"ds/m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", testMiddleware(h))
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_CPPrefixUsesConfiguredAnthropicEndpoint(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix: "cp", BaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"m1"},
	}})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"cp/m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

func TestAnthropicNonStreamRetriesWithRebuiltRequest(t *testing.T) {
	var calls int
	var requestBodies []string
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestBodies = append(requestBodies, string(body))
		if r.Header.Get("x-api-key") != "provider-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("attempt %d headers = %v", calls, r.Header)
		}
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m1\",\"content\":[{\"type\":\"text\",\"text\":\"recovered\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}")
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: mockAPI.URL, APIKey: "provider-secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{\"model\":\"ds/m1\",\"max_tokens\":5,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "recovered") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if calls != 2 || len(requestBodies) != 2 || requestBodies[0] != requestBodies[1] {
		t.Fatalf("calls = %d, request bodies = %#v", calls, requestBodies)
	}
	if strings.Contains(requestBodies[0], "ds/m1") || !strings.Contains(requestBodies[0], `"model":"m1"`) {
		t.Fatalf("model was not rewritten once before retries: %s", requestBodies[0])
	}
}

func TestAnthropicNonStreamBodyLimitBoundary(t *testing.T) {
	for _, test := range []struct {
		name       string
		size       int
		wantStatus int
	}{
		{name: "exact limit", size: 1 << 20, wantStatus: http.StatusOK},
		{name: "over limit", size: (1 << 20) + 1, wantStatus: http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			payload := strings.Repeat("x", test.size)
			mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, payload)
			}))
			defer mockAPI.Close()
			resolver := router.NewResolver([]router.Provider{{
				Prefix: "ds", BaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"m1"},
			}})
			handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(3))
			request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"messages":[]}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if calls != 1 {
				t.Fatalf("upstream calls = %d, want 1", calls)
			}
			if test.wantStatus == http.StatusOK && recorder.Body.Len() != test.size {
				t.Fatalf("body length = %d, want %d", recorder.Body.Len(), test.size)
			}
		})
	}
}

func TestAnthropicRetriesAllNon2xxStatuses(t *testing.T) {
	statuses := []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusBadGateway, http.StatusOK}
	var calls int
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := statuses[calls]
		calls++
		w.WriteHeader(status)
		if status == http.StatusOK {
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[],"stop_reason":"end_turn","usage":{}}`)
		}
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(4))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || calls != 4 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestAnthropicFinalFailureUsesSafeProtocolError(t *testing.T) {
	const secret = "provider-secret"
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"x-api-key":"provider-secret","message":"denied"}`)
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: mockAPI.URL, APIKey: secret, Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "upstream request failed") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unsafe final response: headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
}

func TestAnthropicStreamRetriesBeforeSuccessfulHeaders(t *testing.T) {
	var calls int
	var acceptHeaders []string
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		acceptHeaders = append(acceptHeaders, r.Header.Get("Accept"))
		if calls == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"stream":true,"messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if len(acceptHeaders) != 2 || acceptHeaders[0] != "text/event-stream" || acceptHeaders[1] != "text/event-stream" {
		t.Fatalf("Accept headers = %#v", acceptHeaders)
	}
	if counts := parseSSEEvents(recorder.Body.String()); counts["message_start"] != 1 {
		t.Fatalf("SSE events = %v, body = %s", counts, recorder.Body.String())
	}
}

func TestAnthropicTransportFailureRecovers(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[],"stop_reason":"end_turn","usage":{}}`)),
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: "http://unused", APIKey: "secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
}

func TestAnthropicRequestFactoryErrorsStayLocal(t *testing.T) {
	const secret = "provider-secret"
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: "://" + secret, APIKey: secret, Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, http.DefaultClient, http.DefaultClient, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "upstream_retry_exhausted") {
		t.Fatalf("local request error was reported as exhausted upstream retry: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("local request error leaked provider secret: %s", recorder.Body.String())
	}
}

func TestAnthropicAcceptedStreamFailureDoesNotRetry(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(&dataThenErrorReader{
				data: []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"),
			}),
			Request: request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: "http://unused", APIKey: "secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"stream":true,"messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
}

// ==================== External SSE passthrough (Anthropic format) ====================

func TestHandler_ExternalSSEPassthrough(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		// Anthropic API returns properly formatted SSE with event: lines
		io.WriteString(w, "event: message_start\n")
		io.WriteString(w, "data: {\"type\":\"message_start\"}\n\n")
		f.Flush()
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, "data: {\"type\":\"content_block_start\"}\n\n")
		f.Flush()
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"ds/m1","max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	assertAnthropicSSE(t, w.Body.String(), "message_start", "content_block_start")
}

func TestExternalPassthrough_AnyFormat(t *testing.T) {
	// External APIs might send SSE with or without event: lines.
	// Our passthrough handler should NOT add/remove event: lines — just pass through.
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		// Some Anthropic endpoints send WITH event: lines
		io.WriteString(w, "event: message_start\n")
		io.WriteString(w, "data: {}\n\n")
		f.Flush()
		// Some send WITHOUT event: lines
		io.WriteString(w, "data: {\"type\":\"ping\"}\n\n")
		f.Flush()
		// Back to with event
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
		f.Flush()
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"ds/m1","max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", testMiddleware(h))
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// External passthrough preserves input format exactly
	out := w.Body.String()
	if !strings.Contains(out, "event: message_start") {
		t.Error("passthrough should preserve event: message_start line")
	}
	if !strings.Contains(out, "data: {\"type\":\"ping\"}") {
		t.Error("passthrough should preserve bare data: line")
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Error("passthrough should preserve event: message_stop line")
	}
}

// ==================== Panic recover + StatusWriter ====================

func TestHandler_PanicRecover(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("oops")
	})
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"x/x","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", testMiddleware(panicHandler))
	mux.ServeHTTP(w, req)
	if w.Code != 500 {
		t.Errorf("expected 500 after panic, got %d", w.Code)
	}
}

func TestStatusWriter_FlushDelegates(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &testStatusWriter{ResponseWriter: rec, status: 200}
	sw.Flush()
}

func TestStatusWriter_NoFlushUnderlying(t *testing.T) {
	type noFlush struct{ http.ResponseWriter }
	sw := &testStatusWriter{ResponseWriter: &noFlush{httptest.NewRecorder()}, status: 200}
	sw.Flush()
}

func TestStatusWriter_CapturesCode(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &testStatusWriter{ResponseWriter: rec, status: 200}
	http.Error(sw, "not found", 404)
	if sw.status != 404 {
		t.Errorf("expected 404, got %d", sw.status)
	}
}

// ==================== System prompt: string and array ====================

func TestHandler_SystemStringFormat(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"ds/m1","max_tokens":5,"system":"you are helpful","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("system string: %d %s", w.Code, w.Body.String())
	}
}

func TestHandler_SystemArrayFormat(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"ds/m1","max_tokens":5,"system":[{"type":"text","text":"you are helpful"}],"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("system array: %d %s", w.Code, w.Body.String())
	}
}

// ==================== Context cancellation ====================

func TestHandler_ContextCancel(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"ds/m1","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
}

func TestExternalRequestUsesTimeoutOverride(t *testing.T) {
	t.Setenv("ONELLM_EXTERNAL_REQUEST_TIMEOUT_MS", "25")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: "http://unused", Models: []string{"m1"},
	}})
	handler := &Handler{Resolver: resolver, ProxyClient: client, DirectClient: client, Logger: slog.New(slog.DiscardHandler)}
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
	recorder := httptest.NewRecorder()
	started := time.Now()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("timeout override took %s", elapsed)
	}
}

func TestExternalStreamUsesFirstEventTimeoutOverride(t *testing.T) {
	t.Setenv("ONELLM_STREAM_FIRST_EVENT_TIMEOUT_MS", "20")
	t.Setenv("ONELLM_EXTERNAL_STREAM_TIMEOUT_MS", "5000")
	reader, writer := io.Pipe()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       reader,
			Request:    request,
		}, nil
	})}
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: "http://unused", Models: []string{"m1"},
	}})
	handler := &Handler{Resolver: resolver, ProxyClient: client, DirectClient: client, Logger: slog.New(slog.DiscardHandler)}
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"ds/m1","max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, request)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		writer.Close()
		<-done
		t.Fatal("stream did not honor first-event timeout")
	}
	writer.Close()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("unexpected stalled-stream output: %q", recorder.Body.String())
	}
}

func newRetryTestExecutor(maxAttempts int) *upstream.Executor {
	return upstream.NewExecutor(config.RetryConfig{
		Enabled:         true,
		MaxAttempts:     maxAttempts,
		InitialDelay:    config.Duration(time.Nanosecond),
		MaxDelay:        config.Duration(time.Nanosecond),
		MaxElapsed:      config.Duration(time.Second),
		HonorRetryAfter: true,
	})
}

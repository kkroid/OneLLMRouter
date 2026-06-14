package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkroid/onecc-router/internal/auth"
	"github.com/kkroid/onecc-router/internal/router"
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

func tokenManagerForTest(t *testing.T) *auth.TokenManager {
	t.Helper()
	dir := t.TempDir()
	tf := filepath.Join(dir, "token")
	os.WriteFile(tf, []byte("ghu_test_token"), 0600)
	tm, err := auth.NewTokenManager(tf, "")
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// anthropicSSEPattern verifies the output follows Anthropic SSE spec:
//   event: <type>\ndata: <json>\n\n
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

// ==================== Copilot SSE (OpenAI format) → Anthropic translation ====================

func TestHandler_CopilotStream_Basic(t *testing.T) {
	var copilotFlushes int32
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		// OpenAI format from Copilot: data: only, no event: lines
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 1)
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 1)
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 1)
		io.WriteString(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer mockAPI.Close()

	tm := tokenManagerForTest(t)
	tm.SetTestToken("test-copilot-token", mockAPI.URL)
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8"}},
	})
	h := &Handler{Resolver: resolver, TokenManager: tm, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"cp/claude-opus-4.8","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", testMiddleware(h))
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Output MUST be Anthropic SSE with event: lines
	assertAnthropicSSE(t, w.Body.String(), "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop")
}

func TestHandler_CopilotStream_ToolCalls(t *testing.T) {
	var copilotFlushes int32
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		// Chunk 1: role only
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		f.Flush()
		// Chunk 2: text
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Let me check.\"}}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 2)
		// Chunk 3: tool call ID only (delayed name)
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_abc\"}]}}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 1)
		// Chunk 4: tool call name + args
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"bj\\\"}\"}}]}}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 1)
		// Chunk 5: finish
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		f.Flush()
		atomic.AddInt32(&copilotFlushes, 1)
		io.WriteString(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer mockAPI.Close()

	tm := tokenManagerForTest(t)
	tm.SetTestToken("test-token", mockAPI.URL)
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8"}},
	})
	h := &Handler{Resolver: resolver, TokenManager: tm, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"cp/claude-opus-4.8","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"weather in beijing"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", testMiddleware(h))
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// MUST have event: lines for ALL Anthropic SSE events
	assertAnthropicSSE(t, w.Body.String(),
		"message_start", "content_block_start", "content_block_delta",
		"content_block_stop", "message_delta", "message_stop")
	if !strings.Contains(w.Body.String(), "get_weather") {
		t.Error("missing tool name get_weather")
	}
}

// ==================== SSE format validation ====================

func TestSSEOutputFormat_AllEventsHaveEventLine(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		// OpenAI format (no event: lines) — this is what Copilot actually sends
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		f.Flush()
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		f.Flush()
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		f.Flush()
		io.WriteString(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer mockAPI.Close()

	tm := tokenManagerForTest(t)
	tm.SetTestToken("test-token", mockAPI.URL)
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8"}},
	})
	h := &Handler{Resolver: resolver, TokenManager: tm, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"cp/claude-opus-4.8","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", testMiddleware(h))
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	bodyStr := w.Body.String()
	// Verify every data line is preceded by an event line
	lines := strings.Split(bodyStr, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			if i == 0 || !strings.HasPrefix(lines[i-1], "event: ") {
				// Allow "data: [DONE]" without event
				if !strings.Contains(line, "[DONE]") {
					t.Errorf("data line without preceding event line at line %d: %s", i+1, line[:min(80, len(line))])
				}
			}
		}
	}

	// Count events
	counts := parseSSEEvents(bodyStr)
	for _, required := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
		if counts[required] == 0 {
			t.Errorf("missing required event type: %s", required)
		}
	}
	t.Logf("SSE events: %v", counts)
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

// ==================== Copilot API error ====================

func TestHandler_CopilotError(t *testing.T) {
	var baseURL string
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/token") {
			w.Write([]byte(`{"token":"tid=test;exp=2000000000","endpoints":{"api":"` + baseURL + `"}}`))
			return
		}
		w.WriteHeader(500)
	}))
	baseURL = mockAPI.URL
	defer mockAPI.Close()
	tm := tokenManagerForTest(t)
	tm.SetTestToken("test-token", mockAPI.URL)
	resolver := router.NewResolver([]router.Provider{
		{Prefix: "cp", Name: "Copilot", Models: []string{"claude-opus-4.8"}},
	})
	h := &Handler{Resolver: resolver, TokenManager: tm, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}
	body := `{"model":"cp/claude-opus-4.8","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errMap, _ := resp["error"].(map[string]interface{}); errMap == nil {
		t.Error("expected error response from copilot API failure")
	}
}

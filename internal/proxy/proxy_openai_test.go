package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kkroid/onellm-router/internal/router"
	"github.com/kkroid/onellm-router/internal/upstream"
)

// ==================== OpenAI direct passthrough routing ====================

func TestOpenAI_DirectNonStream(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, OpenAIBaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"ds/m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeOpenAI(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["object"] != "chat.completion" {
		t.Errorf("expected chat.completion, got %v", resp["object"])
	}
	cs, _ := resp["choices"].([]interface{})
	m, _ := cs[0].(map[string]interface{})["message"].(map[string]interface{})
	if m["content"] != "Hello" {
		t.Errorf("expected Hello, got %v", m["content"])
	}
}

func TestOpenAI_DirectStream(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		flusher.Flush()
		io.WriteString(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		flusher.Flush()
		io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, OpenAIBaseURL: mockAPI.URL, APIKey: "sk-test", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"ds/m1","max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeOpenAI(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	out := w.Body.String()
	if !strings.Contains(out, `"object":"chat.completion.chunk"`) {
		t.Error("stream should contain OpenAI-format chunk")
	}
	if !strings.Contains(out, `"delta":{"role":"assistant"}`) {
		t.Error("stream should contain role delta")
	}
	if !strings.Contains(out, `data: [DONE]`) {
		t.Error("stream should contain [DONE] marker")
	}
}

func TestOpenAI_TranslateFallback(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("translate path should hit /messages, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_x","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"ds/m1","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeOpenAI(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["object"] != "chat.completion" {
		t.Errorf("expected chat.completion, got %v", resp["object"])
	}
	cs, _ := resp["choices"].([]interface{})
	m, _ := cs[0].(map[string]interface{})["message"].(map[string]interface{})
	if m["content"] != "hello" {
		t.Errorf("expected hello, got %v", m["content"])
	}
}

func TestOpenAI_RouteAlias(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{
		{Prefix: "ds", Name: "DS", BaseURL: mockAPI.URL, OpenAIBaseURL: mockAPI.URL, APIKey: "k", Models: []string{"m1"}},
	})
	h := &Handler{Resolver: resolver, ProxyClient: mockAPI.Client(), DirectClient: mockAPI.Client(), Logger: slog.New(slog.DiscardHandler)}

	body := `{"model":"ds/m1","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/openai/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeOpenAI(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAIDirectRetriesWithRebuiltRequest(t *testing.T) {
	var calls int
	var requestBodies []string
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestBodies = append(requestBodies, string(requestBody))
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Errorf("attempt %d Authorization = %q", calls, got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("attempt %d Content-Type = %q", calls, got)
		}
		if calls == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"chat_1","object":"chat.completion","model":"m1","choices":[],"usage":{}}`)
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", OpenAIBaseURL: mockAPI.URL, APIKey: "provider-secret", Models: []string{"m1[1m]"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1[1m]","messages":[{"role":"user","content":"hi"}]}`))
	recorder := httptest.NewRecorder()

	handler.ServeOpenAI(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if calls != 2 || len(requestBodies) != 2 || requestBodies[0] != requestBodies[1] {
		t.Fatalf("calls = %d, request bodies = %#v", calls, requestBodies)
	}
	if strings.Contains(requestBodies[0], "[1m]") || !strings.Contains(requestBodies[0], `"model":"m1"`) {
		t.Fatalf("direct model was not stripped before retries: %s", requestBodies[0])
	}
}

func TestOpenAITranslateRetriesWithRebuiltRequest(t *testing.T) {
	var calls int
	var requestBodies []string
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestBodies = append(requestBodies, string(requestBody))
		if got := r.Header.Get("x-api-key"); got != "provider-secret" {
			t.Errorf("attempt %d x-api-key = %q", calls, got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("attempt %d Content-Type = %q", calls, got)
		}
		if calls == 1 {
			http.Error(w, "temporary", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"recovered"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer mockAPI.Close()

	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: mockAPI.URL, APIKey: "provider-secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","messages":[{"role":"user","content":"hi"}]}`))
	recorder := httptest.NewRecorder()

	handler.ServeOpenAI(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "recovered") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if calls != 2 || len(requestBodies) != 2 || requestBodies[0] != requestBodies[1] {
		t.Fatalf("calls = %d, request bodies = %#v", calls, requestBodies)
	}
	if strings.Contains(requestBodies[0], "ds/m1") || !strings.Contains(requestBodies[0], `"model":"m1"`) {
		t.Fatalf("translated model was not rewritten before retries: %s", requestBodies[0])
	}
}

func TestOpenAIChatPersistentFailuresUseProtocolError(t *testing.T) {
	for _, test := range []struct {
		name          string
		openAIBaseURL bool
	}{
		{name: "direct", openAIBaseURL: true},
		{name: "translate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const secret = "provider-secret"
			var calls int
			mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"x-api-key":"provider-secret","message":"denied"}`)
			}))
			defer mockAPI.Close()

			provider := router.Provider{Prefix: "ds", BaseURL: mockAPI.URL, APIKey: secret, Models: []string{"m1"}}
			if test.openAIBaseURL {
				provider.OpenAIBaseURL = mockAPI.URL
			}
			resolver := router.NewResolver([]router.Provider{provider})
			handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","messages":[]}`))
			recorder := httptest.NewRecorder()

			handler.ServeOpenAI(recorder, request)

			if recorder.Code != http.StatusForbidden || calls != 1 {
				t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
			}
			var payload struct {
				Error struct {
					Message string  `json:"message"`
					Type    string  `json:"type"`
					Param   *string `json:"param"`
					Code    string  `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Type != "upstream_error" || payload.Error.Param != nil || payload.Error.Code != "upstream_retry_skipped" {
				t.Fatalf("payload = %+v", payload)
			}
			if strings.Contains(recorder.Body.String(), secret) || !strings.Contains(payload.Error.Message, "Attempts: 1") {
				t.Fatalf("unsafe or incomplete error: %s", recorder.Body.String())
			}
		})
	}
}

func TestOpenAIChatStreamRetryBoundary(t *testing.T) {
	for _, test := range []struct {
		name          string
		openAIBaseURL bool
	}{
		{name: "direct", openAIBaseURL: true},
		{name: "translate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			var bodies []string
			var hadDeadline bool
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if _, ok := request.Context().Deadline(); ok {
					hadDeadline = true
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				bodies = append(bodies, string(body))
				if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("Accept") != "text/event-stream" {
					t.Errorf("attempt %d headers = %v", calls, request.Header)
				}
				if test.openAIBaseURL && request.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("attempt %d Authorization = %q", calls, request.Header.Get("Authorization"))
				}
				if !test.openAIBaseURL && request.Header.Get("x-api-key") != "secret" {
					t.Errorf("attempt %d x-api-key = %q", calls, request.Header.Get("x-api-key"))
				}
				if calls == 1 {
					return &http.Response{
						StatusCode: http.StatusBadGateway,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("temporary")),
						Request:    request,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body: io.NopCloser(&dataThenErrorReader{
						data: []byte("data: partial\n\n"),
					}),
					Request: request,
				}, nil
			})}
			provider := router.Provider{Prefix: "ds", BaseURL: "http://unused", APIKey: "secret", Models: []string{"m1"}}
			if test.openAIBaseURL {
				provider.OpenAIBaseURL = "http://unused"
			}
			resolver := router.NewResolver([]router.Provider{provider})
			handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler), newRetryTestExecutor(3))
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","stream":true,"messages":[]}`))
			recorder := httptest.NewRecorder()

			handler.ServeOpenAI(recorder, request)

			if recorder.Code != http.StatusOK || calls != 2 {
				t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
			}
			if len(bodies) != 2 || bodies[0] != bodies[1] || recorder.Body.String() != "data: partial\n\n" {
				t.Fatalf("request bodies = %#v, response = %q", bodies, recorder.Body.String())
			}
			if hadDeadline {
				t.Fatal("successful stream retained a fixed request deadline")
			}
		})
	}
}

func TestOpenAITranslateInvalidSuccessJSONDoesNotRetry(t *testing.T) {
	var calls int
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "not-json")
	}))
	defer mockAPI.Close()
	resolver := router.NewResolver([]router.Provider{{
		Prefix: "ds", BaseURL: mockAPI.URL, APIKey: "secret", Models: []string{"m1"},
	}})
	handler := NewHandler(resolver, mockAPI.Client(), mockAPI.Client(), slog.New(slog.DiscardHandler), newRetryTestExecutor(3))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","messages":[]}`))
	recorder := httptest.NewRecorder()

	handler.ServeOpenAI(recorder, request)

	if recorder.Code != http.StatusInternalServerError || calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "parse response") {
		t.Fatalf("unexpected protocol error: %s", recorder.Body.String())
	}
}

func TestOpenAIChatCancellationDoesNotWriteError(t *testing.T) {
	for _, test := range []struct {
		name          string
		openAIBaseURL bool
		cause         error
	}{
		{name: "direct client cancel", openAIBaseURL: true, cause: context.Canceled},
		{name: "direct service shutdown", openAIBaseURL: true, cause: upstream.ErrServiceShutdown},
		{name: "translate client cancel", cause: context.Canceled},
		{name: "translate service shutdown", cause: upstream.ErrServiceShutdown},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := router.Provider{Prefix: "ds", BaseURL: "http://unused", APIKey: "secret", Models: []string{"m1"}}
			if test.openAIBaseURL {
				provider.OpenAIBaseURL = "http://unused"
			}
			resolver := router.NewResolver([]router.Provider{provider})
			handler := NewHandler(resolver, http.DefaultClient, http.DefaultClient, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(test.cause)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","messages":[]}`)).WithContext(ctx)
			recorder := httptest.NewRecorder()

			handler.ServeOpenAI(recorder, request)

			if recorder.Body.Len() != 0 {
				t.Fatalf("canceled request wrote response: %s", recorder.Body.String())
			}
		})
	}
}

func TestOpenAIChatUsesPerAttemptTimeout(t *testing.T) {
	t.Setenv("ONELLM_OPENAI_REQUEST_TIMEOUT_MS", "25")
	for _, test := range []struct {
		name          string
		openAIBaseURL bool
	}{
		{name: "direct", openAIBaseURL: true},
		{name: "translate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}
			provider := router.Provider{Prefix: "ds", BaseURL: "http://unused", APIKey: "secret", Models: []string{"m1"}}
			if test.openAIBaseURL {
				provider.OpenAIBaseURL = "http://unused"
			}
			resolver := router.NewResolver([]router.Provider{provider})
			handler := NewHandler(resolver, client, client, slog.New(slog.DiscardHandler))
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","messages":[]}`))
			recorder := httptest.NewRecorder()

			handler.ServeOpenAI(recorder, request)

			if recorder.Code != http.StatusGatewayTimeout || calls != 1 {
				t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
			}
		})
	}
}

func TestOpenAIChatRequestFactoryErrorsStayLocal(t *testing.T) {
	for _, test := range []struct {
		name          string
		openAIBaseURL bool
	}{
		{name: "direct", openAIBaseURL: true},
		{name: "translate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const secret = "provider-secret"
			provider := router.Provider{Prefix: "ds", BaseURL: "://" + secret, APIKey: secret, Models: []string{"m1"}}
			if test.openAIBaseURL {
				provider.OpenAIBaseURL = "://" + secret
			}
			resolver := router.NewResolver([]router.Provider{provider})
			handler := NewHandler(resolver, http.DefaultClient, http.DefaultClient, slog.New(slog.DiscardHandler), newRetryTestExecutor(2))
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"ds/m1","messages":[]}`))
			recorder := httptest.NewRecorder()

			handler.ServeOpenAI(recorder, request)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var payload struct {
				Error struct {
					Type string `json:"type"`
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Type != "api_error" || payload.Error.Code != "" {
				t.Fatalf("factory error used upstream protocol: %+v", payload)
			}
			if strings.Contains(recorder.Body.String(), secret) {
				t.Fatalf("local request error leaked provider secret: %s", recorder.Body.String())
			}
		})
	}
}

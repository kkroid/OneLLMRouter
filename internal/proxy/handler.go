package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kkroid/onellm-router/internal/auth"
	onellmLog "github.com/kkroid/onellm-router/internal/log"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/kkroid/onellm-router/internal/translate"
)

// Copilot HTTP headers required by the API.
var copilotHeaders = map[string]string{
	"copilot-integration-id":  "vscode-chat",
	"user-agent":              "GitHubCopilotChat/0.26.7",
	"editor-version":          "vscode/1.104.1",
	"editor-plugin-version":   "copilot-chat/0.26.7",
}

// Handler dispatches Anthropic API requests to providers.
type Handler struct {
	Resolver     *router.Resolver
	TokenManager *auth.TokenManager
	ProxyClient  *http.Client // requests through SOCKS5 proxy
	DirectClient *http.Client // requests without proxy
	Logger       *slog.Logger
}

// NewHandler creates a proxy Handler.
func NewHandler(resolver *router.Resolver, tokenMgr *auth.TokenManager, proxyClient, directClient *http.Client, logger *slog.Logger) *Handler {
	return &Handler{
		Resolver:     resolver,
		TokenManager: tokenMgr,
		ProxyClient:  proxyClient,
		DirectClient: directClient,
		Logger:       logger,
	}
}

func (h *Handler) clientFor(p *router.Provider) *http.Client {
	if p.ShouldUseProxy() {
		return h.ProxyClient
	}
	return h.DirectClient
}

// ServeHTTP implements the unified /v1/messages handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse request body
	var body translate.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	fullModel := body.Model

	// No model specified → use first Copilot model as default
	if fullModel == "" {
		cp := h.Resolver.CopilotProvider()
		if cp != nil && len(cp.Models) > 0 {
			h.copilotHandler(w, r, &body, cp.Models[0])
			return
		}
		h.writeError(w, http.StatusBadRequest, "no model specified")
		return
	}

	// Resolve model → provider
	resolved := h.Resolver.Resolve(fullModel)
	if resolved == nil {
		models := h.Resolver.AllModelIDs()
		h.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown model: %s. Available: %s", fullModel, strings.Join(models, ", ")))
		return
	}

	// Attach request metadata to context for logging
	meta := onellmLog.RequestMetaFromContext(r.Context())
	meta.Model = fullModel
	meta.Provider = resolved.Provider.Prefix
	meta.Stream = body.Stream
	meta.MaxTokens = body.MaxTokens

	// Track TTFB via response writer
	w = &ttfbWriter{ResponseWriter: w, meta: meta}

	if resolved.Provider.Prefix == "cp" {
		h.copilotHandler(w, r, &body, resolved.Model)
	} else {
		h.externalHandler(w, r, &body, resolved)
	}
}

// copilotHandler proxies requests to GitHub Copilot API.
func (h *Handler) copilotHandler(w http.ResponseWriter, r *http.Request, body *translate.AnthropicRequest, model string) {
	// Inject resolved model name
	body.Model = model

	openaiReq, err := translate.TranslateRequest(body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "translate request: "+err.Error())
		return
	}

	token, err := h.TokenManager.GetToken()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "get token: "+err.Error())
		return
	}

	apiBase := h.TokenManager.GetAPIBase()
	url := apiBase + "/chat/completions"

	reqBody, _ := json.Marshal(openaiReq)
	timeout := 60 * time.Second
	if body.Stream {
		timeout = 300 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "create request: "+err.Error())
		return
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range copilotHeaders {
		req.Header.Set(k, v)
	}

	if !body.Stream {
		// Non-streaming
		resp, err := h.ProxyClient.Do(req)
		if err != nil {
			h.writeError(w, http.StatusBadGateway, "copilot api: "+err.Error())
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
		if resp.StatusCode != 200 {
			h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("copilot api error %d: %s", resp.StatusCode, string(respBody)))
			return
		}

		var openaiResp translate.OpenAIResponse
		if err := json.Unmarshal(respBody, &openaiResp); err != nil {
			h.writeError(w, http.StatusInternalServerError, "parse copilot response: "+err.Error())
			return
		}

		anthropicResp := translate.TranslateResponse(&openaiResp, body.Model)
		h.writeJSON(w, http.StatusOK, anthropicResp)
		return
	}

	// Streaming
	req.Header.Set("Accept", "text/event-stream")
	resp, err := h.ProxyClient.Do(req)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "copilot api stream: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("copilot api stream error %d: %s", resp.StatusCode, string(respBody)))
		return
	}

	h.streamCopilotResponse(w, resp.Body, body.Model)
}

// streamCopilotResponse translates an OpenAI SSE stream to Anthropic SSE.
func (h *Handler) streamCopilotResponse(w http.ResponseWriter, body io.Reader, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ctx := &translate.StreamContext{
		MessageStartSent: false,
		MessageID:        translate.GenerateMessageID(),
		Model:            model,
		ContentBlockIdx:  0,
		ContentBlockOpen: false,
		ActiveToolIdx:    -1,
		ToolCalls:        make(map[int]*translate.ToolCallState),
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024) // 64KB buffer

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk translate.OpenAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		events, err := translate.TranslateStreamChunk(&chunk, ctx)
		if err != nil {
			continue
		}

		for _, ev := range events {
			evJSON, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, evJSON)
			flusher.Flush()
		}
	}
}

// externalHandler proxies requests to external Anthropic-compatible APIs (direct passthrough).
func (h *Handler) externalHandler(w http.ResponseWriter, r *http.Request, body *translate.AnthropicRequest, resolved *router.ResolveResult) {
	body.Model = resolved.Model

	baseURL := strings.TrimRight(resolved.Provider.BaseURL, "/")
	url := baseURL + "/messages"
	apiKey := resolved.Provider.APIKey

	reqBody, _ := json.Marshal(body)
	timeout := 60 * time.Second
	if body.Stream {
		timeout = 300 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "create request: "+err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	if !body.Stream {
		resp, err := h.clientFor(resolved.Provider).Do(req)
		if err != nil {
			h.writeError(w, http.StatusBadGateway, "external api: "+err.Error())
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 400 {
			h.writeError(w, resp.StatusCode, string(respBody))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBody)
		return
	}

	// Streaming — direct SSE passthrough
	resp, err := h.clientFor(resolved.Provider).Do(req)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "external api stream: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		h.writeError(w, resp.StatusCode, string(respBody))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Fprintf(w, "%s\n", line)
		} else {
			fmt.Fprintf(w, "\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// Helper methods

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	if tw, ok := w.(*ttfbWriter); ok && tw.meta != nil {
		tw.meta.Error = message
	}
	h.writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": message,
		},
	})
}


// ttfbWriter captures time-to-first-byte for logging.
type ttfbWriter struct {
	http.ResponseWriter
	meta     *onellmLog.RequestMeta
	firstWrite bool
}

func (tw *ttfbWriter) Write(b []byte) (int, error) {
	if !tw.firstWrite {
		tw.firstWrite = true
		if tw.meta != nil {
			tw.meta.MarkFirstByte()
		}
	}
	return tw.ResponseWriter.Write(b)
}

func (tw *ttfbWriter) WriteHeader(code int) {
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *ttfbWriter) Flush() {
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ServeOpenAI handles OpenAI-format /v1/chat/completions requests.
func (h *Handler) ServeOpenAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body translate.OpenAIRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	fullModel := body.Model
	if fullModel == "" {
		cp := h.Resolver.CopilotProvider()
		if cp != nil && len(cp.Models) > 0 {
			body.Model = cp.Models[0]
			h.openaiDirectHandler(w, r, &body, &router.ResolveResult{Provider: cp, Model: cp.Models[0]})
			return
		}
		h.writeError(w, http.StatusBadRequest, "no model specified")
		return
	}

	resolved := h.Resolver.Resolve(fullModel)
	if resolved == nil {
		models := h.Resolver.AllModelIDs()
		h.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown model: %s. Available: %s", fullModel, strings.Join(models, ", ")))
		return
	}

	meta := onellmLog.RequestMetaFromContext(r.Context())
	meta.Model = fullModel
	meta.Provider = resolved.Provider.Prefix
	meta.Stream = body.Stream
	meta.MaxTokens = body.MaxTokens
	w = &ttfbWriter{ResponseWriter: w, meta: meta}

	if resolved.Provider.Prefix == "cp" || resolved.Provider.OpenAIBaseURL != "" {
		body.Model = resolved.Model
		h.openaiDirectHandler(w, r, &body, resolved)
	} else {
		h.openaiTranslateHandler(w, r, &body, resolved)
	}
}

func (h *Handler) openaiDirectHandler(w http.ResponseWriter, r *http.Request, body *translate.OpenAIRequest, resolved *router.ResolveResult) {
	body.Model = resolved.Model

	var url string
	var client *http.Client

	if resolved.Provider.Prefix == "cp" {
		apiBase := h.TokenManager.GetAPIBase()
		url = apiBase + "/chat/completions"
		client = h.ProxyClient
	} else {
		url = strings.TrimRight(resolved.Provider.OpenAIBaseURL, "/") + "/v1/chat/completions"
		client = h.clientFor(resolved.Provider)
		// OpenAI API doesn't support anthropic [1m] suffix — strip from model name
		body.Model = strings.TrimSuffix(body.Model, "[1m]")
	}

	reqBody, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "create request: "+err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if resolved.Provider.Prefix == "cp" {
		token, err := h.TokenManager.GetToken()
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "get token: "+err.Error())
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		for k, v := range copilotHeaders {
			req.Header.Set(k, v)
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+resolved.Provider.APIKey)
	}

	if !body.Stream {
		resp, err := client.Do(req)
		if err != nil {
			h.writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
			return
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			h.Logger.Warn("read response body", "error", err)
			h.writeError(w, http.StatusBadGateway, "read response failed: "+err.Error())
			return
		}
		if resp.StatusCode >= 400 {
			h.writeError(w, resp.StatusCode, string(respBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
		return
	}

	// Streaming: byte-for-byte SSE passthrough
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream stream: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		h.writeError(w, resp.StatusCode, string(respBody))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Fprintf(w, "%s\n", line)
		} else {
			fmt.Fprintf(w, "\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		h.Logger.Warn("openai direct stream scanner", "error", err)
	}
}

// openaiTranslateHandler translates OpenAI->Anthropic, proxies, then reverses.
// Fallback for providers without openai_base_url.
func (h *Handler) openaiTranslateHandler(w http.ResponseWriter, r *http.Request, body *translate.OpenAIRequest, resolved *router.ResolveResult) {
	body.Model = resolved.Model

	anthropicReq, err := translate.ReverseTranslateRequest(body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "translate request: "+err.Error())
		return
	}

	baseURL := strings.TrimRight(resolved.Provider.BaseURL, "/")
	url := baseURL + "/messages"
	apiKey := resolved.Provider.APIKey

	reqBody, _ := json.Marshal(anthropicReq)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "create request: "+err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := h.clientFor(resolved.Provider)

	if !body.Stream {
		resp, err := client.Do(req)
		if err != nil {
			h.writeError(w, http.StatusBadGateway, "external api: "+err.Error())
			return
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			h.Logger.Warn("read response body", "error", err)
			h.writeError(w, http.StatusBadGateway, "read response failed: "+err.Error())
			return
		}
		if resp.StatusCode >= 400 {
			h.writeError(w, resp.StatusCode, string(respBody))
			return
		}

		var anthropicResp translate.AnthropicResponse
		if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
			h.writeError(w, http.StatusInternalServerError, "parse response: "+err.Error())
			return
		}
		openaiResp := translate.ReverseTranslateResponse(&anthropicResp, body.Model)
		h.writeJSON(w, http.StatusOK, openaiResp)
		return
	}

	// Streaming: Anthropic SSE passthrough
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "external api: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		h.writeError(w, resp.StatusCode, string(respBody))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Fprintf(w, "%s\n", line)
		} else {
			fmt.Fprintf(w, "\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		h.Logger.Warn("openai translate stream scanner", "error", err, "request_id", onellmLog.RequestIDFromContext(r.Context()))
	}
}

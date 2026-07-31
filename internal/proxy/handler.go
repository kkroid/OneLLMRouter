package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kkroid/onellm-router/internal/catalog"
	"github.com/kkroid/onellm-router/internal/config"
	onellmLog "github.com/kkroid/onellm-router/internal/log"
	"github.com/kkroid/onellm-router/internal/router"
	"github.com/kkroid/onellm-router/internal/translate"
	"github.com/kkroid/onellm-router/internal/upstream"
)

// Handler dispatches Anthropic API requests to providers.
type Handler struct {
	Resolver     *router.Resolver
	ProxyClient  *http.Client // requests through SOCKS5 proxy
	DirectClient *http.Client // requests without proxy
	Logger       *slog.Logger
	Catalog      *catalog.Service
	Upstream     *upstream.Executor
}

// NewHandler creates a proxy Handler.
func NewHandler(resolver *router.Resolver, proxyClient, directClient *http.Client, logger *slog.Logger, executors ...*upstream.Executor) *Handler {
	handler := &Handler{
		Resolver:     resolver,
		ProxyClient:  proxyClient,
		DirectClient: directClient,
		Logger:       logger,
	}
	if len(executors) > 0 {
		handler.Upstream = executors[0]
	}
	handler.Catalog = catalog.New(handler.clientFor)
	return handler
}

func (h *Handler) upstreamExecutor() *upstream.Executor {
	if h.Upstream == nil {
		policy := config.DefaultConfig().Retry
		policy.Enabled = false
		return upstream.NewExecutor(policy)
	}
	return h.Upstream
}

func (h *Handler) clientFor(p *router.Provider) *http.Client {
	if p.ShouldUseProxy() {
		return h.ProxyClient
	}
	return h.DirectClient
}

func (h *Handler) catalogService() *catalog.Service {
	if h.Catalog == nil {
		h.Catalog = catalog.New(h.clientFor)
	}
	return h.Catalog
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

	if fullModel == "" {
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

	h.externalHandler(w, r, &body, resolved)
}

// externalHandler proxies requests to external Anthropic-compatible APIs (direct passthrough).
func (h *Handler) externalHandler(w http.ResponseWriter, r *http.Request, body *translate.AnthropicRequest, resolved *router.ResolveResult) {
	body.Model = resolved.Model

	baseURL := strings.TrimRight(resolved.Provider.BaseURL, "/")
	url := baseURL + "/messages"
	apiKey := resolved.Provider.APIKey

	reqBody, _ := json.Marshal(body)
	timeout := externalRequestTimeout()
	mode := upstream.Buffered
	successBodyLimit := int64(1 << 20)
	if body.Stream {
		timeout = externalStreamTimeout()
		mode = upstream.Headers
		successBodyLimit = 0
	}
	sanitizer := upstream.NewSanitizer(apiKey)
	result, failure := h.upstreamExecutor().Do(
		r.Context(),
		h.clientFor(resolved.Provider),
		upstream.Metadata{
			RequestID: onellmLog.RequestIDFromContext(r.Context()),
			Provider:  resolved.Provider.Prefix,
			Model:     resolved.Model,
			Endpoint:  string(router.EndpointAnthropic),
		},
		upstream.Options{
			Mode:              mode,
			PerAttemptTimeout: timeout,
			SuccessBodyLimit:  successBodyLimit,
			Sanitizer:         sanitizer,
		},
		func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", apiKey)
			if body.Stream {
				req.Header.Set("Accept", "text/event-stream")
			}
			return req, nil
		},
	)
	if failure != nil {
		if failure.Kind == upstream.FailureClientCancel || failure.Kind == upstream.FailureServiceShutdown {
			return
		}
		if failure.Kind == upstream.FailureLocal {
			h.writeError(w, http.StatusInternalServerError, "create request: "+sanitizer.Sanitize([]byte(failure.Err.Error())))
			return
		}
		writeAnthropicUpstreamError(w, resolved.Provider.Prefix, failure)
		return
	}

	if !body.Stream {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(result.Body)
		return
	}

	// Streaming — direct SSE passthrough
	resp := result.Response
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	err := streamLines(resp.Body, streamFirstEventTimeout(), streamIdleTimeout(), func(line string) error {
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil && h.Logger != nil {
		h.Logger.Warn("external stream", "error", err)
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
	meta       *onellmLog.RequestMeta
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

type flushWriter struct {
	writer  io.Writer
	flusher http.Flusher
	meta    *onellmLog.RequestMeta
}

func (writer flushWriter) Write(data []byte) (int, error) {
	written, err := writer.writer.Write(data)
	if written > 0 {
		if writer.meta != nil {
			writer.meta.MarkStreamEvent(written)
		}
		if writer.flusher != nil {
			writer.flusher.Flush()
		}
	}
	return written, err
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

	if resolved.Provider.OpenAIBaseURL != "" {
		body.Model = resolved.Model
		h.openaiDirectHandler(w, r, &body, resolved)
	} else {
		h.openaiTranslateHandler(w, r, &body, resolved)
	}
}

func (h *Handler) openaiDirectHandler(w http.ResponseWriter, r *http.Request, body *translate.OpenAIRequest, resolved *router.ResolveResult) {
	body.Model = resolved.Model

	url := strings.TrimRight(resolved.Provider.OpenAIBaseURL, "/") + "/v1/chat/completions"
	client := h.clientFor(resolved.Provider)
	// OpenAI API doesn't support anthropic [1m] suffix — strip from model name
	body.Model = strings.TrimSuffix(body.Model, "[1m]")

	reqBody, _ := json.Marshal(body)
	sanitizer := upstream.NewSanitizer(resolved.Provider.APIKey)
	mode := upstream.Buffered
	if body.Stream {
		mode = upstream.Headers
	}
	result, failure := h.upstreamExecutor().Do(
		r.Context(),
		client,
		upstream.Metadata{
			RequestID: onellmLog.RequestIDFromContext(r.Context()),
			Provider:  resolved.Provider.Prefix,
			Model:     body.Model,
			Endpoint:  string(router.EndpointOpenAI),
		},
		upstream.Options{
			Mode:              mode,
			PerAttemptTimeout: openAIRequestTimeout(),
			SuccessBodyLimit:  0,
			Sanitizer:         sanitizer,
		},
		func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+resolved.Provider.APIKey)
			if body.Stream {
				req.Header.Set("Accept", "text/event-stream")
			}
			return req, nil
		},
	)
	if failure != nil {
		if failure.Kind == upstream.FailureClientCancel || failure.Kind == upstream.FailureServiceShutdown {
			return
		}
		if failure.Kind == upstream.FailureLocal {
			h.writeError(w, http.StatusInternalServerError, "create request: "+sanitizer.Sanitize([]byte(failure.Err.Error())))
			return
		}
		writeOpenAIUpstreamError(w, resolved.Provider.Prefix, failure)
		return
	}

	if !body.Stream {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Body)
		return
	}

	// Streaming: byte-for-byte SSE passthrough
	resp := result.Response
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	err := streamLines(resp.Body, streamFirstEventTimeout(), streamIdleTimeout(), func(line string) error {
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil && h.Logger != nil {
		h.Logger.Warn("openai direct stream", "error", err)
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
	client := h.clientFor(resolved.Provider)
	sanitizer := upstream.NewSanitizer(apiKey)
	mode := upstream.Buffered
	if body.Stream {
		mode = upstream.Headers
	}
	result, failure := h.upstreamExecutor().Do(
		r.Context(),
		client,
		upstream.Metadata{
			RequestID: onellmLog.RequestIDFromContext(r.Context()),
			Provider:  resolved.Provider.Prefix,
			Model:     body.Model,
			Endpoint:  string(router.EndpointAnthropic),
		},
		upstream.Options{
			Mode:              mode,
			PerAttemptTimeout: openAIRequestTimeout(),
			SuccessBodyLimit:  0,
			Sanitizer:         sanitizer,
		},
		func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", apiKey)
			if body.Stream {
				req.Header.Set("Accept", "text/event-stream")
			}
			return req, nil
		},
	)
	if failure != nil {
		if failure.Kind == upstream.FailureClientCancel || failure.Kind == upstream.FailureServiceShutdown {
			return
		}
		if failure.Kind == upstream.FailureLocal {
			h.writeError(w, http.StatusInternalServerError, "create request: "+sanitizer.Sanitize([]byte(failure.Err.Error())))
			return
		}
		writeOpenAIUpstreamError(w, resolved.Provider.Prefix, failure)
		return
	}

	if !body.Stream {
		var anthropicResp translate.AnthropicResponse
		if err := json.Unmarshal(result.Body, &anthropicResp); err != nil {
			h.writeError(w, http.StatusInternalServerError, "parse response: "+err.Error())
			return
		}
		openaiResp := translate.ReverseTranslateResponse(&anthropicResp, body.Model)
		h.writeJSON(w, http.StatusOK, openaiResp)
		return
	}

	// Streaming: Anthropic SSE passthrough
	resp := result.Response
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	err = streamLines(resp.Body, streamFirstEventTimeout(), streamIdleTimeout(), func(line string) error {
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil && h.Logger != nil {
		h.Logger.Warn("openai translate stream", "error", err, "request_id", onellmLog.RequestIDFromContext(r.Context()))
	}
}

// ServeResponses proxies to the Responses API (/v1/responses), used by Codex CLI.
func (h *Handler) ServeResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "read request body: "+err.Error())
		return
	}
	var reqMeta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(rawBody, &reqMeta); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	fullModel := reqMeta.Model
	if fullModel == "" {
		h.writeError(w, http.StatusBadRequest, "no model specified")
		return
	}
	resolved := h.Resolver.Resolve(fullModel)
	if resolved == nil {
		models := h.Resolver.AllModelIDs()
		h.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown model: %s. Available: %s", fullModel, strings.Join(models, ",")))
		return
	}
	if resolved.Provider.ResponsesBaseURL == "" {
		h.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("provider %q does not support the Responses API", resolved.Provider.Prefix))
		return
	}
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(rawBody, &bodyMap); err == nil {
		bodyMap["model"] = resolved.Model
		rawBody, _ = json.Marshal(bodyMap)
	}
	meta := onellmLog.RequestMetaFromContext(r.Context())
	meta.Model = fullModel
	meta.Provider = resolved.Provider.Prefix
	meta.Stream = reqMeta.Stream
	w = &ttfbWriter{ResponseWriter: w, meta: meta}
	h.responsesDirectHandler(w, r, rawBody, reqMeta.Stream, resolved)
}

func (h *Handler) responsesDirectHandler(w http.ResponseWriter, r *http.Request, rawBody []byte, stream bool, resolved *router.ResolveResult) {
	url := strings.TrimRight(resolved.Provider.ResponsesBaseURL, "/") + "/v1/responses"
	client := h.clientFor(resolved.Provider)
	sanitizer := upstream.NewSanitizer(resolved.Provider.APIKey)
	meta := onellmLog.RequestMetaFromContext(r.Context())
	mode := upstream.Buffered
	if stream {
		mode = upstream.Headers
	}
	meta.UpstreamStage = "headers"
	result, failure := h.upstreamExecutor().Do(
		r.Context(),
		client,
		upstream.Metadata{
			RequestID: onellmLog.RequestIDFromContext(r.Context()),
			Provider:  resolved.Provider.Prefix,
			Model:     resolved.Model,
			Endpoint:  string(router.EndpointResponses),
		},
		upstream.Options{
			Mode:              mode,
			PerAttemptTimeout: openAIRequestTimeout(),
			SuccessBodyLimit:  0,
			Sanitizer:         sanitizer,
		},
		func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawBody))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+resolved.Provider.APIKey)
			if stream {
				req.Header.Set("Accept", "text/event-stream")
			}
			return req, nil
		},
	)
	if failure != nil {
		meta.UpstreamStatus = failure.StatusCode
		if failure.Kind == upstream.FailureClientCancel {
			meta.EndReason = "client_cancel"
			return
		}
		if failure.Kind == upstream.FailureServiceShutdown {
			meta.EndReason = "service_shutdown"
			return
		}
		meta.EndReason = "upstream_error"
		if failure.Kind == upstream.FailureLocal {
			h.writeError(w, http.StatusInternalServerError, "create request: "+sanitizer.Sanitize([]byte(failure.Err.Error())))
			return
		}
		writeOpenAIUpstreamError(w, resolved.Provider.Prefix, failure)
		return
	}

	meta.UpstreamStatus = result.Response.StatusCode
	if !stream {
		meta.UpstreamStage = "body"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Body)
		meta.EndReason = "ok"
		return
	}

	resp := result.Response
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	meta.UpstreamStage = "stream"
	_, err := io.Copy(flushWriter{writer: w, flusher: flusher, meta: meta}, resp.Body)
	meta.MarkStreamFinish()
	if err != nil {
		meta.Error = err.Error()
		if errors.Is(context.Cause(r.Context()), upstream.ErrServiceShutdown) {
			meta.EndReason = "service_shutdown"
		} else if r.Context().Err() != nil {
			meta.EndReason = "client_cancel"
		} else {
			meta.EndReason = "upstream_error"
		}
		if h.Logger != nil {
			h.Logger.Warn("responses stream copy", "error", err, "request_id", onellmLog.RequestIDFromContext(r.Context()))
		}
		return
	}
	meta.EndReason = "ok"
}

func (h *Handler) ServeModelList(w http.ResponseWriter, r *http.Request, endpointType router.EndpointType) {
	providers := h.Resolver.Providers()
	endpoints := []router.EndpointType{endpointType}
	if endpointType == "" {
		endpoints = []router.EndpointType{router.EndpointAnthropic, router.EndpointOpenAI, router.EndpointResponses}
	}

	all := make([]router.ModelEntry, 0)
	responseModels := make([]catalog.Model, 0)
	modelIndex := make(map[string]int)
	sourceErrors := 0
	for _, endpoint := range endpoints {
		result := h.catalogService().List(r.Context(), providers, endpoint)
		sourceErrors += len(result.Errors)
		for _, sourceErr := range result.Errors {
			if h.Logger != nil {
				h.Logger.Warn("model catalog source", "provider", sourceErr.Provider, "endpoint", endpoint, "error", sourceErr.Err)
			}
		}
		for _, model := range result.Models {
			if endpoint == router.EndpointResponses {
				responseModels = append(responseModels, model)
			}
			if index, ok := modelIndex[model.ID]; ok {
				all[index].EndpointTypes = appendEndpointType(all[index].EndpointTypes, model.Endpoint)
				continue
			}
			modelIndex[model.ID] = len(all)
			all = append(all, router.ModelEntry{
				ID:            model.ID,
				Object:        "model",
				Created:       model.Created,
				OwnedBy:       model.OwnedBy,
				EndpointTypes: []router.EndpointType{model.Endpoint},
			})
		}
	}
	if len(all) == 0 && sourceErrors > 0 {
		h.writeError(w, http.StatusBadGateway, "model discovery failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if endpointType == router.EndpointResponses {
		data, err := catalog.MarshalCodex(responseModels)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "encode model catalog failed")
			return
		}
		_, _ = w.Write(data)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": all})
}

func appendEndpointType(endpointTypes []router.EndpointType, endpoint router.EndpointType) []router.EndpointType {
	for _, existing := range endpointTypes {
		if existing == endpoint {
			return endpointTypes
		}
	}
	return append(endpointTypes, endpoint)
}

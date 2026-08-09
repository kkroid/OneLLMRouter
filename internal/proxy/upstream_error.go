package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kkroid/onellm-router/internal/upstream"
)

func writeAnthropicUpstreamError(w http.ResponseWriter, provider string, failure *upstream.Failure) {
	payload := struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}{Type: "error"}
	payload.Error.Type = "api_error"
	payload.Error.Message = upstreamFailureMessage(provider, failure)
	writeUpstreamJSON(w, failureStatus(failure), payload)
}

func writeOpenAIUpstreamError(w http.ResponseWriter, provider string, failure *upstream.Failure) {
	payload := struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    string  `json:"code"`
		} `json:"error"`
	}{}
	payload.Error.Message = upstreamFailureMessage(provider, failure)
	payload.Error.Type = "upstream_error"
	payload.Error.Code = "upstream_retry_skipped"
	if failure.RetryEligible {
		payload.Error.Code = "upstream_retry_exhausted"
	}
	writeUpstreamJSON(w, failureStatus(failure), payload)
}

func writeUpstreamFailureResponse(w http.ResponseWriter, failure *upstream.Failure) bool {
	if failure == nil || failure.Kind != upstream.FailureHTTP || !failure.UpstreamResponseComplete {
		return false
	}
	copyUpstreamResponseHeaders(w.Header(), failure.UpstreamResponseHeader)
	status := failure.UpstreamResponseStatus
	if status == 0 {
		status = failure.StatusCode
	}
	w.WriteHeader(status)
	_, _ = w.Write(failure.UpstreamResponseBody)
	return true
}

func copyUpstreamResponseHeaders(destination, source http.Header) {
	connectionHeaders := make(map[string]struct{})
	for _, value := range source.Values("Connection") {
		for token := range strings.SplitSeq(value, ",") {
			connectionHeaders[http.CanonicalHeaderKey(strings.TrimSpace(token))] = struct{}{}
		}
	}
	for key, values := range source {
		canonicalKey := http.CanonicalHeaderKey(key)
		if isHopByHopHeader(canonicalKey) || canonicalKey == "Content-Length" {
			continue
		}
		if _, skip := connectionHeaders[canonicalKey]; skip {
			continue
		}
		for _, value := range values {
			destination.Add(canonicalKey, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch key {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func upstreamFailureMessage(provider string, failure *upstream.Failure) string {
	lastError := ""
	if failure.StatusCode != 0 && failure.Kind == upstream.FailureHTTP {
		lastError = fmt.Sprintf("HTTP %d %s", failure.StatusCode, http.StatusText(failure.StatusCode))
	} else if failure.Err != nil {
		lastError = failure.Err.Error()
	} else {
		lastError = string(failure.Kind)
	}
	var message strings.Builder
	fmt.Fprintln(&message, "OneLLMRouter upstream request failed.")
	fmt.Fprintf(&message, "Provider: %s\n", provider)
	fmt.Fprintf(&message, "Attempts: %d\n", failure.Attempts)
	fmt.Fprintf(&message, "Elapsed: %s\n", failure.Elapsed)
	fmt.Fprintf(&message, "Last error: %s", lastError)
	if failure.Summary != "" && failure.Summary != lastError {
		fmt.Fprintf(&message, "\nUpstream response: %s", failure.Summary)
	}
	return message.String()
}

func failureStatus(failure *upstream.Failure) int {
	if failure.StatusCode != 0 {
		if failure.StatusCode < http.StatusOK ||
			failure.StatusCode == http.StatusNoContent ||
			failure.StatusCode == http.StatusNotModified {
			return http.StatusBadGateway
		}
		return failure.StatusCode
	}
	if failure.Kind == upstream.FailureTimeout {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func writeUpstreamJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

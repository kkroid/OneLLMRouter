package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kkroid/onellm-router/internal/upstream"
)

func TestWriteAnthropicUpstreamError(t *testing.T) {
	recorder := httptest.NewRecorder()
	failure := &upstream.Failure{
		StatusCode: http.StatusTooManyRequests,
		Kind:       upstream.FailureHTTP,
		Summary:    "capacity temporarily exhausted",
		Err:        errors.New("upstream returned HTTP 429"),
		Attempts:   15,
		Elapsed:    4*time.Minute + 58*time.Second,
	}

	writeAnthropicUpstreamError(recorder, "c78", failure)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var payload struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "error" || payload.Error.Type != "api_error" {
		t.Fatalf("payload = %+v", payload)
	}
	assertUpstreamFailureMessage(t, payload.Error.Message, "c78", "15", "4m58s", "HTTP 429", failure.Summary)
}

func TestWriteOpenAIUpstreamError(t *testing.T) {
	recorder := httptest.NewRecorder()
	failure := &upstream.Failure{
		Kind:     upstream.FailureTransport,
		Summary:  "connection reset by peer",
		Err:      errors.New("connection reset by peer"),
		Attempts: 9,
		Elapsed:  5 * time.Minute,
	}

	writeOpenAIUpstreamError(recorder, "ds", failure)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
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
	if payload.Error.Type != "upstream_error" || payload.Error.Param != nil || payload.Error.Code != "upstream_retry_exhausted" {
		t.Fatalf("payload = %+v", payload)
	}
	assertUpstreamFailureMessage(t, payload.Error.Message, "ds", "9", "5m0s", "connection reset by peer")
}

func TestUpstreamErrorWriterUsesTimeoutStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	failure := &upstream.Failure{
		StatusCode: http.StatusGatewayTimeout,
		Kind:       upstream.FailureTimeout,
		Err:        errors.New("upstream attempt timeout"),
		Attempts:   3,
		Elapsed:    10 * time.Second,
	}

	writeOpenAIUpstreamError(recorder, "mars", failure)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", recorder.Code)
	}
}

func TestUpstreamErrorWriterMapsBodylessStatusToBadGateway(t *testing.T) {
	for _, status := range []int{http.StatusSwitchingProtocols, http.StatusNotModified} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			failure := &upstream.Failure{StatusCode: status, Kind: upstream.FailureHTTP}
			if got := failureStatus(failure); got != http.StatusBadGateway {
				t.Fatalf("failureStatus(%d) = %d, want 502", status, got)
			}
		})
	}
}

func assertUpstreamFailureMessage(t *testing.T, message string, parts ...string) {
	t.Helper()
	if !strings.Contains(message, "OneLLMRouter upstream request failed") {
		t.Fatalf("message lacks router identity: %q", message)
	}
	for _, part := range parts {
		if !strings.Contains(message, part) {
			t.Errorf("message %q lacks %q", message, part)
		}
	}
}

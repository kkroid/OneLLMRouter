package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	onellmLog "github.com/kkroid/onellm-router/internal/log"
)

func TestRequestLogIncludesRetrySummary(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := onellmLog.RequestMetaFromContext(r.Context())
		meta.Model = "c78/gpt-5.6-sol"
		meta.Provider = "c78"
		meta.UpstreamAttempts = 3
		meta.RetryElapsedMs = 0
		meta.LastUpstreamStatus = http.StatusBadGateway
		meta.LastFailureKind = "http"
		w.WriteHeader(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)

	withRequestID(handler, logger).ServeHTTP(recorder, request)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode request log: %v\n%s", err, output.String())
	}
	if record["request_id"] == "" || record["upstream_attempts"] != float64(3) || record["retry_elapsed_ms"] != float64(0) {
		t.Fatalf("request retry summary = %#v", record)
	}
	if record["last_upstream_status"] != float64(http.StatusBadGateway) || record["last_failure_kind"] != "http" {
		t.Fatalf("request last failure = %#v", record)
	}
}

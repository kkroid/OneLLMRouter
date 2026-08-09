package usage

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreAppendsAndRollsOverByUTCDay(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.August, 9, 23, 59, 0, 0, time.FixedZone("west", -7*60*60))
	store := NewStore(dir, nil, func() time.Time { return now })

	first := testRecord("request-1", 1)
	if err := store.Write(first); err != nil {
		t.Fatal(err)
	}
	second := testRecord("request-2", 1)
	if err := store.Write(second); err != nil {
		t.Fatal(err)
	}

	firstPath := filepath.Join(dir, "2026-08-10.jsonl")
	data, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Fatalf("line count = %d, want 2", lines)
	}

	now = now.Add(24 * time.Hour)
	if err := store.Write(testRecord("request-3", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-08-11.jsonl")); err != nil {
		t.Fatal(err)
	}

	result, err := ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || !result.Records[0].Time.Equal(time.Date(2026, time.August, 10, 6, 59, 0, 0, time.UTC)) {
		t.Fatalf("records = %+v", result.Records)
	}
}

func TestStoreLogsWriteFailureWithoutRecordContents(t *testing.T) {
	root := t.TempDir()
	blockedPath := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedPath, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	store := NewStore(blockedPath, logger, func() time.Time {
		return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	})

	err := store.Write(testRecord("request-safe", 1))
	if err == nil {
		t.Fatal("Write() error = nil")
	}
	logLine := output.String()
	if !strings.Contains(logLine, `"msg":"write usage record"`) || !strings.Contains(logLine, `"request_id":"request-safe"`) {
		t.Fatalf("log = %s", logLine)
	}
	for _, secret := range []string{"api-key-secret", "Authorization", "request body", "response body"} {
		if strings.Contains(logLine, secret) {
			t.Fatalf("log contains %q: %s", secret, logLine)
		}
	}
}

func TestStoredRecordContainsOnlyUsageContractFields(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, nil, func() time.Time {
		return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	})
	if err := store.Write(testRecord("request-1", 1)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "2026-08-09.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 14 {
		t.Fatalf("field count = %d, want 14: %v", len(fields), fields)
	}
}

func testRecord(requestID string, attempt int) Record {
	return Record{
		RequestID:       requestID,
		Provider:        "openai",
		RequestedModel:  "openai/gpt-5",
		UpstreamModel:   "gpt-5",
		Protocol:        ProtocolOpenAIResponses,
		UpstreamAttempt: attempt,
		Status:          StatusSuccess,
	}
}

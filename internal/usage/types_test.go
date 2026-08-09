package usage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRecordJSONDistinguishesAbsentAndZeroTokens(t *testing.T) {
	zero := 0
	record := Record{
		InputTokens: &zero,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["input_tokens"] != float64(0) {
		t.Fatalf("input_tokens = %#v, want 0", fields["input_tokens"])
	}
	if fields["output_tokens"] != nil {
		t.Fatalf("output_tokens = %#v, want null", fields["output_tokens"])
	}
}

func TestRecordJSONRoundTripsAllTokenCategories(t *testing.T) {
	input := 1
	output := 2
	cacheRead := 3
	cacheWrite := 4
	reasoning := 5
	source := SourceTranslatedResponse
	want := Record{
		Time:             time.Date(2026, time.August, 9, 12, 30, 0, 0, time.UTC),
		RequestID:        "request-1",
		Provider:         "openai",
		RequestedModel:   "openai/gpt-5",
		UpstreamModel:    "gpt-5",
		Protocol:         ProtocolOpenAIResponses,
		InputTokens:      &input,
		OutputTokens:     &output,
		CacheReadTokens:  &cacheRead,
		CacheWriteTokens: &cacheWrite,
		ReasoningTokens:  &reasoning,
		UsageSource:      &source,
		UpstreamAttempt:  2,
		Status:           StatusSuccess,
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.Time != want.Time || got.RequestID != want.RequestID || got.Provider != want.Provider ||
		got.RequestedModel != want.RequestedModel || got.UpstreamModel != want.UpstreamModel ||
		got.Protocol != want.Protocol || got.UsageSource == nil || *got.UsageSource != source ||
		got.UpstreamAttempt != want.UpstreamAttempt || got.Status != want.Status {
		t.Fatalf("record metadata did not round-trip: got %+v, want %+v", got, want)
	}
	assertTokenValue(t, "input", got.InputTokens, input)
	assertTokenValue(t, "output", got.OutputTokens, output)
	assertTokenValue(t, "cache read", got.CacheReadTokens, cacheRead)
	assertTokenValue(t, "cache write", got.CacheWriteTokens, cacheWrite)
	assertTokenValue(t, "reasoning", got.ReasoningTokens, reasoning)
}

func assertTokenValue(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s tokens = %v, want %d", name, got, want)
	}
}

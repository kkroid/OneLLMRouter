package usage

import (
	"testing"
)

type recordWriter struct {
	records []Record
}

func (w *recordWriter) Write(record Record) error {
	w.records = append(w.records, record)
	return nil
}

func TestCollectorExtractsProtocolUsageAndPreservesPresence(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		body     string
		check    func(*testing.T, Record)
	}{
		{
			name:     "anthropic",
			protocol: ProtocolAnthropicMessages,
			body:     `{"usage":{"input_tokens":0,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}`,
			check: func(t *testing.T, record Record) {
				assertToken(t, "input", record.InputTokens, 0)
				assertToken(t, "output", record.OutputTokens, 2)
				assertToken(t, "cache read", record.CacheReadTokens, 3)
				assertToken(t, "cache write", record.CacheWriteTokens, 4)
			},
		},
		{
			name:     "openai chat",
			protocol: ProtocolOpenAIChat,
			body:     `{"usage":{"prompt_tokens":5,"completion_tokens":6,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`,
			check: func(t *testing.T, record Record) {
				assertToken(t, "input", record.InputTokens, 5)
				assertToken(t, "output", record.OutputTokens, 6)
				assertToken(t, "cache read", record.CacheReadTokens, 2)
				assertToken(t, "reasoning", record.ReasoningTokens, 1)
			},
		},
		{
			name:     "responses",
			protocol: ProtocolOpenAIResponses,
			body:     `{"usage":{"input_tokens":7,"output_tokens":8,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}`,
			check: func(t *testing.T, record Record) {
				assertToken(t, "input", record.InputTokens, 7)
				assertToken(t, "output", record.OutputTokens, 8)
				assertToken(t, "cache read", record.CacheReadTokens, 3)
				assertToken(t, "reasoning", record.ReasoningTokens, 2)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer := &recordWriter{}
			collector := NewCollector(writer)
			collector.CollectResponse(Attempt{RequestID: "r", Protocol: test.protocol, UpstreamAttempt: 1}, []byte(test.body), SourceResponse, StatusSuccess)
			if len(writer.records) != 1 {
				t.Fatalf("records = %d, want 1", len(writer.records))
			}
			test.check(t, writer.records[0])
		})
	}
}

func TestCollectorLeavesMissingUsageUnknown(t *testing.T) {
	writer := &recordWriter{}
	NewCollector(writer).CollectResponse(Attempt{RequestID: "r", Protocol: ProtocolOpenAIChat, UpstreamAttempt: 1}, []byte(`{"usage":{}}`), SourceResponse, StatusSuccess)
	record := writer.records[0]
	if record.InputTokens != nil || record.OutputTokens != nil || record.UsageSource != nil {
		t.Fatalf("record = %+v", record)
	}
}

func TestStreamCollectorRequiresTerminalUsageAndHandlesSplitWrites(t *testing.T) {
	attempt := Attempt{RequestID: "r", Protocol: ProtocolAnthropicMessages, UpstreamAttempt: 1}

	t.Run("terminal usage", func(t *testing.T) {
		writer := &recordWriter{}
		stream := NewCollector(writer).NewStream(attempt)
		_, _ = stream.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0}}}\n"))
		_, _ = stream.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_"))
		_, _ = stream.Write([]byte("tokens\":4}}\n\n"))
		stream.Finish(StatusSuccess)
		record := writer.records[0]
		assertToken(t, "input", record.InputTokens, 0)
		assertToken(t, "output", record.OutputTokens, 4)
		if record.UsageSource == nil || *record.UsageSource != SourceStream {
			t.Fatalf("usage source = %v", record.UsageSource)
		}
	})

	t.Run("missing terminal usage", func(t *testing.T) {
		writer := &recordWriter{}
		stream := NewCollector(writer).NewStream(attempt)
		_, _ = stream.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":9}}}\n\n"))
		stream.Finish(StatusError)
		record := writer.records[0]
		if record.InputTokens != nil || record.OutputTokens != nil || record.UsageSource != nil {
			t.Fatalf("record = %+v", record)
		}
	})
}

func assertToken(t *testing.T, name string, value *int, want int) {
	t.Helper()
	if value == nil || *value != want {
		t.Fatalf("%s tokens = %v, want %d", name, value, want)
	}
}

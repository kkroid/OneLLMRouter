package translate

import (
	"testing"
)

func TestStreamChunk_RoleOnlyDelta(t *testing.T) {
	// Claude Code: first chunk may have delta with only role, no content
	ctx := &StreamContext{
		MessageID: "msg_test", Model: "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}
	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{Role: "assistant"}},
		},
	}
	events, err := TranslateStreamChunk(chunk, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Only message_start should be emitted, no content blocks
	for _, ev := range events {
		if ev.Type != "message_start" {
			t.Errorf("unexpected event type %s on role-only delta", ev.Type)
		}
	}
}

func TestStreamChunk_TextThenToolSwitch(t *testing.T) {
	ctx := &StreamContext{MessageID: "msg", Model: "m",
		ToolCalls: make(map[int]*ToolCallState)}

	// Text first
	chunk1 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{Content: "Let me check that"}},
		},
	}
	events1, _ := TranslateStreamChunk(chunk1, ctx)
	// Should have message_start + content_block_start(text) + delta
	hasTextStart := false
	for _, ev := range events1 {
		if ev.Type == "content_block_start" {
			if cb, ok := ev.ContentBlock.(map[string]interface{}); ok && cb["type"] == "text" {
				hasTextStart = true
			}
		}
	}
	if !hasTextStart {
		t.Error("expected text content_block_start")
	}

	// Then tool call: Copilot closes text block implicitly by sending tool_calls
	chunk2 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{
				ToolCalls: []OpenAIToolCallDelta{
					{Index: 0, ID: "call_weather", Function: &OpenAIToolFunctionDelta{Name: "get_weather", Arguments: `{}`}},
				},
			}},
		},
	}
	events2, _ := TranslateStreamChunk(chunk2, ctx)
	for _, ev := range events2 {
		if ev.Type == "content_block_stop" || ev.Type == "content_block_start" {
			t.Fatalf("tool blocks must wait for finish: %+v", events2)
		}
	}
	finishReason := "tool_calls"
	events2, _ = TranslateStreamChunk(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)
	hasTextStop := false
	hasToolStart := false
	for _, ev := range events2 {
		if ev.Type == "content_block_stop" {
			hasTextStop = true
		}
		if ev.Type == "content_block_start" {
			if cb, ok := ev.ContentBlock.(map[string]interface{}); ok && cb["type"] == "tool_use" {
				hasToolStart = true
			}
		}
	}
	if !hasTextStop {
		t.Error("expected text content_block_stop before tool use")
	}
	if !hasToolStart {
		t.Error("expected tool_use content_block_start after text")
	}
}

func TestStreamChunk_MultipleTools(t *testing.T) {
	ctx := &StreamContext{MessageID: "msg", Model: "m",
		ToolCalls: make(map[int]*ToolCallState)}

	// Copilot sends two tool calls interleaved in one chunk
	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{
				ToolCalls: []OpenAIToolCallDelta{
					{Index: 0, ID: "call_weather", Function: &OpenAIToolFunctionDelta{Name: "get_weather", Arguments: `{"city":"bj"}`}},
					{Index: 1, ID: "call_time", Function: &OpenAIToolFunctionDelta{Name: "get_time", Arguments: `{"tz":"utc"}`}},
				},
			}},
		},
	}
	events, _ := TranslateStreamChunk(chunk, ctx)
	for _, ev := range events {
		if ev.Type == "content_block_start" {
			t.Fatalf("tool blocks must wait for finish: %+v", events)
		}
	}
	finishReason := "tool_calls"
	events, _ = TranslateStreamChunk(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)

	startCount := 0
	for _, ev := range events {
		if ev.Type == "content_block_start" {
			startCount++
		}
	}
	if startCount < 2 {
		t.Errorf("expected 2 content_block_start events, got %d", startCount)
	}
	// Both tool states should be tracked
	if len(ctx.ToolCalls) != 2 {
		t.Errorf("expected 2 tool calls tracked, got %d", len(ctx.ToolCalls))
	}
}

func TestStreamChunk_FinishReasonEmptyString(t *testing.T) {
	// Claude Code: finish_reason may be "" not nil
	ctx := &StreamContext{MessageID: "msg", Model: "m",
		ToolCalls: make(map[int]*ToolCallState)}
	ctx.MessageStartSent = true
	ctx.ContentBlockOpen = true

	emptyFinish := ""
	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{Content: "text"}, FinishReason: &emptyFinish},
		},
	}
	events, _ := TranslateStreamChunk(chunk, ctx)
	// Empty string finish_reason should NOT trigger stop events
	for _, ev := range events {
		if ev.Type == "message_stop" {
			t.Error("empty finish_reason should not trigger message_stop")
		}
	}
}

func TestStreamChunk_FinishReasonContentFilter(t *testing.T) {
	ctx := &StreamContext{MessageID: "msg", Model: "m",
		ToolCalls: make(map[int]*ToolCallState)}
	ctx.MessageStartSent = true

	cf := "content_filter"
	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, FinishReason: &cf},
		},
	}
	events, _ := TranslateStreamChunk(chunk, ctx)
	// Unknown finish reasons default to "end_turn", should not crash
	foundStop := false
	for _, ev := range events {
		if ev.Type == "message_stop" {
			foundStop = true
		}
	}
	if !foundStop {
		t.Error("expected message_stop even for unknown finish_reason")
	}
}

func TestStreamChunk_MultipleDataLines(t *testing.T) {
	// Some SSE implementations send multiple data: lines per event
	// We test that our split-then-parse approach handles single lines correctly
	ctx := &StreamContext{MessageID: "msg", Model: "m",
		ToolCalls: make(map[int]*ToolCallState)}
	ctx.MessageStartSent = true

	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{Content: "hello"}},
		},
	}
	events, _ := TranslateStreamChunk(chunk, ctx)
	if len(events) == 0 {
		t.Error("expected at least 1 event")
	}
}

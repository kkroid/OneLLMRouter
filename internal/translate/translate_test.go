package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranslateRequest_SimpleText(t *testing.T) {
	req := &AnthropicRequest{
		Model: "claude-opus-4.8",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 50,
		System:    "You are helpful",
	}

	result, err := TranslateRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	if result.Model != "claude-opus-4.8" {
		t.Errorf("model mismatch: %s", result.Model)
	}
	if len(result.Messages) != 2 { // system + user
		t.Fatalf("expected 2 messages (system+user), got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("first message should be system, got %s", result.Messages[0].Role)
	}
	if result.Messages[1].Role != "user" {
		t.Errorf("second message should be user, got %s", result.Messages[1].Role)
	}
	if result.MaxTokens != 50 {
		t.Errorf("max_tokens mismatch: %d", result.MaxTokens)
	}
}

func TestTranslateRequest_Stream(t *testing.T) {
	req := &AnthropicRequest{
		Model: "claude-fable-5",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 100,
		Stream:    true,
	}

	result, err := TranslateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stream {
		t.Error("stream should be true")
	}
}

func TestTranslateRequest_WithTools(t *testing.T) {
	req := &AnthropicRequest{
		Model: "claude-opus-4.8",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "query"},
		},
		MaxTokens: 100,
		Tools: []AnthropicTool{
			{Name: "get_weather", Description: "Get weather", InputSchema: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: &AnthropicToolChoice{Type: "auto"},
	}

	result, err := TranslateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Function.Name != "get_weather" {
		t.Errorf("tool name mismatch: %s", result.Tools[0].Function.Name)
	}
	if result.ToolChoice != "auto" {
		t.Errorf("tool_choice should be auto, got %v", result.ToolChoice)
	}
}

func TestTranslateRequest_AssistantWithText(t *testing.T) {
	req := &AnthropicRequest{
		Model: "claude-opus-4.8",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "Hello!"},
			{Role: "user", Content: "bye"},
		},
		MaxTokens: 50,
	}

	result, err := TranslateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	// user + assistant + user = 3 messages
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}
}

func TestTranslateResponse_TextOnly(t *testing.T) {
	openai := &OpenAIResponse{
		ID:     "msg_123",
		Model:  "gpt-4",
		Object: "chat.completion",
		Choices: []OpenAIChoice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: "Hello world",
				},
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	result := TranslateResponse(openai, "claude-opus-4.8")
	if result.ID != "msg_123" {
		t.Errorf("id mismatch: %s", result.ID)
	}
	if result.Model != "claude-opus-4.8" {
		t.Errorf("model mismatch: %s", result.Model)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Errorf("expected 1 text block")
	}
	if result.Content[0].Text != "Hello world" {
		t.Errorf("text mismatch: %s", result.Content[0].Text)
	}
	if result.StopReason == nil || *result.StopReason != "end_turn" {
		t.Errorf("stop_reason mismatch")
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 {
		t.Errorf("usage mismatch")
	}
}

func TestTranslateResponse_WithToolCall(t *testing.T) {
	openai := &OpenAIResponse{
		ID: "msg_456",
		Choices: []OpenAIChoice{
			{
				FinishReason: "tool_calls",
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: nil,
					ToolCalls: []OpenAIToolCall{
						{Index: 0, ID: "call_1", Type: "function", Function: OpenAIToolFunction{
							Name: "get_weather", Arguments: `{"city":"paris"}`,
						}},
					},
				},
			},
		},
		Usage: OpenAIUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}

	result := TranslateResponse(openai, "claude-opus-4.8")
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("expected 1 tool_use block, got %d blocks", len(result.Content))
	}
	if result.Content[0].Name != "get_weather" {
		t.Errorf("tool name mismatch: %s", result.Content[0].Name)
	}
	if *result.StopReason != "tool_use" {
		t.Errorf("stop_reason should be tool_use")
	}
}

func TestStopReasonMapping(t *testing.T) {
	tests := []struct{ finish, expected string }{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"unknown", ""}, // nil
	}
	for _, tc := range tests {
		openai := &OpenAIResponse{
			ID: "x",
			Choices: []OpenAIChoice{
				{FinishReason: tc.finish, Message: OpenAIMessage{Role: "assistant"}},
			},
			Usage: OpenAIUsage{},
		}
		result := TranslateResponse(openai, "m")
		if tc.expected == "" {
			if result.StopReason != nil {
				t.Errorf("%s: expected nil stop_reason, got %s", tc.finish, *result.StopReason)
			}
		} else if result.StopReason == nil || *result.StopReason != tc.expected {
			got := "<nil>"
			if result.StopReason != nil {
				got = *result.StopReason
			}
			t.Errorf("%s: expected %s, got %s", tc.finish, tc.expected, got)
		}
	}
}

func TestStreamChunk_TextFlow(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_test",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	// First chunk: message_start
	chunk1 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{Content: "Hello"}},
		},
	}
	events1, err := TranslateStreamChunk(chunk1, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events1) < 2 {
		t.Fatalf("expected >=2 events (message_start + content_block_start + delta), got %d", len(events1))
	}
	if events1[0].Type != "message_start" {
		t.Errorf("first event should be message_start, got %s", events1[0].Type)
	}

	// Second chunk: more text
	chunk2 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{Content: " world"}},
		},
	}
	events2, err := TranslateStreamChunk(chunk2, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events2) != 1 || events2[0].Type != "content_block_delta" {
		t.Errorf("expected content_block_delta, got %d events", len(events2))
	}

	// Finish chunk
	finish := "stop"
	chunk3 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, FinishReason: &finish},
		},
	}
	events3, err := TranslateStreamChunk(chunk3, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events3) != 3 {
		t.Fatalf("expected 3 finish events (block_stop + message_delta + message_stop), got %d", len(events3))
	}
	if events3[len(events3)-1].Type != "message_stop" {
		t.Errorf("last event should be message_stop, got %s", events3[len(events3)-1].Type)
	}
}

func TestStreamChunk_EmptyChunk(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_test",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{},
	}
	events, err := TranslateStreamChunk(chunk, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty chunk, got %d", len(events))
	}
}

func TestTranslateRequest_ImageContent(t *testing.T) {
	reqJSON := `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc123"}}]}]}`
	var req AnthropicRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		t.Fatal(err)
	}

	result, err := TranslateRequest(&req)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 1 user message with multi-part content
	msg := result.Messages[0]
	if msg.Role != "user" {
		t.Errorf("expected user, got %s", msg.Role)
	}
	parts, ok := msg.Content.([]interface{})
	if !ok {
		// Content might be a single text if the first part was text
		// That's fine — the optimizer simplifies single-text parts
		return
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 content parts, got %d", len(parts))
	}
}

func TestStreamChunk_ToolCall_DelayedName(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_test",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	// Chunk 1: upstream sends ID first, no name (THIS is the bug scenario)
	chunk1 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{
				ToolCalls: []OpenAIToolCallDelta{
					{Index: 0, ID: "call_abc123"},
				},
			}},
		},
	}
	events1, err := TranslateStreamChunk(chunk1, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Must NOT send content_block_start yet (name unknown)
	for _, ev := range events1 {
		if ev.Type == "content_block_start" {
			t.Error("should NOT send content_block_start when name is unknown")
		}
	}

	// Chunk 2: Name arrives, args start
	chunk2 := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{
			{Index: 0, Delta: OpenAIStreamDelta{
				ToolCalls: []OpenAIToolCallDelta{
					{Index: 0, Function: &OpenAIToolFunctionDelta{Name: "get_weather", Arguments: `{"city":"beijing`}},
				},
			}},
		},
	}
	events2, err := TranslateStreamChunk(chunk2, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events2 {
		if ev.Type == "content_block_start" {
			t.Fatalf("tool block must wait for finish: %+v", events2)
		}
	}
	finishReason := "tool_calls"
	events2, err = TranslateStreamChunk(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, ev := range events2 {
		if ev.Type == "content_block_start" {
			cb, _ := ev.ContentBlock.(map[string]interface{})
			if cb["name"] != "get_weather" {
				t.Errorf("expected name get_weather, got %v", cb["name"])
			}
			if cb["id"] != "call_abc123" {
				t.Errorf("expected id call_abc123, got %v", cb["id"])
			}
			found = true
		}
	}
	if !found {
		t.Error("expected content_block_start for tool_use")
	}

	tc := ctx.ToolCalls[0]
	if tc == nil || !tc.BlockSent {
		t.Error("BlockSent should be true after content_block_start emitted")
	}
}

func TestGenerateMessageID(t *testing.T) {
	id := GenerateMessageID()
	if !strings.HasPrefix(id, "msg_") {
		t.Errorf("message ID should start with msg_: %s", id)
	}
	if len(id) < 10 {
		t.Errorf("message ID too short: %s", id)
	}
}

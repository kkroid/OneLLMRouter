package translate

import "testing"

func TestCoreTypes_CanRepresentCurrentRequestSurface(t *testing.T) {
	temp := 0.2
	core := &CoreRequest{
		Model:       "m",
		MaxTokens:   100,
		Stream:      true,
		Temperature: &temp,
		Messages: []CoreMessage{{
			Role: "user",
			Content: []CoreContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "image", Image: &CoreImage{MediaType: "image/png", Data: "abc"}},
			},
		}},
		Tools: []CoreTool{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: map[string]interface{}{"type": "object"},
		}},
		ToolChoice: &CoreToolChoice{Type: "auto"},
	}
	if core.Model != "m" || len(core.Messages) != 1 || len(core.Tools) != 1 {
		t.Fatalf("core request not populated: %+v", core)
	}
}

func TestAnthropicRequestToCore_WithSystemImageAndTools(t *testing.T) {
	req := &AnthropicRequest{
		Model:     "claude",
		System:    "You are helpful",
		MaxTokens: 100,
		Stream:    true,
		Messages: []AnthropicMessage{{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "what is this?"},
				map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": "image/png",
						"data":       "abc123",
					},
				},
			},
		}},
		Tools: []AnthropicTool{{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: map[string]interface{}{"type": "object"},
		}},
		ToolChoice: &AnthropicToolChoice{Type: "tool", Name: "get_weather"},
	}

	core, err := AnthropicRequestToCore(req)
	if err != nil {
		t.Fatal(err)
	}
	if core.Model != "claude" || !core.Stream || core.MaxTokens != 100 {
		t.Fatalf("unexpected core scalar fields: %+v", core)
	}
	if len(core.System) != 1 || core.System[0].Text != "You are helpful" {
		t.Fatalf("system not converted: %+v", core.System)
	}
	if len(core.Messages) != 1 || len(core.Messages[0].Content) != 2 {
		t.Fatalf("message content not converted: %+v", core.Messages)
	}
	if core.Messages[0].Content[1].Image == nil || core.Messages[0].Content[1].Image.MediaType != "image/png" {
		t.Fatalf("image not converted: %+v", core.Messages[0].Content[1])
	}
	if len(core.Tools) != 1 || core.Tools[0].Name != "get_weather" {
		t.Fatalf("tools not converted: %+v", core.Tools)
	}
	if core.ToolChoice == nil || core.ToolChoice.Type != "tool" || core.ToolChoice.Name != "get_weather" {
		t.Fatalf("tool choice not converted: %+v", core.ToolChoice)
	}
}

func TestCoreToOpenAIRequest_ToolResultAndToolChoice(t *testing.T) {
	core := &CoreRequest{
		Model:     "m",
		MaxTokens: 50,
		Messages: []CoreMessage{{
			Role: "user",
			Content: []CoreContentBlock{
				{Type: "tool_result", ToolUseID: "call_1", ToolResultContent: "ok"},
				{Type: "text", Text: "continue"},
			},
		}},
		Tools: []CoreTool{{Name: "get_weather", InputSchema: map[string]interface{}{"type": "object"}}},
		ToolChoice: &CoreToolChoice{
			Type: "any",
		},
	}

	openai, err := CoreToOpenAIRequest(core)
	if err != nil {
		t.Fatal(err)
	}
	if len(openai.Messages) != 2 {
		t.Fatalf("expected tool message plus user message, got %+v", openai.Messages)
	}
	if openai.Messages[0].Role != "tool" || openai.Messages[0].ToolCallID != "call_1" {
		t.Fatalf("tool result not converted first: %+v", openai.Messages[0])
	}
	if openai.ToolChoice != "required" {
		t.Fatalf("tool choice mismatch: %v", openai.ToolChoice)
	}
}

func TestOpenAIResponseToCore_WithToolCall(t *testing.T) {
	openai := &OpenAIResponse{
		ID: "chatcmpl_1",
		Choices: []OpenAIChoice{{
			FinishReason: "tool_calls",
			Message: OpenAIMessage{
				Role:    "assistant",
				Content: "checking",
				ToolCalls: []OpenAIToolCall{{
					ID: "call_1",
					Function: OpenAIToolFunction{
						Name:      "get_weather",
						Arguments: `{"city":"paris"}`,
					},
				}},
			},
		}},
		Usage: OpenAIUsage{PromptTokens: 3, CompletionTokens: 4},
	}

	core := OpenAIResponseToCore(openai)
	if core.ID != "chatcmpl_1" || core.StopReason != "tool_use" {
		t.Fatalf("unexpected core response: %+v", core)
	}
	if len(core.Content) != 2 {
		t.Fatalf("expected text and tool_use, got %+v", core.Content)
	}
	if core.Content[1].ToolName != "get_weather" || core.Content[1].ToolInput["city"] != "paris" {
		t.Fatalf("tool call not converted: %+v", core.Content[1])
	}
	if core.Usage.InputTokens != 3 || core.Usage.OutputTokens != 4 {
		t.Fatalf("usage mismatch: %+v", core.Usage)
	}
}

func TestCoreToAnthropicResponse_TextAndUsage(t *testing.T) {
	core := &CoreResponse{
		ID:         "msg_1",
		Model:      "upstream",
		Role:       "assistant",
		StopReason: "end_turn",
		Content: []CoreContentBlock{{
			Type: "text",
			Text: "hello",
		}},
		Usage: CoreUsage{InputTokens: 1, OutputTokens: 2},
	}

	resp := CoreToAnthropicResponse(core, "claude")
	if resp.Model != "claude" || len(resp.Content) != 1 || resp.Content[0].Text != "hello" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.StopReason == nil || *resp.StopReason != "end_turn" {
		t.Fatalf("stop reason mismatch: %+v", resp.StopReason)
	}
	if resp.Usage.InputTokens != 1 || resp.Usage.OutputTokens != 2 {
		t.Fatalf("usage mismatch: %+v", resp.Usage)
	}
}

func TestAnthropicResponseToCore_ThinkingAndToolUse(t *testing.T) {
	stop := "tool_use"
	resp := &AnthropicResponse{
		ID:         "msg_1",
		Role:       "assistant",
		StopReason: &stop,
		Content: []AnthropicContentBlock{
			{Type: "thinking", Thinking: "think"},
			{Type: "tool_use", ID: "call_1", Name: "read", Input: map[string]interface{}{"path": "a.go"}},
		},
		Usage: AnthropicUsage{InputTokens: 1, OutputTokens: 2},
	}

	core := AnthropicResponseToCore(resp)
	if core.StopReason != "tool_use" || len(core.Content) != 2 {
		t.Fatalf("unexpected core response: %+v", core)
	}
	if core.Content[0].Thinking != "think" || core.Content[1].ToolName != "read" {
		t.Fatalf("content mismatch: %+v", core.Content)
	}
}

func TestCoreToOpenAIResponse_ToolUse(t *testing.T) {
	core := &CoreResponse{
		ID:         "msg_1",
		StopReason: "tool_use",
		Content: []CoreContentBlock{{
			Type:      "tool_use",
			ToolUseID: "call_1",
			ToolName:  "read",
			ToolInput: map[string]interface{}{"path": "a.go"},
		}},
		Usage: CoreUsage{InputTokens: 1, OutputTokens: 2},
	}

	openai := CoreToOpenAIResponse(core, "gpt")
	if len(openai.Choices) != 1 || openai.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("unexpected openai response: %+v", openai)
	}
	calls := openai.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "read" {
		t.Fatalf("tool call mismatch: %+v", calls)
	}
	if openai.Usage.TotalTokens != 3 {
		t.Fatalf("usage mismatch: %+v", openai.Usage)
	}
}

func TestReverseTranslateRequest_MapsOpenAIToolsToAnthropicTools(t *testing.T) {
	req := &OpenAIRequest{
		Model:     "mock-model",
		MaxTokens: 20,
		Messages:  []OpenAIMessage{{Role: "user", Content: "weather?"}},
		Tools: []OpenAITool{{
			Type: "function",
			Function: OpenAIToolFunctionDef{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		}},
		ToolChoice: "required",
	}

	anthropic, err := ReverseTranslateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(anthropic.Tools) != 1 {
		t.Fatalf("expected one tool, got %+v", anthropic.Tools)
	}
	if anthropic.Tools[0].Name != "get_weather" || anthropic.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tool mismatch: %+v", anthropic.Tools[0])
	}
	if anthropic.ToolChoice == nil || anthropic.ToolChoice.Type != "any" {
		t.Fatalf("tool_choice mismatch: %+v", anthropic.ToolChoice)
	}
}

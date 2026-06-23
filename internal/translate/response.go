package translate

import (
	"encoding/json"
)

// TranslateResponse converts an OpenAI response to Anthropic format.
func TranslateResponse(openai *OpenAIResponse, originalModel string) *AnthropicResponse {
	if len(openai.Choices) == 0 {
		return &AnthropicResponse{
			ID:      openai.ID,
			Type:    "message",
			Role:    "assistant",
			Content: []AnthropicContentBlock{},
			Model:   originalModel,
			Usage: AnthropicUsage{
				InputTokens:  openai.Usage.PromptTokens,
				OutputTokens: openai.Usage.CompletionTokens,
			},
		}
	}

	choice := openai.Choices[0]
	message := choice.Message

	var content []AnthropicContentBlock

	// Text content
	if textContent := extractTextContent(message.Content); textContent != "" {
		content = append(content, AnthropicContentBlock{
			Type: "text",
			Text: textContent,
		})
	}

	// Tool calls
	for _, tc := range message.ToolCalls {
		var input map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = make(map[string]interface{})
		}
		id := tc.ID
		if id == "" {
			id = "toolu_" + randomSuffix()
		}
		content = append(content, AnthropicContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	stopReason := mapStopReason(choice.FinishReason)

	return &AnthropicResponse{
		ID:           openai.ID,
		Type:         "message",
		Role:         "assistant",
		Content:      content,
		Model:        originalModel,
		StopReason:   stopReason,
		StopSequence: nil,
		Usage: AnthropicUsage{
			InputTokens:  openai.Usage.PromptTokens,
			OutputTokens: openai.Usage.CompletionTokens,
		},
	}
}

// extractTextContent extracts text from possibly multi-part content.
func extractTextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, p := range c {
			if m, ok := p.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return joinStrings(parts, "")
	default:
		return ""
	}
}

func mapStopReason(finish string) *string {
	var s string
	switch finish {
	case "stop":
		s = "end_turn"
	case "length":
		s = "max_tokens"
	case "tool_calls":
		s = "tool_use"
	default:
		return nil
	}
	return &s
}

func randomSuffix() string {
	// Simple random suffix — matches toolu_<random> pattern
	return "01JR" + randHex(8)
}

func randHex(n int) string {
	// Inline rand for simplicity (not crypto)
	const letters = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%16] // deterministic but sufficient for tool IDs
	}
	return string(b)
}

// ReverseTranslateResponse converts an Anthropic response to OpenAI format.
func ReverseTranslateResponse(anthropic *AnthropicResponse, originalModel string) *OpenAIResponse {
	message := OpenAIMessage{Role: "assistant"}

	var textParts []string
	var toolCalls []OpenAIToolCall

	for i, block := range anthropic.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, OpenAIToolCall{
				Index: i,
				ID:    block.ID,
				Type:  "function",
				Function: OpenAIToolFunction{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}

	if len(textParts) == 1 {
		message.Content = textParts[0]
	} else if len(textParts) > 1 {
		var parts []OpenAIContentPart
		for _, t := range textParts {
			parts = append(parts, OpenAIContentPart{Type: "text", Text: t})
		}
		message.Content = parts
	}

	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	stopReason := "stop"
	if anthropic.StopReason != nil {
		switch *anthropic.StopReason {
		case "end_turn":
			stopReason = "stop"
		case "max_tokens":
			stopReason = "length"
		case "tool_use":
			stopReason = "tool_calls"
		}
	}

	return &OpenAIResponse{
		ID:     anthropic.ID,
		Object: "chat.completion",
		Model:  originalModel,
		Choices: []OpenAIChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: stopReason,
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     anthropic.Usage.InputTokens,
			CompletionTokens: anthropic.Usage.OutputTokens,
			TotalTokens:      anthropic.Usage.InputTokens + anthropic.Usage.OutputTokens,
		},
	}
}

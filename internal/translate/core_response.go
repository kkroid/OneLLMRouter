package translate

import (
	"encoding/json"
	"strings"
)

// OpenAIResponseToCore converts an OpenAI Chat Completions response into Core IR.
func OpenAIResponseToCore(openai *OpenAIResponse) *CoreResponse {
	core := &CoreResponse{
		ID:    openai.ID,
		Model: openai.Model,
		Role:  "assistant",
		Usage: CoreUsage{
			InputTokens:  openai.Usage.PromptTokens,
			OutputTokens: openai.Usage.CompletionTokens,
		},
	}
	if len(openai.Choices) == 0 {
		return core
	}

	choice := openai.Choices[0]
	core.StopReason = openAIStopReasonToCore(choice.FinishReason)
	message := choice.Message

	if text := extractTextContent(message.Content); text != "" {
		core.Content = append(core.Content, CoreContentBlock{
			Type: "text",
			Text: text,
		})
	}

	for _, tc := range message.ToolCalls {
		var input map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = make(map[string]interface{})
		}
		id := tc.ID
		if id == "" {
			id = "toolu_" + randomSuffix()
		}
		core.Content = append(core.Content, CoreContentBlock{
			Type:      "tool_use",
			ToolUseID: id,
			ToolName:  tc.Function.Name,
			ToolInput: input,
		})
	}

	return core
}

// CoreToAnthropicResponse converts Core IR into an Anthropic Messages response.
func CoreToAnthropicResponse(core *CoreResponse, originalModel string) *AnthropicResponse {
	content := make([]AnthropicContentBlock, 0, len(core.Content))
	for _, block := range core.Content {
		switch block.Type {
		case "text":
			content = append(content, AnthropicContentBlock{Type: "text", Text: block.Text})
		case "thinking":
			content = append(content, AnthropicContentBlock{
				Type:      "thinking",
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			content = append(content, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    block.ToolUseID,
				Name:  block.ToolName,
				Input: block.ToolInput,
			})
		}
	}

	var stopReason *string
	if core.StopReason != "" {
		s := core.StopReason
		stopReason = &s
	}

	return &AnthropicResponse{
		ID:           core.ID,
		Type:         "message",
		Role:         "assistant",
		Content:      content,
		Model:        originalModel,
		StopReason:   stopReason,
		StopSequence: nil,
		Usage: AnthropicUsage{
			InputTokens:  core.Usage.InputTokens,
			OutputTokens: core.Usage.OutputTokens,
		},
	}
}

// AnthropicResponseToCore converts an Anthropic Messages response into Core IR.
func AnthropicResponseToCore(anthropic *AnthropicResponse) *CoreResponse {
	core := &CoreResponse{
		ID:    anthropic.ID,
		Model: anthropic.Model,
		Role:  anthropic.Role,
		Usage: CoreUsage{
			InputTokens:  anthropic.Usage.InputTokens,
			OutputTokens: anthropic.Usage.OutputTokens,
		},
	}
	if anthropic.StopReason != nil {
		core.StopReason = *anthropic.StopReason
	}

	for _, block := range anthropic.Content {
		core.Content = append(core.Content, anthropicBlockToCore(block))
	}

	return core
}

// CoreToOpenAIResponse converts Core IR into an OpenAI Chat Completions response.
func CoreToOpenAIResponse(core *CoreResponse, originalModel string) *OpenAIResponse {
	message := OpenAIMessage{Role: "assistant"}
	var textParts []string
	var toolCalls []OpenAIToolCall

	for i, block := range core.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, sanitizeUTF8(block.Text))
		case "thinking":
			t := block.Thinking
			if t == "" {
				t = block.Text
			}
			if t != "" {
				textParts = append(textParts, sanitizeUTF8(t))
			}
		case "tool_use":
			toolCalls = append(toolCalls, coreToolUseToOpenAIResponse(block, i))
		}
	}

	if len(textParts) > 0 {
		message.Content = strings.Join(textParts, "\n")
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	return &OpenAIResponse{
		ID:     core.ID,
		Object: "chat.completion",
		Model:  originalModel,
		Choices: []OpenAIChoice{{
			Index:        0,
			Message:      message,
			FinishReason: coreStopReasonToOpenAI(core.StopReason),
		}},
		Usage: OpenAIUsage{
			PromptTokens:     core.Usage.InputTokens,
			CompletionTokens: core.Usage.OutputTokens,
			TotalTokens:      core.Usage.InputTokens + core.Usage.OutputTokens,
		},
	}
}

func coreToolUseToOpenAIResponse(block CoreContentBlock, index int) OpenAIToolCall {
	args, _ := marshalJSON(block.ToolInput)
	return OpenAIToolCall{
		Index: index,
		ID:    block.ToolUseID,
		Type:  "function",
		Function: OpenAIToolFunction{
			Name:      block.ToolName,
			Arguments: args,
		},
	}
}

func openAIStopReasonToCore(finish string) string {
	switch finish {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return ""
	}
}

func coreStopReasonToOpenAI(stopReason string) string {
	switch stopReason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

func marshalJSON(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

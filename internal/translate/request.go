package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TranslateRequest converts an Anthropic request to OpenAI format.
func TranslateRequest(req *AnthropicRequest) (*OpenAIRequest, error) {
	core, err := AnthropicRequestToCore(req)
	if err != nil {
		return nil, err
	}
	return CoreToOpenAIRequest(core)
}

func handleUserMessage(msg *AnthropicMessage) ([]OpenAIMessage, error) {
	switch c := msg.Content.(type) {
	case string:
		return []OpenAIMessage{{Role: "user", Content: c}}, nil
	case []interface{}:
		return handleUserBlocks(c)
	default:
		return nil, fmt.Errorf("unexpected user content type: %T", msg.Content)
	}
}

func handleUserBlocks(blocks []interface{}) ([]OpenAIMessage, error) {
	var toolResults []AnthropicContentBlock
	var otherBlocks []AnthropicContentBlock

	for _, b := range blocks {
		block, err := parseContentBlock(b)
		if err != nil {
			return nil, err
		}
		if block.Type == "tool_result" {
			toolResults = append(toolResults, block)
		} else {
			otherBlocks = append(otherBlocks, block)
		}
	}

	var messages []OpenAIMessage

	// Tool results → separate tool messages
	for _, block := range toolResults {
		messages = append(messages, OpenAIMessage{
			Role:       "tool",
			ToolCallID: block.ToolUseID,
			Content:    block.Content,
		})
	}

	// Text + image blocks
	if len(otherBlocks) > 0 {
		var parts []OpenAIContentPart
		for _, block := range otherBlocks {
			switch block.Type {
			case "text":
				parts = append(parts, OpenAIContentPart{
					Type: "text",
					Text: block.Text,
				})
			case "image":
				parts = append(parts, OpenAIContentPart{
					Type: "image_url",
					ImageURL: &OpenAIImageURL{
						URL: fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data),
					},
				})
			}
		}

		var content interface{}
		if len(parts) == 1 && parts[0].Type == "text" {
			content = parts[0].Text
		} else {
			content = parts
		}

		messages = append(messages, OpenAIMessage{
			Role:    "user",
			Content: content,
		})
	}

	return messages, nil
}

func handleAssistantMessage(msg *AnthropicMessage) ([]OpenAIMessage, error) {
	switch c := msg.Content.(type) {
	case string:
		return []OpenAIMessage{{Role: "assistant", Content: c}}, nil
	case []interface{}:
		return handleAssistantBlocks(c)
	default:
		return nil, fmt.Errorf("unexpected assistant content type: %T", msg.Content)
	}
}

func handleAssistantBlocks(blocks []interface{}) ([]OpenAIMessage, error) {
	var textParts []string
	var toolCalls []OpenAIToolCall

	for i, b := range blocks {
		block, err := parseContentBlock(b)
		if err != nil {
			return nil, err
		}
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			args := "{}"
			if block.Input != nil {
				if j, err := json.Marshal(block.Input); err == nil {
					args = string(j)
				}
			}
			toolCalls = append(toolCalls, OpenAIToolCall{
				Index: i,
				ID:    block.ID,
				Type:  "function",
				Function: OpenAIToolFunction{
					Name:      block.Name,
					Arguments: args,
				},
			})
		}
	}

	msg := OpenAIMessage{
		Role: "assistant",
	}
	if len(textParts) > 0 {
		msg.Content = joinStrings(textParts, "\n")
	} else {
		msg.Content = nil
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return []OpenAIMessage{msg}, nil
}

func translateTools(tools []AnthropicTool) []OpenAITool {
	result := make([]OpenAITool, len(tools))
	for i, t := range tools {
		result[i] = OpenAITool{
			Type: "function",
			Function: OpenAIToolFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return result
}

func translateToolChoice(tc *AnthropicToolChoice) interface{} {
	if tc == nil {
		return nil
	}
	switch tc.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		return map[string]interface{}{
			"type": "function",
			"function": map[string]string{
				"name": tc.Name,
			},
		}
	}
	return nil
}

// parseContentBlock converts a generic map to AnthropicContentBlock.
func parseContentBlock(b interface{}) (AnthropicContentBlock, error) {
	m, ok := b.(map[string]interface{})
	if !ok {
		return AnthropicContentBlock{}, fmt.Errorf("content block is not a map: %T", b)
	}

	block := AnthropicContentBlock{}
	if t, ok := m["type"].(string); ok {
		block.Type = t
	}
	if t, ok := m["text"].(string); ok {
		block.Text = t
	}
	if t, ok := m["thinking"].(string); ok {
		block.Thinking = t
	}
	if s, ok := m["signature"].(string); ok {
		block.Signature = s
	}
	if n, ok := m["name"].(string); ok {
		block.Name = n
	}
	if id, ok := m["id"].(string); ok {
		block.ID = id
	}
	if tid, ok := m["tool_use_id"].(string); ok {
		block.ToolUseID = tid
	}
	if input, ok := m["input"].(map[string]interface{}); ok {
		block.Input = input
	}
	if content, ok := m["content"]; ok {
		block.Content = content
	}
	if src, ok := m["source"].(map[string]interface{}); ok {
		block.Source = &ImageSource{}
		if st, ok := src["type"].(string); ok {
			block.Source.Type = st
		}
		if mt, ok := src["media_type"].(string); ok {
			block.Source.MediaType = mt
		}
		if d, ok := src["data"].(string); ok {
			block.Source.Data = d
		}
	}
	return block, nil
}

// extractSystemText handles system as string or []contentBlock.
func extractSystemText(system interface{}) string {
	switch s := system.(type) {
	case string:
		return s
	case []interface{}:
		var parts []string
		for _, block := range s {
			if m, ok := block.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return joinStrings(parts, "\n")
	default:
		return ""
	}
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}

// ReverseTranslateRequest converts an OpenAI Chat Completions request to Anthropic Messages format.
func ReverseTranslateRequest(req *OpenAIRequest) (*AnthropicRequest, error) {
	messages := make([]AnthropicMessage, 0, len(req.Messages))

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			// System messages become system prompt (only last one wins for simplicity)
			// We handle this below
		case "user", "assistant", "tool":
			m := reverseConvertMessage(&msg)
			if m != nil {
				messages = append(messages, *m)
			}
		}
	}

	// Build system prompt from system messages
	var systemParts []string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if s, ok := msg.Content.(string); ok {
				systemParts = append(systemParts, s)
			}
		}
	}

	anthropicReq := &AnthropicRequest{
		Model:         req.Model,
		Messages:      messages,
		System:        joinStringParts(systemParts, "\n"),
		MaxTokens:     req.MaxTokens,
		Stream:        req.Stream,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
		Tools:         reverseTranslateTools(req.Tools),
		ToolChoice:    reverseTranslateToolChoice(req.ToolChoice),
	}

	return anthropicReq, nil
}

func reverseTranslateTools(tools []OpenAITool) []AnthropicTool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]AnthropicTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		if tool.Function.Name == "" {
			continue
		}
		result = append(result, AnthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}
	return result
}

func reverseTranslateToolChoice(toolChoice interface{}) *AnthropicToolChoice {
	switch tc := toolChoice.(type) {
	case string:
		switch tc {
		case "auto":
			return &AnthropicToolChoice{Type: "auto"}
		case "required":
			return &AnthropicToolChoice{Type: "any"}
		case "none", "":
			return nil
		}
	case map[string]interface{}:
		if tc["type"] != "function" {
			return nil
		}
		fn, ok := tc["function"].(map[string]interface{})
		if !ok {
			return nil
		}
		name, ok := fn["name"].(string)
		if !ok || name == "" {
			return nil
		}
		return &AnthropicToolChoice{Type: "tool", Name: name}
	case map[string]string:
		if tc["type"] == "function" && tc["name"] != "" {
			return &AnthropicToolChoice{Type: "tool", Name: tc["name"]}
		}
	}
	return nil
}

func reverseConvertMessage(msg *OpenAIMessage) *AnthropicMessage {
	var contentBlocks []AnthropicContentBlock

	switch msg.Role {
	case "user":
		if c, ok := msg.Content.(string); ok {
			return &AnthropicMessage{Role: "user", Content: c}
		}
		if parts, ok := msg.Content.([]interface{}); ok {
			for _, p := range parts {
				if m, ok := p.(map[string]interface{}); ok {
					switch m["type"] {
					case "text":
						if t, ok := m["text"].(string); ok {
							contentBlocks = append(contentBlocks, AnthropicContentBlock{Type: "text", Text: t})
						}
					case "image_url":
						if url, ok := m["image_url"].(map[string]interface{}); ok {
							if u, ok := url["url"].(string); ok {
								contentBlocks = append(contentBlocks, AnthropicContentBlock{
									Type: "image",
									Source: &ImageSource{
										Type:      "base64",
										MediaType: "image/png",
										Data:      extractBase64(u),
									},
								})
							}
						}
					}
				}
			}
			if len(contentBlocks) > 0 {
				return &AnthropicMessage{Role: "user", Content: contentBlocks}
			}
		}
	case "assistant":
		if c, ok := msg.Content.(string); ok && c != "" {
			contentBlocks = append(contentBlocks, AnthropicContentBlock{Type: "text", Text: c})
		}
		for _, tc := range msg.ToolCalls {
			var input map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			if input == nil {
				input = map[string]interface{}{}
			}
			contentBlocks = append(contentBlocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
		if len(contentBlocks) > 0 {
			return &AnthropicMessage{Role: "assistant", Content: contentBlocks}
		}
	case "tool":
		content := ""
		if c, ok := msg.Content.(string); ok {
			content = c
		}
		return &AnthropicMessage{
			Role: "user",
			Content: []AnthropicContentBlock{{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   content,
			}},
		}
	}
	return nil
}

func extractBase64(dataURL string) string {
	// data:image/png;base64,iVBOR... → iVBOR...
	if idx := strings.LastIndex(dataURL, ","); idx >= 0 {
		return dataURL[idx+1:]
	}
	return dataURL
}

func joinStringParts(parts []string, sep string) string {
	var result string
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

package translate

import (
	"fmt"
)

// AnthropicRequestToCore converts an Anthropic Messages request into Core IR.
func AnthropicRequestToCore(req *AnthropicRequest) (*CoreRequest, error) {
	core := &CoreRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Stream:        req.Stream,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.StopSequences,
	}

	if req.Metadata != nil {
		core.Metadata = map[string]string{}
		if req.Metadata.UserID != "" {
			core.Metadata["user_id"] = req.Metadata.UserID
		}
	}

	core.System = coreSystemBlocks(req.System)

	for _, msg := range req.Messages {
		blocks, err := anthropicMessageContentToCore(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("convert %s message: %w", msg.Role, err)
		}
		core.Messages = append(core.Messages, CoreMessage{
			Role:    msg.Role,
			Content: blocks,
		})
	}

	if len(req.Tools) > 0 {
		core.Tools = make([]CoreTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			core.Tools = append(core.Tools, CoreTool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
	}
	if req.ToolChoice != nil {
		core.ToolChoice = &CoreToolChoice{
			Type: req.ToolChoice.Type,
			Name: req.ToolChoice.Name,
		}
	}

	return core, nil
}

// CoreToOpenAIRequest converts Core IR into an OpenAI Chat Completions request.
func CoreToOpenAIRequest(core *CoreRequest) (*OpenAIRequest, error) {
	messages := make([]OpenAIMessage, 0, len(core.Messages)+len(core.System))

	if len(core.System) > 0 {
		if systemText := coreText(core.System, "\n"); systemText != "" {
			messages = append(messages, OpenAIMessage{
				Role:    "system",
				Content: systemText,
			})
		}
	}

	for _, msg := range core.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, coreUserMessageToOpenAI(msg.Content)...)
		case "assistant":
			messages = append(messages, coreAssistantMessageToOpenAI(msg.Content))
		}
	}

	openaiReq := &OpenAIRequest{
		Model:       core.Model,
		Messages:    messages,
		MaxTokens:   core.MaxTokens,
		Stream:      core.Stream,
		Temperature: core.Temperature,
		TopP:        core.TopP,
		Stop:        core.StopSequences,
	}

	if len(core.Tools) > 0 {
		openaiReq.Tools = make([]OpenAITool, 0, len(core.Tools))
		for _, tool := range core.Tools {
			openaiReq.Tools = append(openaiReq.Tools, OpenAITool{
				Type: "function",
				Function: OpenAIToolFunctionDef{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
		openaiReq.ToolChoice = coreToolChoiceToOpenAI(core.ToolChoice)
	}

	return openaiReq, nil
}

func coreSystemBlocks(system interface{}) []CoreContentBlock {
	switch s := system.(type) {
	case string:
		if s == "" {
			return nil
		}
		return []CoreContentBlock{{Type: "text", Text: s}}
	case []interface{}:
		var blocks []CoreContentBlock
		for _, raw := range s {
			block, err := parseContentBlock(raw)
			if err != nil || block.Type != "text" {
				continue
			}
			blocks = append(blocks, CoreContentBlock{Type: "text", Text: block.Text})
		}
		return blocks
	default:
		return nil
	}
}

func anthropicMessageContentToCore(content interface{}) ([]CoreContentBlock, error) {
	switch c := content.(type) {
	case string:
		return []CoreContentBlock{{Type: "text", Text: c}}, nil
	case []interface{}:
		blocks := make([]CoreContentBlock, 0, len(c))
		for _, raw := range c {
			block, err := parseContentBlock(raw)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, anthropicBlockToCore(block))
		}
		return blocks, nil
	case []AnthropicContentBlock:
		blocks := make([]CoreContentBlock, 0, len(c))
		for _, block := range c {
			blocks = append(blocks, anthropicBlockToCore(block))
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("unexpected content type: %T", content)
	}
}

func anthropicBlockToCore(block AnthropicContentBlock) CoreContentBlock {
	core := CoreContentBlock{
		Type:              block.Type,
		Text:              block.Text,
		Thinking:          block.Thinking,
		Signature:         block.Signature,
		ToolUseID:         block.ToolUseID,
		ToolName:          block.Name,
		ToolInput:         block.Input,
		ToolResultContent: block.Content,
	}
	if block.Type == "tool_use" {
		core.ToolUseID = block.ID
	}
	if block.Type == "image" && block.Source != nil {
		core.Image = &CoreImage{
			MediaType: block.Source.MediaType,
			Data:      block.Source.Data,
		}
	}
	return core
}

func coreUserMessageToOpenAI(blocks []CoreContentBlock) []OpenAIMessage {
	var toolResults []CoreContentBlock
	var otherBlocks []CoreContentBlock
	for _, block := range blocks {
		if block.Type == "tool_result" {
			toolResults = append(toolResults, block)
		} else {
			otherBlocks = append(otherBlocks, block)
		}
	}

	var messages []OpenAIMessage
	for _, block := range toolResults {
		messages = append(messages, OpenAIMessage{
			Role:       "tool",
			ToolCallID: block.ToolUseID,
			Content:    block.ToolResultContent,
		})
	}

	if len(otherBlocks) == 0 {
		return messages
	}

	parts := make([]OpenAIContentPart, 0, len(otherBlocks))
	for _, block := range otherBlocks {
		switch block.Type {
		case "text":
			parts = append(parts, OpenAIContentPart{Type: "text", Text: block.Text})
		case "image":
			if block.Image != nil {
				parts = append(parts, OpenAIContentPart{
					Type: "image_url",
					ImageURL: &OpenAIImageURL{
						URL: fmt.Sprintf("data:%s;base64,%s", block.Image.MediaType, block.Image.Data),
					},
				})
			}
		}
	}
	if len(parts) == 0 {
		return messages
	}

	var content interface{} = parts
	if len(parts) == 1 && parts[0].Type == "text" {
		content = parts[0].Text
	}
	messages = append(messages, OpenAIMessage{Role: "user", Content: content})
	return messages
}

func coreAssistantMessageToOpenAI(blocks []CoreContentBlock) OpenAIMessage {
	var textParts []string
	var toolCalls []OpenAIToolCall
	for i, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, coreToolUseToOpenAI(block, i))
		}
	}

	msg := OpenAIMessage{Role: "assistant"}
	if len(textParts) > 0 {
		msg.Content = joinStrings(textParts, "\n")
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		if msg.Content == nil {
			msg.Content = nil
		}
	}
	return msg
}

func coreToolUseToOpenAI(block CoreContentBlock, index int) OpenAIToolCall {
	args := "{}"
	if block.ToolInput != nil {
		if j, err := marshalJSON(block.ToolInput); err == nil {
			args = j
		}
	}
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

func coreText(blocks []CoreContentBlock, sep string) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return joinStrings(parts, sep)
}

func coreToolChoiceToOpenAI(tc *CoreToolChoice) interface{} {
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
	default:
		return nil
	}
}

package translate

import (
	"fmt"
	"sort"
)

// StreamContext tracks state across SSE stream chunks.
type StreamContext struct {
	MessageStartSent bool
	MessageID        string
	Model            string
	ContentBlockIdx  int
	ContentBlockOpen bool
	ActiveToolIdx    int
	ToolCalls        map[int]*ToolCallState
}

type ToolCallState struct {
	ID        string
	Name      string
	Args      string
	BlockSent bool
}

func TranslateStreamChunk(chunk *OpenAIStreamChunk, ctx *StreamContext) ([]SSEEvent, error) {
	coreEvents, err := OpenAIStreamChunkToCoreEvents(chunk, ctx)
	if err != nil {
		return nil, err
	}
	return CoreStreamEventsToAnthropicSSE(coreEvents), nil
}

// OpenAIStreamChunkToCoreEvents converts OpenAI stream deltas to Core stream
// events while preserving the existing Anthropic block state machine.
func OpenAIStreamChunkToCoreEvents(chunk *OpenAIStreamChunk, ctx *StreamContext) ([]CoreStreamEvent, error) {
	var coreEvents []CoreStreamEvent

	if len(chunk.Choices) == 0 {
		return coreEvents, nil
	}

	delta := chunk.Choices[0].Delta

	if !ctx.MessageStartSent {
		ctx.MessageStartSent = true
		ctx.ActiveToolIdx = -1
		coreEvents = append(coreEvents, CoreStreamEvent{
			Type:  "message_start",
			ID:    ctx.MessageID,
			Model: ctx.Model,
		})
	}

	hasToolCalls := len(delta.ToolCalls) > 0
	if hasToolCalls {
		for _, tc := range delta.ToolCalls {
			idx := tc.Index

			if ctx.ToolCalls[idx] == nil {
				ctx.ToolCalls[idx] = &ToolCallState{
					ID:   tc.ID,
					Name: tc.funcName(),
				}
			}
			if tc.ID != "" {
				ctx.ToolCalls[idx].ID = tc.ID
			}
			if tc.funcName() != "" {
				ctx.ToolCalls[idx].Name = tc.funcName()
			}
			ctx.ToolCalls[idx].Args += tc.funcArgs()
		}
	}

	if delta.Content != "" && !hasToolCalls {
		if !ctx.ContentBlockOpen {
			ctx.ActiveToolIdx = -1
			ci := ctx.ContentBlockIdx
			coreEvents = append(coreEvents, CoreStreamEvent{
				Type:  "content_block_start",
				Index: ci,
				ContentBlock: &CoreContentBlock{
					Type: "text",
					Text: "",
				},
			})
			ctx.ContentBlockOpen = true
		}
		ci := ctx.ContentBlockIdx
		coreEvents = append(coreEvents, CoreStreamEvent{
			Type:  "content_block_delta",
			Index: ci,
			Text:  delta.Content,
		})
	}

	finishReason := chunk.Choices[0].FinishReason
	if finishReason != nil && *finishReason != "" {
		if ctx.ContentBlockOpen {
			ci := ctx.ContentBlockIdx
			coreEvents = append(coreEvents, CoreStreamEvent{
				Type:  "content_block_stop",
				Index: ci,
			})
			ctx.ContentBlockOpen = false
			ctx.ContentBlockIdx++
		}
		if len(ctx.ToolCalls) > 0 {
			indices := make([]int, 0, len(ctx.ToolCalls))
			for index := range ctx.ToolCalls {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			for _, toolIndex := range indices {
				toolCall := ctx.ToolCalls[toolIndex]
				if toolCall.Name == "" {
					return nil, fmt.Errorf("tool call %d missing tool name", toolIndex)
				}
				if toolCall.ID == "" {
					return nil, fmt.Errorf("tool call %d missing tool id", toolIndex)
				}

				blockIndex := ctx.ContentBlockIdx
				coreEvents = append(coreEvents, CoreStreamEvent{
					Type:  "content_block_start",
					Index: blockIndex,
					ContentBlock: &CoreContentBlock{
						Type:      "tool_use",
						ToolUseID: toolCall.ID,
						ToolName:  toolCall.Name,
						ToolInput: map[string]interface{}{},
					},
				})
				arguments := toolCall.Args
				if arguments == "" {
					arguments = "{}"
				}
				coreEvents = append(coreEvents, CoreStreamEvent{
					Type:        "content_block_delta",
					Index:       blockIndex,
					PartialJSON: arguments,
				})
				coreEvents = append(coreEvents, CoreStreamEvent{
					Type:  "content_block_stop",
					Index: blockIndex,
				})
				toolCall.BlockSent = true
				ctx.ContentBlockIdx++
			}
		}
		st := mapAnthropicStopReason(*finishReason)
		coreEvents = append(coreEvents, CoreStreamEvent{
			Type:       "message_delta",
			StopReason: st,
			Usage:      CoreUsage{OutputTokens: 0},
		})
		coreEvents = append(coreEvents, CoreStreamEvent{
			Type: "message_stop",
		})
	}

	return coreEvents, nil
}

// CoreStreamEventsToAnthropicSSE converts Core stream events to Anthropic SSE DTOs.
func CoreStreamEventsToAnthropicSSE(coreEvents []CoreStreamEvent) []SSEEvent {
	events := make([]SSEEvent, 0, len(coreEvents))
	for _, event := range coreEvents {
		switch event.Type {
		case "message_start":
			events = append(events, SSEEvent{
				Type: "message_start",
				Message: &AnthropicResponse{
					ID:      event.ID,
					Type:    "message",
					Role:    "assistant",
					Content: []AnthropicContentBlock{},
					Model:   event.Model,
					Usage: AnthropicUsage{
						InputTokens:  0,
						OutputTokens: 0,
					},
				},
			})
		case "content_block_start":
			ci := event.Index
			events = append(events, SSEEvent{
				Type:         "content_block_start",
				Index:        &ci,
				ContentBlock: coreBlockToAnthropicStreamBlock(event.ContentBlock),
			})
		case "content_block_delta":
			ci := event.Index
			delta := &SSEDelta{}
			if event.PartialJSON != "" {
				delta.Type = "input_json_delta"
				delta.PartialJSON = event.PartialJSON
			} else {
				delta.Type = "text_delta"
				delta.Text = event.Text
			}
			events = append(events, SSEEvent{
				Type:  "content_block_delta",
				Index: &ci,
				Delta: delta,
			})
		case "content_block_stop":
			ci := event.Index
			events = append(events, SSEEvent{
				Type:  "content_block_stop",
				Index: &ci,
			})
		case "message_delta":
			events = append(events, SSEEvent{
				Type: "message_delta",
				Delta: &SSEDelta{
					StopReason: event.StopReason,
				},
				Usage: &SSEUsage{OutputTokens: event.Usage.OutputTokens},
			})
		case "message_stop":
			events = append(events, SSEEvent{Type: "message_stop"})
		}
	}
	return events
}

func coreBlockToAnthropicStreamBlock(block *CoreContentBlock) map[string]interface{} {
	if block == nil {
		return nil
	}
	switch block.Type {
	case "tool_use":
		return map[string]interface{}{
			"type":  "tool_use",
			"id":    block.ToolUseID,
			"name":  block.ToolName,
			"input": map[string]interface{}{},
		}
	case "text":
		return map[string]interface{}{
			"type": "text",
			"text": block.Text,
		}
	default:
		return map[string]interface{}{"type": block.Type}
	}
}

func mapAnthropicStopReason(finish string) string {
	switch finish {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func (tc *OpenAIToolCallDelta) funcName() string {
	if tc.Function != nil {
		return tc.Function.Name
	}
	return ""
}

func (tc *OpenAIToolCallDelta) funcArgs() string {
	if tc.Function != nil {
		return tc.Function.Arguments
	}
	return ""
}

func GenerateMessageID() string {
	return fmt.Sprintf("msg_%s", randHex(16))
}

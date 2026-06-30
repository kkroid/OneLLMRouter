package translate

// ========== Anthropic Request (what we receive) ==========

// AnthropicContentBlock is a content block in Anthropic messages.
type AnthropicContentBlock struct {
	Type         string                 `json:"type"`                    // "text" | "image" | "tool_use" | "tool_result" | "thinking"
	Text         string                 `json:"text,omitempty"`
	Thinking     string                 `json:"thinking,omitempty"`      // for "thinking" (DeepSeek)
	Signature    string                 `json:"signature,omitempty"`     // for "thinking" (DeepSeek)
	Source       *ImageSource           `json:"source,omitempty"`       // for "image"
	ID           string                 `json:"id,omitempty"`           // for "tool_use"
	Name         string                 `json:"name,omitempty"`         // for "tool_use"
	Input        map[string]interface{} `json:"input,omitempty"`        // for "tool_use"
	ToolUseID    string                 `json:"tool_use_id,omitempty"`  // for "tool_result"
	Content      interface{}            `json:"content,omitempty"`      // for "tool_result" (string or []ContentBlock)
}

// ImageSource is the source of an image content block.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// AnthropicMessage is a single message in an Anthropic request.
type AnthropicMessage struct {
	Role    string                   `json:"role"`    // "user" | "assistant"
	Content interface{}              `json:"content"` // string or []AnthropicContentBlock
}

// AnthropicRequest is the top-level Anthropic Messages API request.
type AnthropicRequest struct {
	Model         string              `json:"model"`
	Messages      []AnthropicMessage  `json:"messages"`
	System        interface{}         `json:"system,omitempty"` // string or []contentBlock
	MaxTokens     int                 `json:"max_tokens,omitempty"`
	Stream        bool                `json:"stream,omitempty"`
	Temperature   *float64            `json:"temperature,omitempty"`
	TopP          *float64            `json:"top_p,omitempty"`
	StopSequences []string            `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool     `json:"tools,omitempty"`
	ToolChoice    *AnthropicToolChoice `json:"tool_choice,omitempty"`
	Metadata      *AnthropicMetadata  `json:"metadata,omitempty"`
}

// AnthropicTool is a tool definition in an Anthropic request.
type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// AnthropicToolChoice specifies how the model should use tools.
type AnthropicToolChoice struct {
	Type string `json:"type"`          // "auto" | "any" | "tool"
	Name string `json:"name,omitempty"` // for "tool" type
}

// AnthropicMetadata contains optional metadata.
type AnthropicMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// ========== Anthropic Response (what we return) ==========

// AnthropicResponse is the top-level Anthropic Messages API response.
type AnthropicResponse struct {
	ID           string                   `json:"id"`
	Type         string                   `json:"type"` // "message"
	Role         string                   `json:"role"` // "assistant"
	Content      []AnthropicContentBlock  `json:"content"`
	Model        string                   `json:"model"`
	StopReason   *string                  `json:"stop_reason"`
	StopSequence *string                  `json:"stop_sequence"`
	Usage        AnthropicUsage           `json:"usage"`
}

// AnthropicUsage contains token usage info.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ========== Anthropic SSE Events (streaming) ==========

// SSEEvent represents an Anthropic streaming event.
type SSEEvent struct {
	Type         string      `json:"type"`                    // message_start | content_block_start | content_block_delta | content_block_stop | message_delta | message_stop | ping
	Message      interface{} `json:"message,omitempty"`       // for message_start
	Index        *int        `json:"index,omitempty"`         // for content_block_*
	ContentBlock interface{} `json:"content_block,omitempty"` // for content_block_start
	Delta        *SSEDelta   `json:"delta,omitempty"`         // for content_block_delta | message_delta
	Usage        *SSEUsage   `json:"usage,omitempty"`         // for message_delta
}

// SSEDelta represents a delta in streaming events.
type SSEDelta struct {
	Type         string `json:"type,omitempty"`          // "text_delta" | "input_json_delta"
	Text         string `json:"text,omitempty"`          // for text_delta
	PartialJSON  string `json:"partial_json,omitempty"`  // for input_json_delta
	StopReason   string `json:"stop_reason,omitempty"`   // for message_delta
}

// SSEUsage contains usage info in streaming events.
type SSEUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// ========== OpenAI Request (what we send to Copilot) ==========

// OpenAIMessage is a message in an OpenAI-format request.
type OpenAIMessage struct {
	Role       string           `json:"role"`                    // "system" | "user" | "assistant" | "tool"
	Content    interface{}      `json:"content"`                 // string or []OpenAIContentPart
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`    // for assistant
	ToolCallID string           `json:"tool_call_id,omitempty"`  // for tool
}

// OpenAIContentPart is a multi-part content element.
type OpenAIContentPart struct {
	Type     string            `json:"type"`               // "text" | "image_url"
	Text     string            `json:"text,omitempty"`
	ImageURL *OpenAIImageURL   `json:"image_url,omitempty"`
}

// OpenAIImageURL wraps an image URL or data URI.
type OpenAIImageURL struct {
	URL string `json:"url"`
}

// OpenAIToolCall is a tool call in an OpenAI assistant message.
type OpenAIToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"` // "function"
	Function OpenAIToolFunction `json:"function"`
}

// OpenAIToolFunction is the function within a tool call.
type OpenAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAIRequest is the top-level OpenAI Chat Completions request.
type OpenAIRequest struct {
	Model      string           `json:"model"`
	Messages   []OpenAIMessage  `json:"messages"`
	MaxTokens  int              `json:"max_tokens"`
	Stream     bool             `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP       *float64         `json:"top_p,omitempty"`
	Stop       []string         `json:"stop,omitempty"`
	Tools      []OpenAITool     `json:"tools,omitempty"`
	ToolChoice interface{}      `json:"tool_choice,omitempty"` // string or object
}

// OpenAITool is a tool definition in OpenAI format.
type OpenAITool struct {
	Type     string              `json:"type"` // "function"
	Function OpenAIToolFunctionDef `json:"function"`
}

// OpenAIToolFunctionDef defines a function for OpenAI tools.
type OpenAIToolFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ========== OpenAI Response ==========

// OpenAIResponse is the OpenAI Chat Completions response.
type OpenAIResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Model   string            `json:"model"`
	Choices []OpenAIChoice    `json:"choices"`
	Usage   OpenAIUsage       `json:"usage"`
}

// OpenAIChoice is a single choice in an OpenAI response.
type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      OpenAIMessage  `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

// OpenAIUsage contains token usage for OpenAI.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ========== OpenAI Stream Chunk ==========

// OpenAIStreamChunk is a single chunk from an OpenAI SSE stream.
type OpenAIStreamChunk struct {
	Choices []OpenAIStreamChoice `json:"choices"`
}

// OpenAIStreamChoice is a choice within a stream chunk.
type OpenAIStreamChoice struct {
	Index        int              `json:"index"`
	Delta        OpenAIStreamDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

// OpenAIStreamDelta is the delta content in a stream chunk.
type OpenAIStreamDelta struct {
	Role     string                       `json:"role,omitempty"`
	Content  string                       `json:"content,omitempty"`
	ToolCalls []OpenAIToolCallDelta       `json:"tool_calls,omitempty"`
}

// OpenAIToolCallDelta is a tool call delta in stream.
type OpenAIToolCallDelta struct {
	Index    int                       `json:"index"`
	ID       string                    `json:"id,omitempty"`
	Function *OpenAIToolFunctionDelta  `json:"function,omitempty"`
}

// OpenAIToolFunctionDelta is the function delta in stream.
type OpenAIToolFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

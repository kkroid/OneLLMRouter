package translate

// CoreRequest is the protocol-neutral request shape used inside the translator.
// It intentionally covers only the protocol surface OneLLMRouter already
// supports.
type CoreRequest struct {
	Model         string
	Messages      []CoreMessage
	System        []CoreContentBlock
	MaxTokens     int
	Stream        bool
	Temperature   *float64
	TopP          *float64
	StopSequences []string
	Tools         []CoreTool
	ToolChoice    *CoreToolChoice
	Metadata      map[string]string
}

// CoreMessage is a protocol-neutral conversation message.
type CoreMessage struct {
	Role    string
	Content []CoreContentBlock
}

// CoreContentBlock represents a single protocol-neutral content block.
type CoreContentBlock struct {
	Type string // text | image | tool_use | tool_result | thinking

	Text      string
	Thinking  string
	Signature string
	Image     *CoreImage

	ToolUseID         string
	ToolName          string
	ToolInput         map[string]interface{}
	ToolResultContent interface{}
}

// CoreImage is a base64 image payload.
type CoreImage struct {
	MediaType string
	Data      string
}

// CoreTool describes a model-callable tool.
type CoreTool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

// CoreToolChoice describes tool selection behavior.
type CoreToolChoice struct {
	Type string
	Name string
}

// CoreUsage stores input/output token usage.
type CoreUsage struct {
	InputTokens  int
	OutputTokens int
}

// CoreResponse is the protocol-neutral response shape used inside the translator.
type CoreResponse struct {
	ID         string
	Model      string
	Role       string
	Content    []CoreContentBlock
	StopReason string
	Usage      CoreUsage
}

// CoreStreamEvent represents a protocol-neutral streaming event.
type CoreStreamEvent struct {
	Type string

	ID           string
	Model        string
	Index        int
	ContentBlock *CoreContentBlock
	Text         string
	PartialJSON  string
	StopReason   string
	Usage        CoreUsage
}

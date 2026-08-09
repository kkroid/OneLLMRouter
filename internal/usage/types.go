package usage

import "time"

// Protocol identifies the upstream API protocol used by a request.
type Protocol string

const (
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
	ProtocolOpenAIChat        Protocol = "openai_chat"
	ProtocolOpenAIResponses   Protocol = "openai_responses"
)

// Source identifies where usage was observed.
type Source string

const (
	SourceResponse           Source = "response"
	SourceStream             Source = "stream"
	SourceTranslatedResponse Source = "translated_response"
)

// Status identifies the outcome of an upstream attempt.
type Status string

const (
	StatusSuccess   Status = "success"
	StatusError     Status = "error"
	StatusCancelled Status = "cancelled"
	StatusUnknown   Status = "unknown"
)

// Record describes token usage reported by one upstream attempt.
type Record struct {
	Time             time.Time `json:"time"`
	RequestID        string    `json:"request_id"`
	Provider         string    `json:"provider"`
	RequestedModel   string    `json:"requested_model"`
	UpstreamModel    string    `json:"upstream_model"`
	Protocol         Protocol  `json:"protocol"`
	InputTokens      *int      `json:"input_tokens"`
	OutputTokens     *int      `json:"output_tokens"`
	CacheReadTokens  *int      `json:"cache_read_tokens"`
	CacheWriteTokens *int      `json:"cache_write_tokens"`
	ReasoningTokens  *int      `json:"reasoning_tokens"`
	UsageSource      *Source   `json:"usage_source"`
	UpstreamAttempt  int       `json:"upstream_attempt"`
	Status           Status    `json:"status"`
}

package usage

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
)

// Writer persists one usage record.
type Writer interface {
	Write(Record) error
}

// Attempt describes the stable identity and routing metadata for one upstream attempt.
type Attempt struct {
	RequestID       string
	Provider        string
	RequestedModel  string
	UpstreamModel   string
	Protocol        Protocol
	UpstreamAttempt int
}

// Collector extracts explicit upstream usage and writes protocol-neutral records.
type Collector struct {
	writer Writer
}

func NewCollector(writer Writer) *Collector {
	return &Collector{writer: writer}
}

// CollectResponse extracts usage from one complete upstream response body.
func (c *Collector) CollectResponse(attempt Attempt, body []byte, source Source, status Status) {
	values := extractUsage(attempt.Protocol, body)
	if !hasValues(values) {
		c.write(attempt, values, nil, status)
		return
	}
	c.write(attempt, values, &source, status)
}

// CollectUnknown writes an attempt whose upstream response did not report usage.
func (c *Collector) CollectUnknown(attempt Attempt, status Status) {
	c.write(attempt, tokenValues{}, nil, status)
}

// NewStream starts incremental collection for an SSE response. Finish must be called once.
func (c *Collector) NewStream(attempt Attempt) *StreamCollector {
	return &StreamCollector{collector: c, attempt: attempt}
}

func (c *Collector) write(attempt Attempt, values tokenValues, source *Source, status Status) {
	if c == nil || c.writer == nil {
		return
	}
	_ = c.writer.Write(Record{
		RequestID:        attempt.RequestID,
		Provider:         attempt.Provider,
		RequestedModel:   attempt.RequestedModel,
		UpstreamModel:    attempt.UpstreamModel,
		Protocol:         attempt.Protocol,
		InputTokens:      values.input,
		OutputTokens:     values.output,
		CacheReadTokens:  values.cacheRead,
		CacheWriteTokens: values.cacheWrite,
		ReasoningTokens:  values.reasoning,
		UsageSource:      source,
		UpstreamAttempt:  attempt.UpstreamAttempt,
		Status:           status,
	})
}

// StreamCollector observes SSE bytes without changing their order or contents.
type StreamCollector struct {
	mu        sync.Mutex
	collector *Collector
	attempt   Attempt
	pending   []byte
	values    tokenValues
	terminal  bool
	finished  bool
}

func (s *StreamCollector) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return len(data), nil
	}
	s.pending = append(s.pending, data...)
	for {
		index := bytes.IndexByte(s.pending, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSuffix(string(s.pending[:index]), "\r")
		s.pending = s.pending[index+1:]
		s.observeLine(line)
	}
	return len(data), nil
}

func (s *StreamCollector) Finish(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.finished = true
	if len(s.pending) > 0 {
		s.observeLine(strings.TrimSuffix(string(s.pending), "\r"))
	}
	if s.terminal && hasValues(s.values) {
		source := SourceStream
		s.collector.write(s.attempt, s.values, &source, status)
		return
	}
	s.collector.write(s.attempt, tokenValues{}, nil, status)
}

func (s *StreamCollector) observeLine(line string) {
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := bytes.TrimSpace([]byte(strings.TrimPrefix(line, "data:")))
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return
	}
	values, terminal := extractStreamUsage(s.attempt.Protocol, payload)
	mergeValues(&s.values, values)
	s.terminal = s.terminal || terminal
}

type tokenValues struct {
	input      *int
	output     *int
	cacheRead  *int
	cacheWrite *int
	reasoning  *int
}

type usagePayload struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	PromptTokens             *int `json:"prompt_tokens"`
	CompletionTokens         *int `json:"completion_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
	PromptTokensDetails      *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	InputTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func extractUsage(protocol Protocol, body []byte) tokenValues {
	var envelope struct {
		Usage *usagePayload `json:"usage"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Usage == nil {
		return tokenValues{}
	}
	return valuesFromPayload(protocol, envelope.Usage)
}

func extractStreamUsage(protocol Protocol, payload []byte) (tokenValues, bool) {
	var envelope struct {
		Type     string          `json:"type"`
		Usage    *usagePayload   `json:"usage"`
		Message  json.RawMessage `json:"message"`
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return tokenValues{}, false
	}

	switch protocol {
	case ProtocolAnthropicMessages:
		if envelope.Type == "message_start" && len(envelope.Message) > 0 {
			return extractUsage(protocol, envelope.Message), false
		}
		if envelope.Type == "message_delta" && envelope.Usage != nil {
			return valuesFromPayload(protocol, envelope.Usage), true
		}
	case ProtocolOpenAIChat:
		if envelope.Usage != nil {
			return valuesFromPayload(protocol, envelope.Usage), true
		}
	case ProtocolOpenAIResponses:
		if (envelope.Type == "response.completed" || envelope.Type == "response.done") && len(envelope.Response) > 0 {
			values := extractUsage(protocol, envelope.Response)
			return values, hasValues(values)
		}
	}
	return tokenValues{}, false
}

func valuesFromPayload(protocol Protocol, payload *usagePayload) tokenValues {
	values := tokenValues{}
	switch protocol {
	case ProtocolAnthropicMessages, ProtocolOpenAIResponses:
		values.input = payload.InputTokens
		values.output = payload.OutputTokens
	case ProtocolOpenAIChat:
		values.input = payload.PromptTokens
		values.output = payload.CompletionTokens
	}
	values.cacheRead = payload.CacheReadInputTokens
	values.cacheWrite = payload.CacheCreationInputTokens
	if payload.PromptTokensDetails != nil {
		values.cacheRead = payload.PromptTokensDetails.CachedTokens
	}
	if payload.InputTokensDetails != nil {
		values.cacheRead = payload.InputTokensDetails.CachedTokens
	}
	if payload.CompletionTokensDetails != nil {
		values.reasoning = payload.CompletionTokensDetails.ReasoningTokens
	}
	if payload.OutputTokensDetails != nil {
		values.reasoning = payload.OutputTokensDetails.ReasoningTokens
	}
	return values
}

func mergeValues(target *tokenValues, incoming tokenValues) {
	if incoming.input != nil {
		target.input = incoming.input
	}
	if incoming.output != nil {
		target.output = incoming.output
	}
	if incoming.cacheRead != nil {
		target.cacheRead = incoming.cacheRead
	}
	if incoming.cacheWrite != nil {
		target.cacheWrite = incoming.cacheWrite
	}
	if incoming.reasoning != nil {
		target.reasoning = incoming.reasoning
	}
}

func hasValues(values tokenValues) bool {
	return values.input != nil || values.output != nil || values.cacheRead != nil ||
		values.cacheWrite != nil || values.reasoning != nil
}

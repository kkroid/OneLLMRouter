package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/kkroid/onellm-router/internal/upstream"
)

const (
	// The probe only holds lifecycle events. Once this bound is reached the
	// stream is handed to the client unchanged.
	responsesProbeMaxBytes           = 256 << 10
	responsesProbeMaxLifecycleEvents = 16
)

var errResponsesProbeFrameTooLarge = errors.New("responses SSE probe frame exceeds cache limit")

type responsesProbeDecision uint8

const (
	responsesProbeIgnore responsesProbeDecision = iota
	responsesProbeContinue
	responsesProbePassThrough
	responsesProbeCapacity
)

// probeResponsesSSE looks for a known capacity failure before the downstream
// HTTP response is committed. Any bytes consumed while probing are replayed
// byte-for-byte when the stream is accepted.
func probeResponsesSSE(
	ctx context.Context,
	response *http.Response,
	sanitizer *upstream.Sanitizer,
) *upstream.Failure {
	if response == nil || response.Body == nil {
		return nil
	}

	originalBody := response.Body
	reader := bufio.NewReader(originalBody)
	var prefix bytes.Buffer

	replay := func() {
		response.Body = &replayReadCloser{
			Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader),
			Body:   originalBody,
		}
	}

	for eventCount := 0; eventCount < responsesProbeMaxLifecycleEvents; {
		if cause := context.Cause(ctx); cause != nil {
			return &upstream.Failure{Kind: upstream.FailureBodyRead, Err: cause}
		}

		remaining := responsesProbeMaxBytes - prefix.Len()
		if remaining <= 0 {
			replay()
			return nil
		}
		frame, err := readSSEFrame(reader, remaining)
		if len(frame) > 0 {
			prefix.Write(frame)
			if errors.Is(err, errResponsesProbeFrameTooLarge) || prefix.Len() > responsesProbeMaxBytes {
				replay()
				return nil
			}

			decision, summary := classifyResponsesSSEFrame(frame)
			switch decision {
			case responsesProbeCapacity:
				return responsesCapacityFailure(summary, response, prefix.Bytes(), sanitizer)
			case responsesProbePassThrough:
				replay()
				return nil
			case responsesProbeContinue:
				eventCount++
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				replay()
				return nil
			}
			summary := sanitizeResponsesProbeText(err.Error(), sanitizer)
			return &upstream.Failure{
				StatusCode: http.StatusBadGateway,
				Kind:       upstream.FailureBodyRead,
				Summary:    summary,
				Err:        errors.New(summary),
			}
		}
	}

	replay()
	return nil
}

func readSSEFrame(reader *bufio.Reader, limit int) ([]byte, error) {
	var frame []byte
	for {
		line, err := reader.ReadSlice('\n')
		frame = append(frame, line...)
		if len(frame) > limit {
			return frame, errResponsesProbeFrameTooLarge
		}
		if isSSEBlankLine(line) {
			return frame, nil
		}
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			return frame, err
		}
	}
}

func isSSEBlankLine(line []byte) bool {
	return len(bytes.TrimSpace(line)) == 0
}

func classifyResponsesSSEFrame(frame []byte) (responsesProbeDecision, string) {
	eventType, data, hasData := parseSSEFrame(frame)
	if eventType == "" && !hasData {
		return responsesProbeIgnore, ""
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		return responsesProbePassThrough, ""
	}

	var payload map[string]any
	if hasData {
		if err := json.Unmarshal(data, &payload); err != nil {
			return responsesProbePassThrough, ""
		}
		if eventType == "" {
			eventType, _ = payload["type"].(string)
			if eventType == "" {
				if _, hasError := payload["error"]; hasError {
					eventType = "error"
				}
			}
		}
	}
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType == "" {
		return responsesProbePassThrough, ""
	}

	switch eventType {
	case "response.failed", "error":
		code, message := responseErrorFields(payload)
		if isResponsesCapacityError(code, message) {
			if message == "" {
				message = code
			}
			if message == "" {
				message = string(bytes.TrimSpace(data))
			}
			return responsesProbeCapacity, message
		}
		// Other protocol failures must remain visible to Codex. They are not
		// capacity failures and should not be retried by this layer.
		return responsesProbePassThrough, ""
	case "response.created", "response.in_progress", "response.queued", "response.metadata":
		return responsesProbeContinue, ""
	default:
		return responsesProbePassThrough, ""
	}
}

func parseSSEFrame(frame []byte) (eventType string, data []byte, hasData bool) {
	var dataLines [][]byte
	for _, rawLine := range bytes.Split(frame, []byte{'\n'}) {
		line := bytes.TrimSuffix(rawLine, []byte{'\r'})
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		field, value, found := bytes.Cut(line, []byte{':'})
		if found && len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		switch string(field) {
		case "event":
			eventType = strings.TrimSpace(string(value))
		case "data":
			dataLines = append(dataLines, value)
			hasData = true
		}
	}
	if len(dataLines) > 0 {
		data = bytes.Join(dataLines, []byte{'\n'})
	}
	return eventType, data, hasData
}

func responseErrorFields(payload map[string]any) (code, message string) {
	if response, ok := payload["response"].(map[string]any); ok {
		code, message = errorObjectFields(response["error"])
	}
	if code == "" && message == "" {
		code, message = errorObjectFields(payload["error"])
	}
	if code == "" {
		code, _ = payload["code"].(string)
	}
	if message == "" {
		message, _ = payload["message"].(string)
	}
	return strings.TrimSpace(code), strings.TrimSpace(message)
}

func errorObjectFields(value any) (code, message string) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}
	code, _ = object["code"].(string)
	message, _ = object["message"].(string)
	return code, message
}

func isResponsesCapacityError(code, message string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "server_is_overloaded", "slow_down", "model_at_capacity", "capacity_exceeded":
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(normalized, "overloaded") ||
		strings.Contains(normalized, "at capacity") ||
		(strings.Contains(normalized, "temporarily unavailable") && strings.Contains(normalized, "model"))
}

func responsesCapacityFailure(
	summary string,
	response *http.Response,
	body []byte,
	sanitizer *upstream.Sanitizer,
) *upstream.Failure {
	if summary == "" {
		summary = "Responses upstream reported that the selected model is at capacity"
	}
	failure := &upstream.Failure{
		StatusCode:   http.StatusServiceUnavailable,
		Kind:         upstream.FailureHTTP,
		UpstreamCode: "server_is_overloaded",
		Summary:      sanitizeResponsesProbeText(summary, sanitizer),
		Err:          errors.New("responses upstream model capacity error"),
	}
	if response != nil {
		failure.UpstreamResponseStatus = response.StatusCode
		failure.UpstreamResponseHeader = response.Header.Clone()
		failure.UpstreamResponseBody = append([]byte{}, body...)
		failure.UpstreamResponseComplete = true
	}
	return failure
}

func sanitizeResponsesProbeText(value string, sanitizer *upstream.Sanitizer) string {
	if sanitizer == nil {
		sanitizer = upstream.NewSanitizer()
	}
	return sanitizer.Sanitize([]byte(value))
}

type replayReadCloser struct {
	io.Reader
	Body io.ReadCloser
	once sync.Once
	err  error
}

func (body *replayReadCloser) Close() error {
	body.once.Do(func() { body.err = body.Body.Close() })
	return body.err
}

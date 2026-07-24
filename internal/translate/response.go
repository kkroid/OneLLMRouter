package translate

import "strings"

// sanitizeUTF8 replaces invalid UTF-8 sequences including lone surrogates.
func sanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "�")
}

// TranslateResponse converts an OpenAI response to Anthropic format.
func TranslateResponse(openai *OpenAIResponse, originalModel string) *AnthropicResponse {
	return CoreToAnthropicResponse(OpenAIResponseToCore(openai), originalModel)
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
	return CoreToOpenAIResponse(AnthropicResponseToCore(anthropic), originalModel)
}

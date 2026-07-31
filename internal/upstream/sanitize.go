package upstream

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxFailureSummaryBytes = 4096

var (
	bearerCredential    = regexp.MustCompile(`(?i)\bBearer[ \t]+[^\s"',;}\]]+`)
	jsonCredentialStart = regexp.MustCompile(`(?i)"(?:authorization|api_key|api-key|x-api-key)"\s*:\s*"`)
	textCredential      = regexp.MustCompile(`(?i)((?:authorization|api_key|api-key|x-api-key)\s*[:=]\s*)([^\r\n,;]+)`)
)

type Sanitizer struct {
	secretPatterns []*regexp.Regexp
	secrets        []string
}

func NewSanitizer(secrets ...string) *Sanitizer {
	patterns := make([]*regexp.Regexp, 0, len(secrets))
	nonEmptySecrets := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret != "" {
			patterns = append(patterns, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(secret)))
			nonEmptySecrets = append(nonEmptySecrets, secret)
		}
	}
	return &Sanitizer{secretPatterns: patterns, secrets: nonEmptySecrets}
}

func sanitizeWith(sanitizer *Sanitizer, input []byte) string {
	if sanitizer == nil {
		sanitizer = NewSanitizer()
	}
	return sanitizer.Sanitize(input)
}

func (s *Sanitizer) Sanitize(input []byte) string {
	truncated := len(input) > maxFailureSummaryBytes
	value := strings.ToValidUTF8(string(input), "�")
	for _, pattern := range s.secretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	value = bearerCredential.ReplaceAllString(value, "Bearer [REDACTED]")
	value = redactJSONCredentials(value)
	value = textCredential.ReplaceAllString(value, "$1[REDACTED]")
	value = removeUnsafeControls(value)
	if truncated {
		value = s.redactTrailingSecretPrefix(value)
	}
	if len(value) > maxFailureSummaryBytes {
		truncated = true
		value = truncateUTF8(value, maxFailureSummaryBytes)
	}
	if truncated {
		value += "\n...[truncated]"
	}
	return value
}

func redactJSONCredentials(value string) string {
	var redacted strings.Builder
	for {
		match := jsonCredentialStart.FindStringIndex(value)
		if match == nil {
			redacted.WriteString(value)
			return redacted.String()
		}

		redacted.WriteString(value[:match[1]])
		redacted.WriteString("[REDACTED]")
		value = value[match[1]:]

		escaped := false
		closed := false
		for index := 0; index < len(value); index++ {
			switch {
			case escaped:
				escaped = false
			case value[index] == '\\':
				escaped = true
			case value[index] == '"':
				redacted.WriteByte('"')
				value = value[index+1:]
				closed = true
			}
			if closed {
				break
			}
		}
		if !closed {
			return redacted.String()
		}
	}
}

func (s *Sanitizer) redactTrailingSecretPrefix(value string) string {
	candidate := strings.TrimRight(value, string(utf8.RuneError))
	for _, secret := range s.secrets {
		maxPrefix := len(secret) - 1
		if maxPrefix > len(candidate) {
			maxPrefix = len(candidate)
		}
		for prefixLength := maxPrefix; prefixLength > 0; prefixLength-- {
			if !utf8.ValidString(secret[:prefixLength]) {
				continue
			}
			suffix := candidate[len(candidate)-prefixLength:]
			if utf8.ValidString(suffix) && strings.EqualFold(suffix, secret[:prefixLength]) {
				return candidate[:len(candidate)-prefixLength] + "[REDACTED]"
			}
		}
	}
	return value
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}

func removeUnsafeControls(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, value)
}

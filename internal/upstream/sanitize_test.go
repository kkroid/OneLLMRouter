package upstream

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeRedactsCredentialsAcrossFormats(t *testing.T) {
	sanitizer := NewSanitizer("provider-secret")
	input := []byte(`{
  "authorization": "Bearer json-token",
  "API_KEY": "json-secret",
  "api-key": "dash-secret",
  "x-api-key": "provider-secret"
}
Authorization: Bearer header-token
X-API-Key: header-secret
api_key=query-secret
plain provider-secret`)

	result := sanitizer.Sanitize(input)

	for _, secret := range []string{
		"json-token", "json-secret", "dash-secret", "provider-secret",
		"header-token", "header-secret", "query-secret",
	} {
		if strings.Contains(result, secret) {
			t.Errorf("sanitized output contains %q: %s", secret, result)
		}
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("sanitized output has no redaction marker: %s", result)
	}
}

func TestSanitizeIsCaseInsensitiveForCredentialLabels(t *testing.T) {
	sanitizer := NewSanitizer()
	result := sanitizer.Sanitize([]byte("aUtHoRiZaTiOn: bEaReR MixedSecret\nX-aPi-KeY: OtherSecret"))

	if strings.Contains(result, "MixedSecret") || strings.Contains(result, "OtherSecret") {
		t.Fatalf("mixed-case credentials leaked: %s", result)
	}
}

func TestSanitizeRedactsConfiguredSecretCaseInsensitively(t *testing.T) {
	sanitizer := NewSanitizer("Provider-Secret")
	result := sanitizer.Sanitize([]byte("reflected pRoViDeR-sEcReT value"))

	if strings.Contains(strings.ToLower(result), "provider-secret") {
		t.Fatalf("mixed-case provider secret leaked: %s", result)
	}
}

func TestSanitizeReplacesInvalidUTF8AndRemovesControls(t *testing.T) {
	sanitizer := NewSanitizer()
	result := sanitizer.Sanitize([]byte{'o', 'k', '\t', '\n', 0xff, 0x00, 0x1b, 'x'})

	if !utf8.ValidString(result) {
		t.Fatalf("result is not valid UTF-8: %q", result)
	}
	if !strings.Contains(result, "\t\n") || !strings.ContainsRune(result, utf8.RuneError) {
		t.Fatalf("normal whitespace or replacement rune missing: %q", result)
	}
	if strings.ContainsRune(result, 0x00) || strings.ContainsRune(result, 0x1b) {
		t.Fatalf("control characters remain: %q", result)
	}
}

func TestSanitizeTruncatesAt4096BytesWithMarker(t *testing.T) {
	sanitizer := NewSanitizer()
	result := sanitizer.Sanitize([]byte(strings.Repeat("x", 4097)))

	if !strings.HasPrefix(result, strings.Repeat("x", 4096)) {
		t.Fatalf("sanitized prefix length/content is wrong: %d", len(result))
	}
	if !strings.Contains(result, "[truncated]") {
		t.Fatalf("truncation marker missing: %q", result[len(result)-32:])
	}
}

func TestSanitizeDoesNotMarkExactBoundaryAsTruncated(t *testing.T) {
	sanitizer := NewSanitizer()
	result := sanitizer.Sanitize([]byte(strings.Repeat("x", 4096)))

	if strings.Contains(result, "[truncated]") {
		t.Fatal("exact 4096-byte input was marked truncated")
	}
}

func TestSanitizeRedactsSecretBeforeTruncationBoundary(t *testing.T) {
	sanitizer := NewSanitizer("Mixed-Boundary-Secret")
	input := strings.Repeat("x", 4090) + "mIxEd-BoUnDaRy-SeCrEt" + "tail"
	result := sanitizer.Sanitize([]byte(input))

	if strings.Contains(strings.ToLower(result), "mixed-") {
		t.Fatalf("provider secret prefix leaked at truncation boundary: %q", result[len(result)-64:])
	}
	if !strings.Contains(result, "[truncated]") {
		t.Fatalf("truncation marker missing: %q", result[len(result)-64:])
	}
}

func TestSanitizeRedactsEscapedJSONCredentialValue(t *testing.T) {
	result := NewSanitizer().Sanitize([]byte(`{"api_key":"secret\"tail","message":"denied"}`))

	if strings.Contains(result, "secret") || strings.Contains(result, "tail") {
		t.Fatalf("escaped JSON credential leaked: %q", result)
	}
}

func TestSanitizeRedactsUnclosedJSONCredentialValue(t *testing.T) {
	result := NewSanitizer().Sanitize([]byte(`{"api_key":"secret-tail`))

	if strings.Contains(result, "secret-tail") {
		t.Fatalf("unclosed JSON credential leaked: %q", result)
	}
}

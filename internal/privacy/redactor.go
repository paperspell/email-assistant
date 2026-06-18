package privacy

import (
	"regexp"
	"sync"
)

var (
	once     sync.Once
	patterns []struct {
		re          *regexp.Regexp
		placeholder string
	}
)

func initPatterns() {
	patterns = []struct {
		re          *regexp.Regexp
		placeholder string
	}{
		// Email — apply first; contains @ so won't conflict with others.
		{regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), "[EMAIL]"},
		// IBAN — letter-prefixed; apply before phone to avoid digits being matched first.
		// Handles optional spaces between groups (e.g. "DE89 3704 0044 ...").
		{regexp.MustCompile(`[A-Z]{2}\d{2}(?:\s*[A-Z0-9]{4}){4,7}(?:\s*[A-Z0-9]{1,4})?`), "[FINANCIAL]"},
		// Credit card — four groups of four digits; apply before phone.
		{regexp.MustCompile(`\b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b`), "[FINANCIAL]"},
		// UUID — hex with dashes; apply before long-token so dashes aren't mismatched.
		{regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), "[TOKEN]"},
		// Long alphanumeric token ≥ 20 chars (API keys, session IDs, etc.).
		{regexp.MustCompile(`[A-Za-z0-9_\-]{20,}`), "[TOKEN]"},
		// OTP / verification code in context.
		{regexp.MustCompile(`(?i)(code|otp|token|pin|passcode)[^0-9]{0,10}(\d{4,8})`), "[TOKEN]"},
		// Street address — apply before phone so leading digits aren't consumed first.
		{regexp.MustCompile(
			`\b\d{1,5}\s+[A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,3}\s+` +
				`(St|Street|Ave|Avenue|Rd|Road|Blvd|Dr|Lane|Ln|ul\.|al\.)\b`,
		), "[ADDRESS]"},
		// Phone — most general numeric pattern; apply last.
		{regexp.MustCompile(`(\+?[\d][\d\s\-(). ]{6,14}[\d])`), "[PHONE]"},
	}
}

// Redact replaces recognised sensitive patterns in text with placeholder tags.
func Redact(text string) string {
	once.Do(initPatterns)
	for _, p := range patterns {
		text = p.re.ReplaceAllString(text, p.placeholder)
	}
	return text
}

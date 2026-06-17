package privacy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedact_Email(t *testing.T) {
	got := Redact("Contact john@company.com for details.")
	assert.NotContains(t, got, "john@company.com")
	assert.Contains(t, got, "[EMAIL]")
}

func TestRedact_Phone(t *testing.T) {
	got := Redact("Call us at +1 (555) 123-4567 anytime.")
	assert.NotContains(t, got, "555")
	assert.Contains(t, got, "[PHONE]")
}

func TestRedact_IBAN(t *testing.T) {
	got := Redact("Transfer to DE89 3704 0044 0532 0130 00 please.")
	assert.NotContains(t, got, "DE89")
	assert.Contains(t, got, "[FINANCIAL]")
}

func TestRedact_CreditCard(t *testing.T) {
	got := Redact("Card number: 4111 1111 1111 1111")
	assert.NotContains(t, got, "4111")
	assert.Contains(t, got, "[FINANCIAL]")
}

func TestRedact_UUID(t *testing.T) {
	got := Redact("Your token is 550e8400-e29b-41d4-a716-446655440000.")
	assert.NotContains(t, got, "550e8400")
	assert.Contains(t, got, "[TOKEN]")
}

func TestRedact_LongToken(t *testing.T) {
	got := Redact("API key: abcdefghijklmnopqrstu")
	assert.NotContains(t, got, "abcdefghijklmnopqrstu")
	assert.Contains(t, got, "[TOKEN]")
}

func TestRedact_OTP(t *testing.T) {
	got := Redact("Your verification code: 123456")
	assert.Contains(t, got, "[TOKEN]")
	assert.NotContains(t, got, "123456")
}

func TestRedact_Address(t *testing.T) {
	got := Redact("We are located at 123 Main St.")
	assert.Contains(t, got, "[ADDRESS]")
	assert.NotContains(t, got, "123 Main St")
}

func TestRedact_NonSensitiveUnchanged(t *testing.T) {
	input := "Hello, please review the attached report."
	got := Redact(input)
	assert.Equal(t, input, got)
}

func TestRedact_MultiplePatterns(t *testing.T) {
	input := "Email john@example.com or call +48 600 123 456"
	got := Redact(input)
	assert.NotContains(t, got, "john@example.com")
	assert.Contains(t, got, "[EMAIL]")
	assert.Contains(t, got, "[PHONE]")
}

func TestRedact_EmptyString(t *testing.T) {
	assert.Equal(t, "", Redact(""))
}

func TestRedact_NoPlaceholderInPlainText(t *testing.T) {
	input := "The meeting is scheduled for tomorrow at 3pm."
	got := Redact(input)
	assert.False(t, strings.Contains(got, "[EMAIL]") ||
		strings.Contains(got, "[PHONE]") ||
		strings.Contains(got, "[FINANCIAL]") ||
		strings.Contains(got, "[TOKEN]") ||
		strings.Contains(got, "[ADDRESS]"),
	)
}

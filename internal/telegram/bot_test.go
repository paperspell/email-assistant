package telegram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/domain"
)

var testClassification = domain.Classification{
	Level:    domain.LevelImportant,
	Category: domain.CategoryWork,
	Score:    75,
	Reason: []string{
		"baseline: +40", "urgent keyword in subject: +25",
		"meeting keyword in subject: +20", "unknown sender: -10",
	},
}

func TestFormatMessage_WithName(t *testing.T) {
	e := domain.Email{
		FromName:  "Alice Smith",
		FromEmail: "alice@example.com",
		Subject:   "Project update",
		Date:      time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
	}
	msg := formatMessage(e, testClassification, "Work")
	// Angle brackets around the address are HTML-escaped for Telegram's parser.
	assert.Contains(t, msg, "Alice Smith &lt;alice@example.com&gt;")
	assert.Contains(t, msg, "Project update")
	assert.Contains(t, msg, "New email")
	assert.Contains(t, msg, "important")
	assert.Contains(t, msg, "75")
	// Importance is emphasised in bold with an icon for the level.
	assert.Contains(t, msg, "<b>🟠 Importance: important (score 75)</b>")
	// The source account is labelled.
	assert.Contains(t, msg, "Account: Work")
}

func TestFormatMessage_OmitsAccountWhenEmpty(t *testing.T) {
	e := domain.Email{FromEmail: "x@y.com", Date: time.Now()}
	msg := formatMessage(e, testClassification, "")
	assert.NotContains(t, msg, "Account:")
}

func TestFormatMessage_WithoutName(t *testing.T) {
	e := domain.Email{
		FromEmail: "bob@example.com",
		Subject:   "Hello",
		Date:      time.Now(),
	}
	msg := formatMessage(e, testClassification, "")
	assert.Contains(t, msg, "bob@example.com")
	// A bare address is not wrapped in angle brackets (escaped or otherwise).
	assert.NotContains(t, msg, "&lt;")
}

func TestFormatMessage_EmptySubject(t *testing.T) {
	e := domain.Email{
		FromEmail: "x@y.com",
		Date:      time.Now(),
	}
	assert.NotPanics(t, func() { formatMessage(e, testClassification, "") })
}

func TestFormatMessage_ShowsSummaryWhenPresent(t *testing.T) {
	e := domain.Email{FromEmail: "x@y.com", Date: time.Now()}
	c := domain.Classification{
		Level:   domain.LevelImportant,
		Score:   80,
		Summary: "Quarterly budget review needs approval.",
	}
	msg := formatMessage(e, c, "")
	assert.Contains(t, msg, "Summary: Quarterly budget review needs approval.")
	assert.NotContains(t, msg, "Why:")
}

func TestFormatMessage_ShowsReasonsWhenNoSummary(t *testing.T) {
	e := domain.Email{FromEmail: "x@y.com", Date: time.Now()}
	msg := formatMessage(e, testClassification, "")
	assert.Contains(t, msg, "Why:")
	assert.Contains(t, msg, "baseline: +40")
}

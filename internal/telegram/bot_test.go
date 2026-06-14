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
	msg := formatMessage(e, testClassification)
	assert.Contains(t, msg, "Alice Smith <alice@example.com>")
	assert.Contains(t, msg, "Project update")
	assert.Contains(t, msg, "New email")
	assert.Contains(t, msg, "important")
	assert.Contains(t, msg, "75")
}

func TestFormatMessage_WithoutName(t *testing.T) {
	e := domain.Email{
		FromEmail: "bob@example.com",
		Subject:   "Hello",
		Date:      time.Now(),
	}
	msg := formatMessage(e, testClassification)
	assert.Contains(t, msg, "bob@example.com")
	assert.NotContains(t, msg, "<")
}

func TestFormatMessage_EmptySubject(t *testing.T) {
	e := domain.Email{
		FromEmail: "x@y.com",
		Date:      time.Now(),
	}
	assert.NotPanics(t, func() { formatMessage(e, testClassification) })
}

func TestFormatMessage_ContainsReasons(t *testing.T) {
	e := domain.Email{
		FromEmail: "x@y.com",
		Date:      time.Now(),
	}
	msg := formatMessage(e, testClassification)
	assert.Contains(t, msg, "baseline: +40")
	assert.Contains(t, msg, "urgent keyword")
}

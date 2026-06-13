package telegram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/domain"
)

func TestFormatMessage_WithName(t *testing.T) {
	e := domain.Email{
		FromName:  "Alice Smith",
		FromEmail: "alice@example.com",
		Subject:   "Project update",
		Date:      time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
	}
	msg := formatMessage(e)
	assert.Contains(t, msg, "Alice Smith <alice@example.com>")
	assert.Contains(t, msg, "Project update")
	assert.Contains(t, msg, "New email")
}

func TestFormatMessage_WithoutName(t *testing.T) {
	e := domain.Email{
		FromName:  "",
		FromEmail: "bob@example.com",
		Subject:   "Hello",
		Date:      time.Now(),
	}
	msg := formatMessage(e)
	assert.Contains(t, msg, "bob@example.com")
	assert.NotContains(t, msg, "<")
}

func TestFormatMessage_EmptySubject(t *testing.T) {
	e := domain.Email{
		FromEmail: "x@y.com",
		Subject:   "",
		Date:      time.Now(),
	}
	assert.NotPanics(t, func() { formatMessage(e) })
}

package telegram

import (
	"context"

	"github.com/paperspell/email-assistant/internal/domain"
)

// Notifier sends email notifications to the user.
type Notifier interface {
	SendNewEmail(ctx context.Context, e domain.Email) error
}

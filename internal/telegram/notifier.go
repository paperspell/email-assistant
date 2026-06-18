package telegram

import (
	"context"

	"github.com/paperspell/email-assistant/internal/domain"
)

// Notifier sends email notifications to the user.
type Notifier interface {
	// accountName and accountEmail label which configured account the email
	// arrived on, so accounts are distinguishable even when they share a name.
	// Both may be empty for single-account setups.
	SendNewEmail(
		ctx context.Context, e domain.Email, c domain.Classification, accountName, accountEmail string,
	) (int64, error)
}

package domain

import "time"

// Pending action kinds — the kind of free-text or confirmation the bot awaits.
const (
	// PendingClause: the next message is the text of a new ignore clause.
	PendingClause = "clause"
	// PendingSubjectEdit: the next message is an edited subject pattern.
	PendingSubjectEdit = "subject_edit"
	// PendingSubjectConfirm: awaiting Use/Edit/Cancel; Payload holds the suggestion.
	PendingSubjectConfirm = "subject_confirm"
)

// PendingAction records an in-progress multi-step Telegram interaction for a chat.
// There is at most one per chat (keyed by ChatID); a new choice overwrites it.
type PendingAction struct {
	ChatID    int64
	Kind      string
	EmailID   string
	AccountID string
	Payload   string // suggested subject pattern (PendingSubjectConfirm)
	CreatedAt time.Time
}

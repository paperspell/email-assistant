package domain

import "time"

// SyncState tracks the last processed message UID for an account.
type SyncState struct {
	AccountID string
	LastUID   uint32
	SyncedAt  time.Time
}

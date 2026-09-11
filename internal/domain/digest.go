package domain

import "time"

// Digest is a sent daily digest for one account, used to map a Telegram reply
// (`/important …`) and the bulk buttons back to the right emails.
type Digest struct {
	ID        string
	AccountID string
	Date      string // YYYY-MM-DD in the account's timezone
	// TGMessageID is the Telegram message carrying the digest's buttons — the
	// last part when the digest was split. Its keyboard is what gets removed
	// once the buttons have been used.
	TGMessageID int64
	// TGMessageIDs lists every part in sending order, so a reply to any of them
	// resolves to this digest. A single-message digest has exactly one entry,
	// equal to TGMessageID.
	TGMessageIDs []int64
	SentAt       time.Time
}

// DigestItem is one numbered, LLM-judged-unimportant email in a digest.
type DigestItem struct {
	DigestID string
	SeqNo    int // 1-based number shown to the user
	EmailID  string
	Promoted bool
}

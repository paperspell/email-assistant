package domain

import "time"

// Digest is a sent daily digest for one account, used to map a Telegram reply
// (`/important …`) and the bulk buttons back to the right emails.
type Digest struct {
	ID          string
	AccountID   string
	Date        string // YYYY-MM-DD in the account's timezone
	TGMessageID int64
	SentAt      time.Time
}

// DigestItem is one numbered, LLM-judged-unimportant email in a digest.
type DigestItem struct {
	DigestID string
	SeqNo    int // 1-based number shown to the user
	EmailID  string
	Promoted bool
}

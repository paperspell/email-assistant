package domain

import "time"

// EmailStatus represents the lifecycle state of an email.
type EmailStatus string

// Email status constants represent the processing lifecycle of a message.
const (
	StatusNew         EmailStatus = "new"
	StatusNotified    EmailStatus = "notified"
	StatusIgnored     EmailStatus = "ignored"
	StatusHandled     EmailStatus = "handled"
	StatusReplyNeeded EmailStatus = "reply_needed"
)

// Email represents a received email message stored locally.
type Email struct {
	ID                string
	AccountID         string
	MessageUID        uint32
	Subject           string
	FromEmail         string
	FromName          string
	Date              time.Time
	Status            EmailStatus
	ReceivedAt        time.Time
	Language          string
	TelegramMessageID int64 // 0 until a notification is sent

	// DecidedBy records what filtered an ignored email: "rule:<id>" (Tier-0
	// rule), "baseline" (score gate), or "llm:low" (LLM judged it unimportant).
	// Empty when the email was notified or forced important by an allow rule.
	DecidedBy string
}

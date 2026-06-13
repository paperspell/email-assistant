package domain

import "time"

// EmailStatus represents the lifecycle state of an email.
type EmailStatus string

// Email status constants represent the processing lifecycle of a message.
const (
	StatusNew      EmailStatus = "new"
	StatusNotified EmailStatus = "notified"
	StatusIgnored  EmailStatus = "ignored"
)

// Email represents a received email message stored locally.
type Email struct {
	ID         string
	AccountID  string
	MessageUID uint32
	Subject    string
	FromEmail  string
	FromName   string
	Date       time.Time
	Status     EmailStatus
	ReceivedAt time.Time
}

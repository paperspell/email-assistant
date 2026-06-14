package email

import (
	"context"
	"time"
)

// Message contains the metadata of a single email fetched from the provider.
type Message struct {
	UID       uint32
	Subject   string
	FromEmail string
	FromName  string
	Date      time.Time
	// Extra header fields used by the importance filter
	InReplyTo       string // set when this is a reply in an active thread
	ListUnsubscribe string // set on newsletters
	Precedence      string // "bulk" or "list" on bulk mail
}

// Provider is the interface that all email backend implementations must satisfy.
type Provider interface {
	// Connect establishes and authenticates a connection to the mail server.
	Connect(ctx context.Context) error
	// FetchSince returns messages with UID strictly greater than lastUID.
	FetchSince(ctx context.Context, lastUID uint32) ([]Message, error)
	// Close closes the underlying connection.
	Close() error
}

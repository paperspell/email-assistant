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
	// Body contains the plain-text body; empty when not fetched (FetchBody=false).
	Body string
}

// Provider is the interface that all email backend implementations must satisfy.
type Provider interface {
	// Connect establishes and authenticates a connection to the mail server.
	Connect(ctx context.Context) error
	// FetchSince returns messages with UID strictly greater than lastUID.
	FetchSince(ctx context.Context, lastUID uint32) ([]Message, error)
	// MarkRead marks the message with the given UID as read (seen) in the
	// mailbox. It is best-effort with respect to connection state: callers
	// should treat an error as non-fatal.
	MarkRead(ctx context.Context, uid uint32) error
	// FetchBody returns the plain-text body of the message with the given UID,
	// fetched on demand without changing its read state. Returns an empty string
	// when the message has no text body.
	FetchBody(ctx context.Context, uid uint32) (string, error)
	// Close closes the underlying connection.
	Close() error
}

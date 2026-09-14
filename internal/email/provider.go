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
	// To and Cc hold the recipient addresses, lowercased. They tell a message
	// addressed to the mailbox owner apart from one where the owner is merely
	// copied or reached through a list — the difference focus mode turns on.
	To   []string
	Cc   []string
	Date time.Time
	// Extra header fields used by the importance filter
	InReplyTo       string // set when this is a reply in an active thread
	ListUnsubscribe string // set on newsletters
	Precedence      string // "bulk" or "list" on bulk mail
	ListID          string // List-Id header; used by list_id filter rules
	// Body contains the plain-text body; empty when not fetched (FetchBody=false).
	Body string
	// Notification describes the machine-generated notification this message
	// is, when it is one — the tool that sent it and why it reached the owner.
	Notification Notification
}

// Notification is what a ticket, code-review or document tool says about why
// it sent a message. Tools put this in headers the classifier never saw, and
// it is the difference between "a person asked for my review" and "a commit
// was pushed to a merge request I am subscribed to".
type Notification struct {
	// Platform is "github", "gitlab", or "" for anything else.
	Platform string
	// Reason is why the owner received it. GitHub states it outright in
	// X-GitHub-Reason (mention, review_requested, assign, subscribed, …).
	// GitLab does not: a message with a discussion id is a "comment", one
	// without is "activity" — a push, approval, merge or resolved thread,
	// which is never a person addressing the owner.
	Reason string
	// Sender is the acting user's handle, e.g. "aicode", when the tool names
	// one. Automated reviewers are users too, and this is how they show.
	Sender string
	// Automated is set for any message marked Auto-Submitted: auto-generated,
	// whether or not the tool is recognised.
	Automated bool
}

// Provider is the interface that all email backend implementations must satisfy.
type Provider interface {
	// Connect establishes and authenticates a connection to the mail server.
	Connect(ctx context.Context) error
	// FetchSince returns messages with UID strictly greater than lastUID.
	FetchSince(ctx context.Context, lastUID uint32) ([]Message, error)
	// LatestUID returns the current highest UID boundary in the mailbox without
	// fetching any messages, used to set the first-run baseline cheaply. Returns 0
	// for an empty mailbox.
	LatestUID(ctx context.Context) (uint32, error)
	// FetchUnseenSince returns unread messages received on or after `since`, capped
	// to the newest `limit` (0 = no cap). Read state is not changed. Used for the
	// first-run backfill.
	FetchUnseenSince(ctx context.Context, since time.Time, limit int) ([]Message, error)
	// MarkRead marks the message with the given UID as read (seen) in the
	// mailbox. It is best-effort with respect to connection state: callers
	// should treat an error as non-fatal.
	MarkRead(ctx context.Context, uid uint32) error
	// FetchBody returns the plain-text body of the message with the given UID,
	// fetched on demand without changing its read state. Returns an empty string
	// when the message has no text body.
	FetchBody(ctx context.Context, uid uint32) (string, error)
	// MoveToTrash moves the message with the given UID to the mailbox Trash
	// (recoverable). It falls back to \Deleted + expunge when no Trash folder is
	// available. Best-effort with respect to connection state.
	MoveToTrash(ctx context.Context, uid uint32) error
	// Close closes the underlying connection.
	Close() error
}

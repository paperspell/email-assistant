package domain

import "time"

// Authentication methods for an account.
const (
	// AuthPassword is the default: a static IMAP password.
	AuthPassword = "password"
	// AuthOAuth uses OAuth tokens (Gmail/Graph) instead of a password.
	AuthOAuth = "oauth"
)

// Account represents a configured email account.
type Account struct {
	ID           string // stable identity = Email; used as account_id elsewhere
	Name         string
	Email        string
	Host         string
	Port         int
	Username     string
	Password     string
	TLS          bool
	PollInterval time.Duration
	AuthType     string // "password" or "oauth"
	Enabled      bool
	// DigestTime overrides the global digest.time for this account ("HH:MM").
	// Empty means use the global setting.
	DigestTime string

	// BackfillWindow controls the first-run backfill: on the very first poll,
	// unread mail received within this window is processed (important → notify,
	// unimportant → digest). 0 disables it (the first run stays silent).
	BackfillWindow time.Duration

	// Focus narrows what the owner wants to hear about from this mailbox, in
	// their own words — e.g. "only mail addressed to me directly, or a ticket or
	// document where someone mentions me". Empty means everything is judged on
	// its own merits, as before. Mail outside the focus is classified as ignore.
	Focus string
	// Aliases are the names and handles the owner is addressed by — a first
	// name, a Jira handle, an @mention — so the classifier can recognise a
	// mention of the owner inside a notification that is not addressed to them.
	Aliases []string
	// DigestEnabled controls whether this account sends a daily digest. In a
	// focused mailbox the digest would list exactly the mail the owner asked
	// not to see, so it is usually turned off there.
	DigestEnabled bool

	// OAuth credentials, populated when AuthType == AuthOAuth. The refresh token
	// is the durable secret; the access token and expiry are a refreshable cache.
	OAuthRefreshToken string
	OAuthAccessToken  string
	OAuthTokenExpiry  time.Time
}

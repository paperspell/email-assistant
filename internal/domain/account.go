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

	// OAuth credentials, populated when AuthType == AuthOAuth. The refresh token
	// is the durable secret; the access token and expiry are a refreshable cache.
	OAuthRefreshToken string
	OAuthAccessToken  string
	OAuthTokenExpiry  time.Time
}

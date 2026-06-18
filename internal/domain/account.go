package domain

import "time"

// AuthPassword is the default authentication method: a static IMAP password.
// Future backends (Gmail, Microsoft Graph) will add "oauth".
const AuthPassword = "password"

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
	AuthType     string // "password" for now; "oauth" reserved for Gmail/Graph
	Enabled      bool
}

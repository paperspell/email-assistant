package oauth

import (
	"fmt"

	"github.com/emersion/go-sasl"
)

// xoauth2 is the SASL mechanism name Gmail uses for OAuth IMAP login.
const xoauth2 = "XOAUTH2"

// xoauth2Client implements sasl.Client for the XOAUTH2 mechanism. go-sasl ships
// only OAUTHBEARER, so this small client provides the format Gmail documents.
type xoauth2Client struct {
	username string
	token    string
}

// NewXOAUTH2 returns a sasl.Client that authenticates as username with the given
// OAuth access token.
func NewXOAUTH2(username, token string) sasl.Client {
	return &xoauth2Client{username: username, token: token}
}

func (c *xoauth2Client) Start() (mech string, ir []byte, err error) {
	// Format: "user=" {email} ^A "auth=Bearer " {token} ^A ^A
	ir = []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01")
	return xoauth2, ir, nil
}

func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	// On success the server does not challenge. A challenge here is the
	// base64-encoded JSON error Gmail returns on a bad token; abort.
	return nil, fmt.Errorf("xoauth2: authentication failed: %s", challenge)
}

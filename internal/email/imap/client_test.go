package imap

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/paperspell/email-assistant/internal/pkg/log"
)

type failingTokenSource struct{}

func (failingTokenSource) Token() (*oauth2.Token, error) {
	return nil, errors.New("token refresh failed")
}

// When a token source is configured, authenticate takes the OAuth path and
// consults the token source before touching the connection. A token error
// surfaces without dereferencing the client.
func TestAuthenticate_OAuthPathConsultsTokenSource(t *testing.T) {
	c := &Client{cfg: Config{
		Username:    "user@gmail.com",
		TokenSource: failingTokenSource{},
		Logger:      log.Noop{},
	}}

	err := c.authenticate(nil) // nil client is safe: the token error returns first
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oauth token")
}

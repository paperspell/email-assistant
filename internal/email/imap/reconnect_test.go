package imap

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"

	"github.com/paperspell/email-assistant/internal/auth/oauth"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

// connMarker is a non-nil *imapclient.Client used only to mark a Client as
// "connected". The reconnect seam is stubbed in these tests, so its methods are
// never called.
var connMarker = &imapclient.Client{}

func TestIsConnErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"net closed", net.ErrClosed, true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"wrapped eof", fmt.Errorf("imap fetch: %w", io.EOF), true},
		{"closed network conn string", errors.New("write tcp: use of closed network connection"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"connection closed", errors.New("imap: connection closed"), true},
		{"server command error", errors.New("imap: NO mailbox does not exist"), false},
		{"plain error", errors.New("something else"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isConnErr(tc.err))
		})
	}
}

// newExecClient returns a client whose reconnect is stubbed to count calls and
// mark itself "connected" (non-nil client) without touching the network.
func newExecClient(reconnectErr error) (*Client, *int) {
	c := &Client{cfg: Config{Logger: log.Noop{}}}
	calls := 0
	c.reconnectFn = func() error {
		calls++
		if reconnectErr != nil {
			return reconnectErr
		}
		c.client = connMarker // non-nil marker; ops in these tests never touch it
		return nil
	}
	return c, &calls
}

func TestExec_SucceedsFirstTry(t *testing.T) {
	c, reconnects := newExecClient(nil)
	c.client = nil // force initial connect

	opCalls := 0
	err := c.exec(func() error { opCalls++; return nil })

	assert.NoError(t, err)
	assert.Equal(t, 1, opCalls)
	assert.Equal(t, 1, *reconnects, "one initial connect because client was nil")
}

func TestExec_NoReconnectWhenAlreadyConnected(t *testing.T) {
	c, reconnects := newExecClient(nil)
	c.client = connMarker // already connected

	opCalls := 0
	err := c.exec(func() error { opCalls++; return nil })

	assert.NoError(t, err)
	assert.Equal(t, 1, opCalls)
	assert.Equal(t, 0, *reconnects)
}

func TestExec_ReconnectsAndRetriesOnConnErr(t *testing.T) {
	c, reconnects := newExecClient(nil)
	c.client = connMarker

	opCalls := 0
	err := c.exec(func() error {
		opCalls++
		if opCalls == 1 {
			return io.EOF // connection dropped on first attempt
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, opCalls, "op retried once after reconnect")
	assert.Equal(t, 1, *reconnects)
}

func TestExec_DoesNotReconnectOnCommandError(t *testing.T) {
	c, reconnects := newExecClient(nil)
	c.client = connMarker

	cmdErr := errors.New("imap: NO permission denied")
	opCalls := 0
	err := c.exec(func() error { opCalls++; return cmdErr })

	assert.ErrorIs(t, err, cmdErr)
	assert.Equal(t, 1, opCalls, "server-side errors are not retried")
	assert.Equal(t, 0, *reconnects)
}

func TestExec_SurfacesReauthErrorFromReconnect(t *testing.T) {
	reauth := fmt.Errorf("imap oauth: refresh token rejected: %w",
		&oauth2.RetrieveError{ErrorCode: "invalid_grant"})
	c, reconnects := newExecClient(reauth)
	c.client = connMarker

	opCalls := 0
	err := c.exec(func() error { opCalls++; return io.EOF })

	assert.Equal(t, 1, opCalls, "op not retried when reconnect fails")
	assert.Equal(t, 1, *reconnects)
	assert.True(t, oauth.IsReauthRequired(err), "re-auth error propagates for the scheduler alert")
}

package imap

import (
	"errors"
	"testing"
	"unicode/utf8"

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

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text", "hello world", "hello world"},
		{"tags removed", "<p>hello <b>world</b></p>", "hello world"},
		{"whitespace collapsed", "a\n\n  b\t c", "a b c"},
		{"script content kept as text", "<div>keep me</div>", "keep me"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripHTML(tt.in))
		})
	}
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10), "shorter than limit is unchanged")
	assert.Equal(t, "hel", truncate("hello", 3), "longer than limit is cut")
	assert.Equal(t, "hello", truncate("hello", 5), "equal to limit is unchanged")
}

func TestTruncate_UTF8Boundary(t *testing.T) {
	// "é" is two bytes; cutting at 1 byte must back off to a valid boundary.
	s := "é"
	got := truncate(s, 1)
	assert.True(t, utf8.ValidString(got), "result must be valid UTF-8")
	assert.Equal(t, "", got)
}

func TestParseHeaderBytes(t *testing.T) {
	data := []byte("In-Reply-To: <abc@x.com>\r\nList-Unsubscribe: <mailto:u@x.com>\r\nPrecedence: bulk\r\n")
	h := parseHeaderBytes(data)
	assert.Equal(t, "<abc@x.com>", h.Get("In-Reply-To"))
	assert.Equal(t, "<mailto:u@x.com>", h.Get("List-Unsubscribe"))
	assert.Equal(t, "bulk", h.Get("Precedence"))
}

func TestParseHeaderBytes_Empty(t *testing.T) {
	h := parseHeaderBytes(nil)
	assert.Empty(t, h.Get("In-Reply-To"))
}

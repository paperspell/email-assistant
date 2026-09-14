package imap

import (
	"errors"
	"net/textproto"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/paperspell/email-assistant/internal/email"
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

// stripHTML now returns raw text with block breaks intact; collapsing runs of
// whitespace is normalizeText's job, so the two can be composed by extractText.
func TestStripHTML(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text", "hello world", "hello world"},
		{"inline tags removed", "<b>hello</b> world", "hello world"},
		{"block ends become newlines", "<p>a</p><p>b</p>", "a\nb\n"},
		{"br becomes a newline", "a<br/>b", "a\nb"},
		{"empty", "", ""},

		// The bug this guards: a <style> block is markup, not letter. Emitting it
		// showed the user a wall of CSS and fed the same to the model.
		{"style content dropped", "<style>p{color:red}</style><p>keep me</p>", "keep me\n"},
		{"script content dropped", "<script>var x=1;</script><p>keep me</p>", "keep me\n"},
		{"head content dropped", "<head><title>t</title></head><body>keep me</body>", "keep me"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripHTML(tt.in))
		})
	}
}

func TestNormalizeText_CollapsesWhitespace(t *testing.T) {
	assert.Equal(t, "a b c", normalizeText("a\t\tb   c"))
	assert.Equal(t, "a\n\nb", normalizeText("a\n\n\n\n\nb"))
	assert.Equal(t, "", normalizeText("   \n\n  "))
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

// Header sets copied from real notifications in a work mailbox, so the parser
// is tested against what the tools actually send rather than their docs —
// this GitLab, for one, sends no X-GitLab-NotificationReason at all.
func TestParseNotification_RealHeaders(t *testing.T) {
	hdr := func(pairs ...string) textproto.MIMEHeader {
		h := textproto.MIMEHeader{}
		for i := 0; i+1 < len(pairs); i += 2 {
			h.Set(pairs[i], pairs[i+1])
		}
		return h
	}
	tests := []struct {
		name     string
		h        textproto.MIMEHeader
		fromName string
		want     email.Notification
	}{
		{
			name: "gitlab comment by the automated reviewer",
			h: hdr("Auto-Submitted", "auto-generated", "X-GitLab-Project", "gam-cli",
				"X-GitLab-Discussion-ID", "1f6a0cb62478e8e1db57fc9b685c7f331ac72e64"),
			fromName: "The Commenter (@aicode)",
			want:     email.Notification{Platform: "gitlab", Reason: "comment", Sender: "aicode", Automated: true},
		},
		{
			name:     "gitlab push has no discussion, so it is activity",
			h:        hdr("Auto-Submitted", "auto-generated", "X-GitLab-Project", "rssp"),
			fromName: "Eliyahu Shvalb (@eliyahu.shvalb)",
			want:     email.Notification{Platform: "gitlab", Reason: "activity", Sender: "eliyahu.shvalb", Automated: true},
		},
		{
			name: "github review request",
			h: hdr("Precedence", "list", "X-GitHub-Reason", "review_requested",
				"X-GitHub-Sender", "eliyahu-shvalb_rakuten"),
			fromName: "Shvalb, Eliyahu",
			want: email.Notification{
				Platform: "github", Reason: "review_requested", Sender: "eliyahu-shvalb_rakuten", Automated: true},
		},
		{
			name:     "confluence carries no reason header",
			h:        hdr("X-Atlassian-Mail-Message-Id", "<x@rakuten-viber.atlassian.net>"),
			fromName: "Confluence",
			want:     email.Notification{},
		},
		{
			name:     "a person's mail is not a notification",
			h:        hdr("In-Reply-To", "<abc@example.com>"),
			fromName: "Alice",
			want:     email.Notification{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseNotification(tt.h, tt.fromName))
		})
	}
}

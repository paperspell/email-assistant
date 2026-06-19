package imap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2/imapclient"
	"golang.org/x/net/html"

	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/pkg/log"

	imaplib "github.com/emersion/go-imap/v2"
)

const maxBodyLen = 3000

// extraHeaders lists the additional header fields fetched alongside ENVELOPE.
var extraHeaders = []string{"In-Reply-To", "List-Unsubscribe", "Precedence"}

// Peek avoids setting the \Seen flag; a BODY[HEADER.FIELDS] fetch without it
// implicitly marks the message as read (RFC 3501 §6.4.5).
var extraHeaderSection = &imaplib.FetchItemBodySection{
	Specifier:    imaplib.PartSpecifierHeader,
	HeaderFields: extraHeaders,
	Peek:         true,
}

// peekBodySection fetches the plain-text body without setting the \Seen flag.
// Peek is ignored by the server when echoing the section spec back, so
// FindBodySection matches it correctly regardless of the Peek field.
var peekBodySection = &imaplib.FetchItemBodySection{
	Specifier: imaplib.PartSpecifierText,
	Peek:      true,
}

// Config holds connection parameters for an IMAP server.
type Config struct {
	Host      string
	Port      int
	Username  string
	Password  string
	TLS       bool
	FetchBody bool       // fetch plain-text body when true (content.mode = full_body)
	Logger    log.Logger // nil → no debug output
}

// Client implements email.Provider using IMAP.
type Client struct {
	cfg    Config
	client *imapclient.Client
	// mu serializes IMAP commands over the single connection, which is shared
	// between the polling goroutine (FetchSince) and on-demand mailbox actions
	// (MarkRead, FetchBody) triggered from Telegram callbacks.
	mu sync.Mutex
}

// NewClient creates an IMAP Client. Call Connect before using.
func NewClient(cfg Config) *Client {
	if cfg.Logger == nil {
		cfg.Logger = log.Noop{}
	}
	return &Client{cfg: cfg}
}

// Connect dials the IMAP server, authenticates, and selects INBOX.
func (c *Client) Connect(_ context.Context) error {
	addr := net.JoinHostPort(c.cfg.Host, fmt.Sprintf("%d", c.cfg.Port))

	var (
		cl  *imapclient.Client
		err error
	)

	if c.cfg.TLS {
		cl, err = imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{ServerName: c.cfg.Host}, //nolint:gosec
		})
	} else {
		cl, err = imapclient.DialInsecure(addr, nil)
	}
	if err != nil {
		return fmt.Errorf("imap dial %s: %w", addr, err)
	}

	if err := cl.Login(c.cfg.Username, c.cfg.Password).Wait(); err != nil {
		cl.Close() //nolint:errcheck
		return fmt.Errorf("imap login: %w", err)
	}

	if _, err := cl.Select("INBOX", nil).Wait(); err != nil {
		cl.Close() //nolint:errcheck
		return fmt.Errorf("imap select INBOX: %w", err)
	}

	c.client = cl
	return nil
}

// FetchSince returns messages with UID greater than lastUID.
// Body is fetched in a separate IMAP command when FetchBody is true to avoid
// a Zoho server bug where combining BODY[HEADER.FIELDS] and BODY[TEXT] in a
// single FETCH produces a malformed section-spec in the response.
func (c *Client) FetchSince(_ context.Context, lastUID uint32) ([]email.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, fmt.Errorf("imap: not connected")
	}

	startUID := imaplib.UID(lastUID + 1)
	c.cfg.Logger.Debug("imap uid search", "start_uid", uint32(startUID), "fetch_body", c.cfg.FetchBody)

	criteria := &imaplib.SearchCriteria{
		UID: []imaplib.UIDSet{
			{{Start: startUID, Stop: 0}},
		},
	}

	searchData, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap uid search: %w", err)
	}

	uids := searchData.AllUIDs()
	c.cfg.Logger.Debug("imap uid search result", "found", len(uids))
	if len(uids) == 0 {
		return nil, nil
	}

	uidSet := imaplib.UIDSetNum(uids...)

	// First fetch: envelope + extra headers only.
	c.cfg.Logger.Debug("imap fetch starting", "uids", len(uids))
	msgs, err := c.client.Fetch(uidSet, &imaplib.FetchOptions{
		Envelope: true,
		UID:      true,
		BodySection: []*imaplib.FetchItemBodySection{
			extraHeaderSection,
		},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}
	c.cfg.Logger.Debug("imap fetch complete", "fetched", len(msgs))

	result := parseMessages(msgs, c.cfg.Logger)

	// Second fetch: body text only, in a separate command.
	if c.cfg.FetchBody && len(result) > 0 {
		bodies, err := c.fetchBodies(uidSet)
		if err != nil {
			c.cfg.Logger.Warn(fmt.Errorf("body fetch skipped, continuing without body: %w", err))
		} else {
			for i := range result {
				if body, ok := bodies[result[i].UID]; ok {
					result[i].Body = body
				}
			}
		}
	}

	return result, nil
}

// fetchBodies fetches plain-text body for each UID using BODY.PEEK[TEXT].
// Returns a map of UID → stripped, truncated body text.
// Must be called with c.mu held (it is invoked from FetchSince).
func (c *Client) fetchBodies(uidSet imaplib.UIDSet) (map[uint32]string, error) {
	c.cfg.Logger.Debug("imap body fetch starting")
	msgs, err := c.client.Fetch(uidSet, &imaplib.FetchOptions{
		UID:         true,
		BodySection: []*imaplib.FetchItemBodySection{peekBodySection},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap body fetch: %w", err)
	}

	bodies := make(map[uint32]string, len(msgs))
	for _, m := range msgs {
		if bodyBytes := m.FindBodySection(peekBodySection); len(bodyBytes) > 0 {
			body := truncate(stripHTML(string(bodyBytes)), maxBodyLen)
			bodies[uint32(m.UID)] = body
			c.cfg.Logger.Debug("imap body fetched", "uid", uint32(m.UID), "raw_bytes", len(bodyBytes), "stripped_len", len(body))
		} else {
			c.cfg.Logger.Debug("imap body not found in response", "uid", uint32(m.UID))
		}
	}
	c.cfg.Logger.Debug("imap body fetch complete", "fetched", len(bodies))
	return bodies, nil
}

// MarkRead sets the \Seen flag on the message with the given UID.
func (c *Client) MarkRead(_ context.Context, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return fmt.Errorf("imap: not connected")
	}

	set := imaplib.UIDSetNum(imaplib.UID(uid))
	cmd := c.client.Store(set, &imaplib.StoreFlags{
		Op:     imaplib.StoreFlagsAdd,
		Silent: true, // we don't need the updated flags echoed back
		Flags:  []imaplib.Flag{imaplib.FlagSeen},
	}, nil) // a UIDSet makes this a UID STORE
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("imap mark read uid %d: %w", uid, err)
	}
	c.cfg.Logger.Debug("imap marked read", "uid", uid)
	return nil
}

// FetchBody returns the plain-text body of the message with the given UID,
// fetched on demand with BODY.PEEK[TEXT] so the read state is not changed.
// Returns an empty string when the message has no text body.
func (c *Client) FetchBody(_ context.Context, uid uint32) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return "", fmt.Errorf("imap: not connected")
	}

	set := imaplib.UIDSetNum(imaplib.UID(uid))
	msgs, err := c.client.Fetch(set, &imaplib.FetchOptions{
		UID:         true,
		BodySection: []*imaplib.FetchItemBodySection{peekBodySection},
	}).Collect()
	if err != nil {
		return "", fmt.Errorf("imap fetch body uid %d: %w", uid, err)
	}

	for _, m := range msgs {
		if bodyBytes := m.FindBodySection(peekBodySection); len(bodyBytes) > 0 {
			return truncate(stripHTML(string(bodyBytes)), maxBodyLen), nil
		}
	}
	return "", nil
}

// Close logs out and closes the IMAP connection.
func (c *Client) Close() error {
	if c.client == nil {
		return nil
	}
	if err := c.client.Logout().Wait(); err != nil {
		return fmt.Errorf("imap logout: %w", err)
	}
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("imap close: %w", err)
	}
	return nil
}

func parseMessages(msgs []*imapclient.FetchMessageBuffer, logger log.Logger) []email.Message {
	out := make([]email.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Envelope == nil {
			logger.Debug("imap message skipped: no envelope", "uid", uint32(m.UID))
			continue
		}
		msg := email.Message{
			UID:     uint32(m.UID),
			Subject: m.Envelope.Subject,
			Date:    m.Envelope.Date,
		}
		if len(m.Envelope.From) > 0 {
			from := m.Envelope.From[0]
			msg.FromEmail = from.Addr()
			msg.FromName = from.Name
		}

		if headerBytes := m.FindBodySection(extraHeaderSection); len(headerBytes) > 0 {
			h := parseHeaderBytes(headerBytes)
			msg.InReplyTo = h.Get("In-Reply-To")
			msg.ListUnsubscribe = h.Get("List-Unsubscribe")
			msg.Precedence = h.Get("Precedence")
		}

		out = append(out, msg)
	}
	return out
}

func parseHeaderBytes(data []byte) textproto.MIMEHeader {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	// ReadMIMEHeader returns io.EOF when headers end without a blank line,
	// which is normal for IMAP HEADER.FIELDS fetches. Use partial results.
	h, err := r.ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return textproto.MIMEHeader{}
	}
	return h
}

// stripHTML removes HTML tags and normalises whitespace.
func stripHTML(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.TextToken {
			b.Write(z.Text())
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// truncate cuts s to at most n bytes on a valid UTF-8 boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for !utf8.ValidString(s[:n]) {
		n--
	}
	return s[:n]
}

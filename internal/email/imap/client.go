package imap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2/imapclient"
	"golang.org/x/net/html"
	"golang.org/x/oauth2"

	"github.com/paperspell/email-assistant/internal/auth/oauth"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/pkg/log"

	imaplib "github.com/emersion/go-imap/v2"
)

const maxBodyLen = 3000

// extraHeaders lists the additional header fields fetched alongside ENVELOPE.
var extraHeaders = []string{"In-Reply-To", "List-Unsubscribe", "Precedence", "List-Id"}

// Peek avoids setting the \Seen flag; a BODY[HEADER.FIELDS] fetch without it
// implicitly marks the message as read (RFC 3501 §6.4.5).
var extraHeaderSection = &imaplib.FetchItemBodySection{
	Specifier:    imaplib.PartSpecifierHeader,
	HeaderFields: extraHeaders,
	Peek:         true,
}

// peekBodySection fetches the whole message without setting the \Seen flag.
// The full message — not BODY[TEXT] — because the top-level headers carry the
// Content-Type and multipart boundary that extractText needs to tell the letter
// apart from the MIME plumbing around it.
// Peek is ignored by the server when echoing the section spec back, so
// FindBodySection matches it correctly regardless of the Peek field.
var peekBodySection = &imaplib.FetchItemBodySection{
	Specifier: imaplib.PartSpecifierNone,
	Peek:      true,
}

// Config holds connection parameters for an IMAP server.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	// TokenSource, when set, authenticates with XOAUTH2 (OAuth) instead of a
	// password LOGIN. Username is still used as the SASL identity.
	TokenSource oauth2.TokenSource
	TLS         bool
	FetchBody   bool       // fetch plain-text body when true (content.mode = full_body)
	Logger      log.Logger // nil → no debug output
}

// Client implements email.Provider using IMAP.
type Client struct {
	cfg    Config
	client *imapclient.Client
	// mu serializes IMAP commands over the single connection, which is shared
	// between the polling goroutine (FetchSince) and on-demand mailbox actions
	// (MarkRead, FetchBody, MoveToTrash) triggered from Telegram callbacks.
	mu sync.Mutex
	// trash caches the resolved Trash mailbox name ("" = none/use \Deleted
	// fallback); trashResolved guards the one-time lookup. Accessed under mu.
	trash         string
	trashResolved bool
	// reconnectFn, when set, fully replaces reconnect. It is a test seam to
	// exercise exec's reconnect/retry path without a real server.
	reconnectFn func() error
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
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dial()
}

// dial establishes a fresh authenticated connection with INBOX selected and
// stores it on c, replacing any previous client. Callers must hold mu.
func (c *Client) dial() error {
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

	if err := c.authenticate(cl); err != nil {
		cl.Close() //nolint:errcheck
		return err
	}

	if _, err := cl.Select("INBOX", nil).Wait(); err != nil {
		cl.Close() //nolint:errcheck
		return fmt.Errorf("imap select INBOX: %w", err)
	}

	c.client = cl
	return nil
}

// exec runs an IMAP operation, transparently reconnecting once if the connection
// is not established or is dropped mid-session. Reconnecting re-runs
// authenticate, which refreshes an expired access token — and surfaces a rejected
// refresh token (via oauth.IsReauthRequired) so the scheduler can alert the user.
// Callers must hold mu.
func (c *Client) exec(op func() error) error {
	if c.client == nil {
		if err := c.reconnect(); err != nil {
			return err
		}
	}

	err := op()
	if err == nil || !isConnErr(err) {
		return err
	}

	c.cfg.Logger.Debug("imap connection lost, reconnecting", "err", err.Error())
	if rerr := c.reconnect(); rerr != nil {
		return rerr
	}
	return op()
}

// reconnect closes any existing connection and dials a fresh one. Must hold mu.
func (c *Client) reconnect() error {
	if c.reconnectFn != nil {
		return c.reconnectFn()
	}
	if c.client != nil {
		c.client.Close() //nolint:errcheck
		c.client = nil
	}
	return c.dial()
}

// isConnErr reports whether err indicates a broken connection that a reconnect
// could recover, as opposed to a server-side command error.
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	s := err.Error()
	for _, needle := range []string{
		"use of closed network connection",
		"connection reset",
		"broken pipe",
		"connection closed",
		"EOF",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// authenticate logs in with XOAUTH2 when a token source is configured, otherwise
// with a password LOGIN.
func (c *Client) authenticate(cl *imapclient.Client) error {
	if c.cfg.TokenSource != nil {
		tok, err := c.cfg.TokenSource.Token() // refreshes the access token if expired
		if err != nil {
			if oauth.IsReauthRequired(err) {
				return fmt.Errorf(
					"imap oauth: refresh token rejected for %s — re-run 'email-agent account add' to re-authorize: %w",
					c.cfg.Username, err)
			}
			return fmt.Errorf("imap oauth token: %w", err)
		}
		if err := cl.Authenticate(oauth.NewXOAUTH2(c.cfg.Username, tok.AccessToken)); err != nil {
			return fmt.Errorf("imap xoauth2: %w", err)
		}
		return nil
	}
	if err := cl.Login(c.cfg.Username, c.cfg.Password).Wait(); err != nil {
		return fmt.Errorf("imap login: %w", err)
	}
	return nil
}

// LatestUID returns the highest UID boundary in INBOX (UIDNEXT-1) with a single
// STATUS command, without fetching any messages. Used to set the first-run
// baseline cheaply. Returns 0 for an empty mailbox.
func (c *Client) LatestUID(_ context.Context) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var uidNext imaplib.UID
	err := c.exec(func() error {
		data, serr := c.client.Status("INBOX", &imaplib.StatusOptions{UIDNext: true}).Wait()
		if serr != nil {
			return fmt.Errorf("imap status uidnext: %w", serr)
		}
		uidNext = data.UIDNext
		return nil
	})
	if err != nil {
		return 0, err
	}
	if uidNext <= 1 {
		return 0, nil
	}
	return uint32(uidNext) - 1, nil
}

// FetchSince returns messages with UID greater than lastUID.
// Body is fetched in a separate IMAP command when FetchBody is true to avoid
// a Zoho server bug where combining BODY[HEADER.FIELDS] and BODY[TEXT] in a
// single FETCH produces a malformed section-spec in the response.
func (c *Client) FetchSince(_ context.Context, lastUID uint32) ([]email.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []email.Message
	err := c.exec(func() error {
		result = nil

		startUID := imaplib.UID(lastUID + 1)
		c.cfg.Logger.Debug("imap uid search", "start_uid", uint32(startUID), "fetch_body", c.cfg.FetchBody)

		criteria := &imaplib.SearchCriteria{
			UID: []imaplib.UIDSet{
				{{Start: startUID, Stop: 0}},
			},
		}

		searchData, err := c.client.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return fmt.Errorf("imap uid search: %w", err)
		}

		uids := searchData.AllUIDs()
		c.cfg.Logger.Debug("imap uid search result", "found", len(uids))
		if len(uids) == 0 {
			return nil
		}

		msgs, ferr := c.fetchMessages(imaplib.UIDSetNum(uids...), len(uids))
		if ferr != nil {
			return ferr
		}
		result = msgs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// FetchUnseenSince returns unread messages received on or after `since`, capped
// to the newest `limit` UIDs (0 = no cap). Used for the first-run backfill. Read
// state is not changed (BODY.PEEK), so the messages stay unread in the mailbox.
func (c *Client) FetchUnseenSince(_ context.Context, since time.Time, limit int) ([]email.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []email.Message
	err := c.exec(func() error {
		result = nil

		criteria := &imaplib.SearchCriteria{
			Since:   since,
			NotFlag: []imaplib.Flag{imaplib.FlagSeen}, // UNSEEN
		}
		searchData, serr := c.client.UIDSearch(criteria, nil).Wait()
		if serr != nil {
			return fmt.Errorf("imap uid search unseen: %w", serr)
		}

		uids := searchData.AllUIDs()
		c.cfg.Logger.Debug("imap backfill search", "since", since, "found", len(uids), "limit", limit)
		if len(uids) == 0 {
			return nil
		}
		// AllUIDs is ascending; keep the newest `limit`.
		if limit > 0 && len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}

		msgs, ferr := c.fetchMessages(imaplib.UIDSetNum(uids...), len(uids))
		if ferr != nil {
			return ferr
		}
		result = msgs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// fetchMessages fetches envelopes (+ extra headers) and, when FetchBody is set,
// plain-text bodies for the given UID set, returning parsed messages. Body is
// fetched in a separate command to avoid a Zoho bug (see FetchSince). Must hold mu.
func (c *Client) fetchMessages(uidSet imaplib.UIDSet, count int) ([]email.Message, error) {
	c.cfg.Logger.Debug("imap fetch starting", "uids", count)
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

	if c.cfg.FetchBody && len(result) > 0 {
		bodies, berr := c.fetchBodies(uidSet)
		if berr != nil {
			c.cfg.Logger.Warn(fmt.Errorf("body fetch skipped, continuing without body: %w", berr))
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
			body := truncate(extractText(bodyBytes), maxBodyLen)
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

	return c.exec(func() error {
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
	})
}

// FetchBody returns the plain-text body of the message with the given UID,
// fetched on demand with BODY.PEEK[TEXT] so the read state is not changed.
// Returns an empty string when the message has no text body.
func (c *Client) FetchBody(_ context.Context, uid uint32) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var body string
	err := c.exec(func() error {
		body = ""
		set := imaplib.UIDSetNum(imaplib.UID(uid))
		msgs, err := c.client.Fetch(set, &imaplib.FetchOptions{
			UID:         true,
			BodySection: []*imaplib.FetchItemBodySection{peekBodySection},
		}).Collect()
		if err != nil {
			return fmt.Errorf("imap fetch body uid %d: %w", uid, err)
		}
		for _, m := range msgs {
			if bodyBytes := m.FindBodySection(peekBodySection); len(bodyBytes) > 0 {
				body = truncate(extractText(bodyBytes), maxBodyLen)
				return nil
			}
		}
		return nil
	})
	return body, err
}

// MoveToTrash moves the message with the given UID to the Trash mailbox. When no
// Trash mailbox can be resolved, it falls back to setting \Deleted and expunging.
func (c *Client) MoveToTrash(_ context.Context, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.exec(func() error {
		set := imaplib.UIDSetNum(imaplib.UID(uid))

		if trash := c.resolveTrash(); trash != "" {
			_, err := c.client.Move(set, trash).Wait()
			if err == nil {
				c.cfg.Logger.Debug("imap moved to trash", "uid", uid, "mailbox", trash)
				return nil
			}
			if isConnErr(err) {
				return fmt.Errorf("imap move uid %d: %w", uid, err) // let exec reconnect and retry
			}
			c.cfg.Logger.Debug("imap move failed, falling back to delete",
				"uid", uid, "mailbox", trash, "err", err.Error())
		}

		// Fallback: mark \Deleted and expunge.
		store := c.client.Store(set, &imaplib.StoreFlags{
			Op:     imaplib.StoreFlagsAdd,
			Silent: true,
			Flags:  []imaplib.Flag{imaplib.FlagDeleted},
		}, nil)
		if err := store.Close(); err != nil {
			return fmt.Errorf("imap mark deleted uid %d: %w", uid, err)
		}
		if _, err := c.client.Expunge().Collect(); err != nil {
			return fmt.Errorf("imap expunge uid %d: %w", uid, err)
		}
		c.cfg.Logger.Debug("imap deleted (no trash mailbox)", "uid", uid)
		return nil
	})
}

// resolveTrash finds the Trash mailbox once and caches it. It prefers the
// SPECIAL-USE \Trash attribute, then falls back to common mailbox names. Returns
// "" when none is found (callers then use the \Deleted fallback). Must hold mu.
func (c *Client) resolveTrash() string {
	if c.trashResolved {
		return c.trash
	}
	c.trashResolved = true

	if datas, err := c.client.List("", "*", &imaplib.ListOptions{ReturnSpecialUse: true}).Collect(); err == nil {
		for _, d := range datas {
			for _, a := range d.Attrs {
				if a == imaplib.MailboxAttrTrash {
					c.trash = d.Mailbox
					return c.trash
				}
			}
		}
	}
	for _, name := range []string{"Trash", "[Gmail]/Trash", "Deleted Items", "Deleted Messages"} {
		if _, err := c.client.Status(name, &imaplib.StatusOptions{}).Wait(); err == nil {
			c.trash = name
			return c.trash
		}
	}
	c.trash = ""
	return ""
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
			msg.ListID = h.Get("List-Id")
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
	// skipDepth counts the nesting of elements whose text content is markup, not
	// letter: a <style> block renders as a wall of CSS, which is what made HTML
	// mail look like it carried technical noise.
	skipDepth := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()

		case html.StartTagToken:
			name, _ := z.TagName()
			if isNonContentTag(string(name)) {
				skipDepth++
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			if isNonContentTag(string(name)) {
				if skipDepth > 0 {
					skipDepth--
				}
				continue
			}
			// Block-level ends become line breaks so paragraphs survive.
			if skipDepth == 0 && isBlockTag(string(name)) {
				b.WriteByte('\n')
			}

		case html.SelfClosingTagToken:
			name, _ := z.TagName()
			if skipDepth == 0 && string(name) == "br" {
				b.WriteByte('\n')
			}

		case html.TextToken:
			if skipDepth == 0 {
				b.Write(z.Text())
			}
		}
	}
}

// isNonContentTag reports whether an element's text is markup rather than the
// letter itself.
func isNonContentTag(name string) bool {
	switch name {
	case "style", "script", "head", "title", "noscript":
		return true
	}
	return false
}

// isBlockTag reports whether closing the element should end a line.
func isBlockTag(name string) bool {
	switch name {
	case "p", "div", "br", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table", "blockquote":
		return true
	}
	return false
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

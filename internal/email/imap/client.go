package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/paperspell/email-assistant/internal/email"

	imaplib "github.com/emersion/go-imap/v2"
)

// Config holds connection parameters for an IMAP server.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	TLS      bool
}

// Client implements email.Provider using IMAP.
type Client struct {
	cfg    Config
	client *imapclient.Client
}

// NewClient creates an IMAP Client. Call Connect before using.
func NewClient(cfg Config) *Client {
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
func (c *Client) FetchSince(_ context.Context, lastUID uint32) ([]email.Message, error) {
	if c.client == nil {
		return nil, fmt.Errorf("imap: not connected")
	}

	// UID 0 is invalid in IMAP; start from 1 for the initial poll.
	startUID := imaplib.UID(lastUID + 1)

	criteria := &imaplib.SearchCriteria{
		UID: []imaplib.UIDSet{
			{{Start: startUID, Stop: 0}}, // Stop:0 means * (last message)
		},
	}

	searchData, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap uid search: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}

	uidSet := imaplib.UIDSetNum(uids...)
	fetchOptions := &imaplib.FetchOptions{Envelope: true, UID: true}

	msgs, err := c.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}

	return parseMessages(msgs), nil
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

func parseMessages(msgs []*imapclient.FetchMessageBuffer) []email.Message {
	out := make([]email.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Envelope == nil {
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
		out = append(out, msg)
	}
	return out
}

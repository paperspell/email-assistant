package scheduler

import (
	"context"
	"fmt"
	"html"
	"sync"
	"time"

	"github.com/paperspell/email-assistant/internal/i18n"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

// defaultCreditAlertRepeat is how long an ongoing outage waits before the owner
// is reminded: once a day keeps an unnoticed alert from being lost without
// turning a weekend's outage into a stream of identical messages.
const defaultCreditAlertRepeat = 24 * time.Hour

// CreditAlert tells the owner in Telegram that the classifier's prepaid
// balance ran out, and again once it is back. It is shared by every account's
// scheduler: there is one provider and one balance, and five mailboxes must not
// produce five identical alerts.
//
// While credits are out, classification falls back to the built-in rules. The
// alert says so, because the owner otherwise has no way to know that some
// important mail is now judged by keywords alone.
type CreditAlert struct {
	Alerter Alerter
	Printer *i18n.Printer
	Logger  log.Logger
	// Provider is the display name, e.g. "Gemini"; TopUpURL is where the owner
	// adds funds. An empty TopUpURL omits the line.
	Provider string
	TopUpURL string
	// Repeat is the reminder interval while the outage lasts; zero means a day.
	Repeat time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	mu       sync.Mutex
	out      bool      // credits are known to be exhausted
	lastSent time.Time // when the owner was last told; zero forces the next send
}

// Failed records a request refused for lack of credits and alerts the owner on
// the first one, then at most once per Repeat while the outage lasts. A nil
// CreditAlert, or one without an Alerter, does nothing.
func (c *CreditAlert) Failed(ctx context.Context) {
	if c == nil || c.Alerter == nil {
		return
	}
	c.mu.Lock()
	now := c.now()
	if c.out && !c.lastSent.IsZero() && now.Sub(c.lastSent) < c.repeat() {
		c.mu.Unlock()
		return
	}
	c.out = true
	c.lastSent = now
	c.mu.Unlock()

	if err := c.Alerter.SendAlert(ctx, c.exhaustedText()); err != nil {
		// Unsent is not sent: clear the mark so the next failure tries again
		// instead of the outage going silent for a day.
		c.mu.Lock()
		c.lastSent = time.Time{}
		c.mu.Unlock()
		c.warn(fmt.Errorf("send out-of-credits alert: %w", err))
	}
}

// Succeeded records a successful classification. After an outage it tells the
// owner once that the classifier is working again; otherwise it does nothing.
func (c *CreditAlert) Succeeded(ctx context.Context) {
	if c == nil || c.Alerter == nil {
		return
	}
	c.mu.Lock()
	if !c.out {
		c.mu.Unlock()
		return
	}
	c.out = false
	c.lastSent = time.Time{}
	c.mu.Unlock()

	if err := c.Alerter.SendAlert(ctx, c.restoredText()); err != nil {
		c.warn(fmt.Errorf("send credits-restored notice: %w", err))
	}
}

// exhaustedText is sent with HTML parse mode; every piece is escaped, the
// catalog strings included, since a translation may contain "&" or "<".
func (c *CreditAlert) exhaustedText() string {
	p := c.Printer
	text := "⚠️ <b>" + html.EscapeString(p.T("credits_exhausted_title", "Provider", c.Provider)) + "</b>\n\n" +
		html.EscapeString(p.T("credits_exhausted_body"))
	if c.TopUpURL != "" {
		text += "\n\n" + html.EscapeString(p.T("credits_top_up")) + " " + html.EscapeString(c.TopUpURL)
	}
	return text
}

func (c *CreditAlert) restoredText() string {
	return "✅ " + html.EscapeString(c.Printer.T("credits_restored", "Provider", c.Provider))
}

func (c *CreditAlert) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *CreditAlert) repeat() time.Duration {
	if c.Repeat > 0 {
		return c.Repeat
	}
	return defaultCreditAlertRepeat
}

func (c *CreditAlert) warn(err error) {
	if c.Logger != nil {
		c.Logger.Warn(err)
	}
}

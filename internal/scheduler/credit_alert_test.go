package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/i18n"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newCreditAlert(a *mockAlerter, clock *fakeClock) *CreditAlert {
	return &CreditAlert{
		Alerter: a, Printer: i18n.English(), Logger: log.Noop{},
		Provider: "Gemini", TopUpURL: "https://aistudio.google.com/billing",
		Now: clock.now,
	}
}

func TestCreditAlert_OncePerOutageThenDaily(t *testing.T) {
	a, clock := &mockAlerter{}, &fakeClock{t: time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)}
	c := newCreditAlert(a, clock)
	ctx := context.Background()

	c.Failed(ctx)
	require.Equal(t, 1, a.count(), "the first refused request alerts")
	assert.Contains(t, a.texts[0], "Gemini: prepaid credits have run out")
	assert.Contains(t, a.texts[0], "https://aistudio.google.com/billing")
	assert.Contains(t, a.texts[0], "built-in rules", "the owner must learn what degrades meanwhile")

	for range 50 { // a busy afternoon of refused requests
		clock.advance(10 * time.Minute)
		c.Failed(ctx)
	}
	assert.Equal(t, 1, a.count(), "an outage within a day alerts once")

	clock.advance(24 * time.Hour)
	c.Failed(ctx)
	assert.Equal(t, 2, a.count(), "a day on, the owner is reminded")
}

func TestCreditAlert_RecoveryIsAnnouncedOnce(t *testing.T) {
	a, clock := &mockAlerter{}, &fakeClock{t: time.Now()}
	c := newCreditAlert(a, clock)
	ctx := context.Background()

	c.Succeeded(ctx)
	assert.Zero(t, a.count(), "a success with no outage says nothing")

	c.Failed(ctx)
	c.Succeeded(ctx)
	c.Succeeded(ctx)
	require.Equal(t, 2, a.count(), "one alert, one recovery — not a recovery per email")
	assert.Contains(t, a.texts[1], "Gemini is working again")

	// A new outage after recovery alerts at once, not a day later.
	c.Failed(ctx)
	assert.Equal(t, 3, a.count())
}

func TestCreditAlert_UnsentAlertIsRetried(t *testing.T) {
	// If Telegram is down when credits run out, the next failure must try
	// again rather than the outage staying silent for a day.
	a, clock := &mockAlerter{err: errors.New("telegram unreachable")}, &fakeClock{t: time.Now()}
	c := newCreditAlert(a, clock)
	ctx := context.Background()

	c.Failed(ctx)
	a.mu.Lock()
	a.err = nil
	a.mu.Unlock()
	c.Failed(ctx)

	assert.Equal(t, 1, a.count(), "the second failure delivered the alert the first could not")
}

func TestCreditAlert_ConcurrentAccountsAlertOnce(t *testing.T) {
	// Every account's scheduler shares one alert; five mailboxes hitting the
	// same empty balance at once must produce one message.
	a, clock := &mockAlerter{}, &fakeClock{t: time.Now()}
	c := newCreditAlert(a, clock)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Failed(context.Background()) }()
	}
	wg.Wait()
	assert.Equal(t, 1, a.count())
}

func TestCreditAlert_NilIsSafe(t *testing.T) {
	var c *CreditAlert
	assert.NotPanics(t, func() { c.Failed(context.Background()); c.Succeeded(context.Background()) })
}

func TestCreditAlert_EscapesForHTML(t *testing.T) {
	// The alert is sent with HTML parse mode: a provider name or URL with "&"
	// must not break Telegram's parser.
	a, clock := &mockAlerter{}, &fakeClock{t: time.Now()}
	c := newCreditAlert(a, clock)
	c.Provider, c.TopUpURL = "A&B", "https://x.test/?a=1&b=2"

	c.Failed(context.Background())

	require.Equal(t, 1, a.count())
	assert.Contains(t, a.texts[0], "A&amp;B")
	assert.Contains(t, a.texts[0], "a=1&amp;b=2")
}

func TestScheduler_OutOfCreditsAlertsOnceAcrossAccounts(t *testing.T) {
	// Two accounts, one shared alert, a provider answering 402 for every email.
	a := &mockAlerter{}
	alert := &CreditAlert{Alerter: a, Printer: i18n.English(), Logger: log.Noop{}, Provider: "Gemini"}
	outOfCredits := fmt.Errorf("gemini classify: http 402: prepayment credits are depleted: %w", llm.ErrOutOfCredits)

	for i, uid := range []uint32{50, 51} {
		msg := email.Message{UID: uid, Subject: fmt.Sprintf("Invoice %d", i), FromEmail: "billing@x.com", Date: time.Now()}
		sched, syncRepo := buildContentModeScheduler(t, msg, &capturingLLMProvider{}, "full_body")
		sched.cfg.LLMProvider = &mockLLMProvider{err: outOfCredits}
		sched.cfg.CreditAlert = alert
		pollOnce(t, sched, syncRepo, uid-1)

		// The email is still processed — by the rule-based fallback — rather
		// than lost with the failed call.
		e, err := sched.cfg.EmailRepo.GetByAccountAndUID(context.Background(), "test@example.com", uid)
		require.NoError(t, err)
		require.NotNil(t, e)
		assert.NotEqual(t, domain.StatusNew, e.Status, "the fallback must still decide the email")
	}
	assert.Equal(t, 1, a.count(), "two accounts, one empty balance, one alert")
}

func TestScheduler_OtherLLMErrorsDoNotAlert(t *testing.T) {
	a := &mockAlerter{}
	alert := &CreditAlert{Alerter: a, Printer: i18n.English(), Logger: log.Noop{}, Provider: "Gemini"}
	msg := email.Message{UID: 60, Subject: "s", FromEmail: "a@b.com", Date: time.Now()}
	sched, syncRepo := buildContentModeScheduler(t, msg, &capturingLLMProvider{}, "full_body")
	sched.cfg.LLMProvider = &mockLLMProvider{err: errors.New("gemini classify: http 503: overloaded")}
	sched.cfg.CreditAlert = alert

	pollOnce(t, sched, syncRepo, 59)

	assert.Zero(t, a.count(), "a transient failure is not a balance to top up")
}

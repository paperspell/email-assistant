package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"

	"github.com/paperspell/email-assistant/internal/pkg/log"
)

type mockAlerter struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (m *mockAlerter) SendAlert(_ context.Context, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.texts = append(m.texts, text)
	return nil
}

func (m *mockAlerter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.texts)
}

// reauthErr mirrors how internal/email/imap wraps the oauth2 error chain when a
// refresh token is rejected (invalid_grant).
func reauthErr() error {
	return fmt.Errorf("imap oauth: refresh token rejected: %w",
		&oauth2.RetrieveError{ErrorCode: "invalid_grant"})
}

func newAlertScheduler(al Alerter) *Scheduler {
	return &Scheduler{cfg: Config{
		AccountID:    "acc-1",
		AccountName:  "Personal",
		AccountEmail: "me@example.com",
		Alerter:      al,
		Logger:       log.Noop{},
	}}
}

func TestAlertReauth_SendsOnceThenDedups(t *testing.T) {
	al := &mockAlerter{}
	s := newAlertScheduler(al)
	ctx := context.Background()

	s.alertReauthIfNeeded(ctx, reauthErr())
	s.alertReauthIfNeeded(ctx, reauthErr()) // deduped: still one alert
	assert.Equal(t, 1, al.count())

	// A successful poll clears the flag, so a later expiry alerts again.
	s.reauthAlerted = false
	s.alertReauthIfNeeded(ctx, reauthErr())
	assert.Equal(t, 2, al.count())

	assert.Contains(t, al.texts[0], "account edit me@example.com")
	assert.Contains(t, al.texts[0], "Personal (me@example.com)")
}

func TestAlertReauth_IgnoresNonReauthErrors(t *testing.T) {
	al := &mockAlerter{}
	s := newAlertScheduler(al)

	s.alertReauthIfNeeded(context.Background(), errors.New("connection reset"))
	s.alertReauthIfNeeded(context.Background(), nil)
	assert.Equal(t, 0, al.count())
	assert.False(t, s.reauthAlerted)
}

func TestAlertReauth_NilAlerterNoPanic(t *testing.T) {
	s := newAlertScheduler(nil)
	assert.NotPanics(t, func() {
		s.alertReauthIfNeeded(context.Background(), reauthErr())
	})
}

func TestAlertReauth_SendFailureIsRetryable(t *testing.T) {
	al := &mockAlerter{err: errors.New("telegram down")}
	s := newAlertScheduler(al)

	// Send fails, so the flag stays false and the next attempt tries again.
	s.alertReauthIfNeeded(context.Background(), reauthErr())
	assert.False(t, s.reauthAlerted)

	al.err = nil
	s.alertReauthIfNeeded(context.Background(), reauthErr())
	assert.Equal(t, 1, al.count())
	assert.True(t, s.reauthAlerted)
}

func TestReauthAlertText_UsesEmailWhenNameEmpty(t *testing.T) {
	s := &Scheduler{cfg: Config{AccountEmail: "solo@example.com", Logger: log.Noop{}}}
	txt := s.reauthAlertText()
	assert.Contains(t, txt, "Account: solo@example.com")
	assert.False(t, strings.Contains(txt, "()"), "no empty name parens")
}

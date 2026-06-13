package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

// --- mocks ---

type mockProvider struct {
	messages []email.Message
	fetchErr error
	mu       sync.Mutex
	lastUID  uint32
}

func (m *mockProvider) Connect(_ context.Context) error { return nil }

func (m *mockProvider) FetchSince(_ context.Context, lastUID uint32) ([]email.Message, error) {
	m.mu.Lock()
	m.lastUID = lastUID
	m.mu.Unlock()
	return m.messages, m.fetchErr
}

func (m *mockProvider) Close() error { return nil }

type mockNotifier struct {
	mu     sync.Mutex
	sent   []domain.Email
	failOn func(domain.Email) bool
}

func (m *mockNotifier) SendNewEmail(_ context.Context, e domain.Email) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn != nil && m.failOn(e) {
		return errors.New("notify failed")
	}
	m.sent = append(m.sent, e)
	return nil
}

func (m *mockNotifier) getSent() []domain.Email {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Email(nil), m.sent...)
}

// --- helpers ---

func newTestScheduler(
	t *testing.T, provider email.Provider, notifier *mockNotifier,
) (*Scheduler, *repo.EmailRepo, *repo.SyncStateRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)

	sched := New(Config{
		AccountID:    "test@example.com",
		PollInterval: time.Hour,
		EmailRepo:    emailRepo,
		SyncRepo:     syncRepo,
		Provider:     provider,
		Notifier:     notifier,
		Logger:       log.Noop{},
	})
	return sched, emailRepo, syncRepo
}

func runOnce(t *testing.T, sched *Scheduler) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, sched.Start(ctx))
}

// --- tests ---

func TestScheduler_NoNewMessages(t *testing.T) {
	provider := &mockProvider{}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestScheduler(t, provider, notifier)

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent())
	state, err := syncRepo.Get(context.Background(), "test@example.com")
	require.NoError(t, err)
	assert.Nil(t, state, "sync state should not be created when no messages arrive")
}

func TestScheduler_ProcessesNewMessages(t *testing.T) {
	messages := []email.Message{
		{UID: 10, Subject: "First", FromEmail: "a@b.com", Date: time.Now()},
		{UID: 11, Subject: "Second", FromEmail: "b@c.com", Date: time.Now()},
	}
	provider := &mockProvider{messages: messages}
	notifier := &mockNotifier{}
	sched, emailRepo, syncRepo := newTestScheduler(t, provider, notifier)

	runOnce(t, sched)

	sent := notifier.getSent()
	assert.Len(t, sent, 2)

	state, err := syncRepo.Get(context.Background(), "test@example.com")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, uint32(11), state.LastUID)

	for _, uid := range []uint32{10, 11} {
		e, err := emailRepo.GetByAccountAndUID(context.Background(), "test@example.com", uid)
		require.NoError(t, err)
		require.NotNil(t, e)
		assert.Equal(t, domain.StatusNotified, e.Status)
	}
}

func TestScheduler_PartialFailure_ContinuesOtherMessages(t *testing.T) {
	messages := []email.Message{
		{UID: 5, Subject: "Fail", FromEmail: "fail@b.com", Date: time.Now()},
		{UID: 6, Subject: "OK", FromEmail: "ok@b.com", Date: time.Now()},
	}
	provider := &mockProvider{messages: messages}
	notifier := &mockNotifier{
		failOn: func(e domain.Email) bool { return e.MessageUID == 5 },
	}
	sched, _, syncRepo := newTestScheduler(t, provider, notifier)

	runOnce(t, sched)

	sent := notifier.getSent()
	require.Len(t, sent, 1)
	assert.Equal(t, "OK", sent[0].Subject)

	// SyncState advances only for the successful message
	state, err := syncRepo.Get(context.Background(), "test@example.com")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, uint32(6), state.LastUID)
}

func TestScheduler_ResumesFromLastUID(t *testing.T) {
	provider := &mockProvider{}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestScheduler(t, provider, notifier)

	// Pre-populate sync state
	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com",
		LastUID:   42,
		SyncedAt:  time.Now(),
	}))

	runOnce(t, sched)

	provider.mu.Lock()
	calledWith := provider.lastUID
	provider.mu.Unlock()

	assert.Equal(t, uint32(42), calledWith)
}

func TestScheduler_FetchError_DoesNotCrash(t *testing.T) {
	provider := &mockProvider{fetchErr: errors.New("connection reset")}
	notifier := &mockNotifier{}
	sched, _, _ := newTestScheduler(t, provider, notifier)

	// Should return nil (context cancellation), not the fetch error
	require.NoError(t, runOnceNoErr(t, sched))
	assert.Empty(t, notifier.getSent())
}

func runOnceNoErr(t *testing.T, sched *Scheduler) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	return sched.Start(ctx)
}

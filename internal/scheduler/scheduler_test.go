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
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

// --- mocks ---

type mockLLMProvider struct {
	mu     sync.Mutex
	result llm.ClassifyResult
	err    error
	calls  int
}

func (m *mockLLMProvider) Name() string { return "mock" }

func (m *mockLLMProvider) Classify(_ context.Context, _ llm.ClassifyRequest) (llm.ClassifyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.result, m.err
}

func (m *mockLLMProvider) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

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

func (m *mockNotifier) SendNewEmail(
	_ context.Context, e domain.Email, _ domain.Classification, _ string,
) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn != nil && m.failOn(e) {
		return 0, errors.New("notify failed")
	}
	m.sent = append(m.sent, e)
	return int64(1000 + len(m.sent)), nil
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
	return newTestSchedulerWithLevel(t, provider, notifier, domain.LevelIgnore)
}

func newTestSchedulerWithLevel(
	t *testing.T, provider email.Provider, notifier *mockNotifier, minImportance domain.ImportanceLevel,
) (*Scheduler, *repo.EmailRepo, *repo.SyncStateRepo) {
	return newTestSchedulerFull(t, provider, notifier, minImportance, nil, 0)
}

func newTestSchedulerFull(
	t *testing.T,
	provider email.Provider,
	notifier *mockNotifier,
	minImportance domain.ImportanceLevel,
	llmProvider llm.Provider,
	divergenceWarn int,
) (*Scheduler, *repo.EmailRepo, *repo.SyncStateRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)
	classRepo := repo.NewClassificationRepo(sqlDB)
	senderRepo := repo.NewSenderRepo(sqlDB)
	domainRepo := repo.NewDomainRepo(sqlDB)
	filter := importance.NewFilter(senderRepo, domainRepo)

	sched := New(Config{
		AccountID:           "test@example.com",
		PollInterval:        time.Hour,
		MinImportance:       minImportance,
		EmailRepo:           emailRepo,
		SyncRepo:            syncRepo,
		ClassificationRepo:  classRepo,
		Filter:              filter,
		LLMProvider:         llmProvider,
		ScoreDivergenceWarn: divergenceWarn,
		Provider:            provider,
		Notifier:            notifier,
		Logger:              log.Noop{},
	})
	return sched, emailRepo, syncRepo
}

func runOnce(t *testing.T, sched *Scheduler) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, sched.poll(ctx))
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

func TestScheduler_FirstRun_SkipsExistingMessages(t *testing.T) {
	messages := []email.Message{
		{UID: 10, Subject: "Old", FromEmail: "a@b.com", Date: time.Now()},
		{UID: 11, Subject: "Also old", FromEmail: "b@c.com", Date: time.Now()},
	}
	provider := &mockProvider{messages: messages}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestScheduler(t, provider, notifier)

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent(), "existing emails must not trigger notifications on first run")

	state, err := syncRepo.Get(context.Background(), "test@example.com")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, uint32(11), state.LastUID, "sync state must be set to the highest existing UID")
}

func TestScheduler_ProcessesNewMessages(t *testing.T) {
	messages := []email.Message{
		{UID: 10, Subject: "First", FromEmail: "a@b.com", Date: time.Now()},
		{UID: 11, Subject: "Second", FromEmail: "b@c.com", Date: time.Now()},
	}
	provider := &mockProvider{messages: messages}
	notifier := &mockNotifier{}
	sched, emailRepo, syncRepo := newTestScheduler(t, provider, notifier)

	// Simulate a prior run so this is not treated as first run
	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com",
		LastUID:   9,
		SyncedAt:  time.Now(),
	}))

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

	// Simulate a prior run
	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com",
		LastUID:   4,
		SyncedAt:  time.Now(),
	}))

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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// poll returns the fetch error; Start/pollWithBackoff would swallow it.
	// The key property: no panic, no notification.
	_ = sched.poll(ctx)
	assert.Empty(t, notifier.getSent())
}

// --- importance filtering tests ---
// Score arithmetic for unknown senders (seen_count=0 → -10 from baseline 40):
//   newsletter (List-Unsubscribe): 40 - 40 - 10 = -10 → clamped 0 → Ignore
//   urgent keyword only:           40 + 25 - 10 = 55              → Maybe
//   urgent + meeting keywords:     40 + 25 + 20 - 10 = 75         → Important

func priorRun(t *testing.T, syncRepo *repo.SyncStateRepo, lastUID uint32) {
	t.Helper()
	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com",
		LastUID:   lastUID,
		SyncedAt:  time.Now(),
	}))
}

func TestScheduler_Newsletter_IsIgnored(t *testing.T) {
	msg := email.Message{
		UID:             10,
		Subject:         "Big sale this week!",
		FromEmail:       "news@shop.com",
		Date:            time.Now(),
		ListUnsubscribe: "<mailto:unsub@shop.com>",
	}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, emailRepo, syncRepo := newTestSchedulerWithLevel(t, provider, notifier, domain.LevelImportant)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent())
	e, err := emailRepo.GetByAccountAndUID(context.Background(), "test@example.com", 10)
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Equal(t, domain.StatusIgnored, e.Status)
}

func TestScheduler_ImportantEmail_IsNotified(t *testing.T) {
	// "urgent" (+25) + "meeting" (+20) - unknown sender (-10) + baseline (40) = 75 → Important
	msg := email.Message{
		UID:       10,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, emailRepo, syncRepo := newTestSchedulerWithLevel(t, provider, notifier, domain.LevelImportant)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	sent := notifier.getSent()
	require.Len(t, sent, 1)
	assert.Equal(t, uint32(10), sent[0].MessageUID)
	e, err := emailRepo.GetByAccountAndUID(context.Background(), "test@example.com", 10)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusNotified, e.Status)
}

func TestScheduler_MaybeEmail_IgnoredByDefault(t *testing.T) {
	// "urgent" (+25) - unknown sender (-10) + baseline (40) = 55 → Maybe; default threshold is Important
	msg := email.Message{
		UID:       10,
		Subject:   "urgent update",
		FromEmail: "someone@x.com",
		Date:      time.Now(),
	}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, emailRepo, syncRepo := newTestSchedulerWithLevel(t, provider, notifier, domain.LevelImportant)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent())
	e, err := emailRepo.GetByAccountAndUID(context.Background(), "test@example.com", 10)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIgnored, e.Status)
}

// --- LLM integration tests ---

func TestScheduler_LLMDisabled_BehavesAsRuleBased(t *testing.T) {
	// No LLMProvider: rule-based result used for notification decision
	msg := email.Message{
		UID:       10,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	// nil LLMProvider — rule-based governs
	sched, _, syncRepo := newTestSchedulerFull(t, provider, notifier, domain.LevelImportant, nil, 0)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Len(t, notifier.getSent(), 1)
}

func TestScheduler_LLMCalled_WhenRuleBasedPasses(t *testing.T) {
	// Rule-based says "important" (75 pts) → LLM is called; LLM also says "important"
	// urgent+meeting = 75 → important
	msg := email.Message{
		UID:       10,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	mockLLM := &mockLLMProvider{result: llm.ClassifyResult{
		Level:    domain.LevelImportant,
		Category: domain.CategoryWork,
		Score:    82,
		Reasons:  []string{"llm signal"},
		Summary:  "Important work meeting.",
	}}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestSchedulerFull(t, provider, notifier, domain.LevelImportant, mockLLM, 0)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Len(t, notifier.getSent(), 1)
	assert.Equal(t, 1, mockLLM.getCalls())
}

func TestScheduler_LLMOverrides_IgnoresWhenLLMSaysIgnore(t *testing.T) {
	// Rule-based says "important" (75 pts) → passes gate → LLM called → LLM says "ignore" → no notification
	msg := email.Message{
		UID:       10,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	mockLLM := &mockLLMProvider{result: llm.ClassifyResult{
		Level:   domain.LevelIgnore,
		Score:   10,
		Summary: "Marketing email disguised as urgent meeting.",
	}}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestSchedulerFull(t, provider, notifier, domain.LevelImportant, mockLLM, 0)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent())
}

func TestScheduler_LLMError_FallsBackToRuleBased(t *testing.T) {
	// LLM fails: rule-based result is used instead
	msg := email.Message{
		UID:       10,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	mockLLM := &mockLLMProvider{err: errors.New("quota exceeded")}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestSchedulerFull(t, provider, notifier, domain.LevelImportant, mockLLM, 0)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	// Rule-based: urgent+meeting = 75 → important → should notify
	assert.Len(t, notifier.getSent(), 1)
}

func TestScheduler_LLMNotCalledWhenRuleBasedIgnores(t *testing.T) {
	// Newsletter ignored by rule-based → LLM should not be called at all
	msg := email.Message{
		UID:             10,
		Subject:         "Big sale!",
		FromEmail:       "news@shop.com",
		Date:            time.Now(),
		ListUnsubscribe: "<unsub@shop.com>",
	}
	mockLLM := &mockLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 90}}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestSchedulerFull(t, provider, notifier, domain.LevelImportant, mockLLM, 0)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent())
	assert.Equal(t, 0, mockLLM.getCalls(), "LLM must not be called when rule-based ignores")
}

func TestScheduler_MaybeEmail_NotifiedWhenThresholdLowered(t *testing.T) {
	// same 55-point email, but min_importance=maybe → should notify
	msg := email.Message{
		UID:       10,
		Subject:   "urgent update",
		FromEmail: "someone@x.com",
		Date:      time.Now(),
	}
	provider := &mockProvider{messages: []email.Message{msg}}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestSchedulerWithLevel(t, provider, notifier, domain.LevelMaybe)
	priorRun(t, syncRepo, 9)

	runOnce(t, sched)

	assert.Len(t, notifier.getSent(), 1)
}

// TestScheduler_MultiAccount_IndependentSyncState verifies that two schedulers
// sharing one database but configured with distinct AccountIDs keep separate
// sync cursors and do not cross-contaminate each other's state.
func TestScheduler_MultiAccount_IndependentSyncState(t *testing.T) {
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)
	classRepo := repo.NewClassificationRepo(sqlDB)
	senderRepo := repo.NewSenderRepo(sqlDB)
	domainRepo := repo.NewDomainRepo(sqlDB)
	filter := importance.NewFilter(senderRepo, domainRepo)

	newSched := func(accountID string, provider email.Provider, notifier *mockNotifier) *Scheduler {
		return New(Config{
			AccountID:          accountID,
			PollInterval:       time.Hour,
			MinImportance:      domain.LevelIgnore,
			EmailRepo:          emailRepo,
			SyncRepo:           syncRepo,
			ClassificationRepo: classRepo,
			Filter:             filter,
			Provider:           provider,
			Notifier:           notifier,
			Logger:             log.Noop{},
		})
	}

	notifierA := &mockNotifier{}
	notifierB := &mockNotifier{}
	provA := &mockProvider{messages: []email.Message{
		{UID: 100, Subject: "A1", FromEmail: "x@a.com", Date: time.Now()},
	}}
	provB := &mockProvider{messages: []email.Message{
		{UID: 5, Subject: "B1", FromEmail: "y@b.com", Date: time.Now()},
	}}

	schedA := newSched("a@example.com", provA, notifierA)
	schedB := newSched("b@example.com", provB, notifierB)

	// Seed distinct prior cursors so the two accounts have unrelated state.
	require.NoError(t, syncRepo.Upsert(context.Background(),
		domain.SyncState{AccountID: "a@example.com", LastUID: 99, SyncedAt: time.Now()}))
	require.NoError(t, syncRepo.Upsert(context.Background(),
		domain.SyncState{AccountID: "b@example.com", LastUID: 4, SyncedAt: time.Now()}))

	runOnce(t, schedA)
	runOnce(t, schedB)

	// Each provider was asked to fetch from its own account's cursor.
	assert.Equal(t, uint32(99), provA.lastUID)
	assert.Equal(t, uint32(4), provB.lastUID)

	// Each account's cursor advanced independently to its own highest UID.
	stateA, err := syncRepo.Get(context.Background(), "a@example.com")
	require.NoError(t, err)
	require.NotNil(t, stateA)
	assert.Equal(t, uint32(100), stateA.LastUID)

	stateB, err := syncRepo.Get(context.Background(), "b@example.com")
	require.NoError(t, err)
	require.NotNil(t, stateB)
	assert.Equal(t, uint32(5), stateB.LastUID)

	assert.Len(t, notifierA.getSent(), 1)
	assert.Len(t, notifierB.getSent(), 1)
}

package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
)

const backfillAcct = "test@example.com"

func allowRule(sender string) domain.FilterRule {
	return domain.FilterRule{
		ID: "allow-" + sender, AccountID: backfillAcct, Action: domain.RuleActionAllow,
		Type: domain.RuleTypeSender, Value: sender, Enabled: true,
	}
}

func TestScheduler_Backfill_DisabledByDefault(t *testing.T) {
	provider := &mockProvider{
		messages: []email.Message{{UID: 50, Subject: "x", FromEmail: "a@b.com", Date: time.Now()}}, // baseline
		unseen:   []email.Message{{UID: 40, Subject: "u", FromEmail: "c@d.com", Date: time.Now()}},
		fetchErr: errors.New("FetchSince must not run on first run"),
	}
	notifier := &mockNotifier{}
	sched, _, syncRepo := newTestScheduler(t, provider, notifier) // BackfillWindow defaults to 0

	runOnce(t, sched)

	assert.Equal(t, 0, provider.unseenLimit, "FetchUnseenSince not called when backfill is disabled")
	assert.Empty(t, notifier.getSent())
	state, err := syncRepo.Get(context.Background(), backfillAcct)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, uint32(50), state.LastUID, "baseline still set from LatestUID")
}

func TestScheduler_Backfill_ProcessesRecentUnread(t *testing.T) {
	unread := email.Message{UID: 40, Subject: "hi", FromEmail: "vip@shop.com", Date: time.Now()}
	provider := &mockProvider{
		messages: []email.Message{{UID: 50, Subject: "x", FromEmail: "a@b.com", Date: time.Now()}}, // baseline 50
		unseen:   []email.Message{unread},
		fetchErr: errors.New("FetchSince must not run on first run"),
	}
	notifier := &mockNotifier{}
	sched, emailRepo, syncRepo := newTestSchedulerWithLevel(t, provider, notifier, domain.LevelImportant)
	sched.cfg.BackfillWindow = 48 * time.Hour
	require.NoError(t, sched.cfg.RuleRepo.Add(context.Background(), allowRule("vip@shop.com")))

	runOnce(t, sched)

	require.Len(t, notifier.getSent(), 1, "important (allow-rule) unread is notified on first run")
	e, err := emailRepo.GetByAccountAndUID(context.Background(), backfillAcct, 40)
	require.NoError(t, err)
	require.NotNil(t, e, "backfilled unread is ingested")

	state, err := syncRepo.Get(context.Background(), backfillAcct)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, uint32(50), state.LastUID, "baseline from LatestUID, not the backfilled UID")

	assert.Equal(t, maxBackfillMessages, provider.unseenLimit, "backfill is capped")
	assert.WithinDuration(t, time.Now().Add(-48*time.Hour), provider.unseenSince, time.Minute)
}

func TestScheduler_Backfill_ClampsWindowToOneWeek(t *testing.T) {
	provider := &mockProvider{
		messages: []email.Message{{UID: 50, Subject: "x", FromEmail: "a@b.com", Date: time.Now()}},
	}
	sched, _, _ := newTestScheduler(t, provider, &mockNotifier{})
	sched.cfg.BackfillWindow = 30 * 24 * time.Hour // over the max

	runOnce(t, sched)

	assert.WithinDuration(t, time.Now().Add(-maxBackfillWindow), provider.unseenSince, time.Minute,
		"window is clamped to one week")
}

func TestScheduler_Backfill_SkipsAlreadyIngested(t *testing.T) {
	unread := email.Message{UID: 40, Subject: "hi", FromEmail: "vip@shop.com", Date: time.Now()}
	provider := &mockProvider{
		messages: []email.Message{{UID: 50, Subject: "x", FromEmail: "a@b.com", Date: time.Now()}},
		unseen:   []email.Message{unread},
	}
	notifier := &mockNotifier{}
	sched, emailRepo, _ := newTestSchedulerWithLevel(t, provider, notifier, domain.LevelImportant)
	sched.cfg.BackfillWindow = 48 * time.Hour
	require.NoError(t, sched.cfg.RuleRepo.Add(context.Background(), allowRule("vip@shop.com")))

	// Simulate a prior interrupted backfill that already ingested UID 40.
	require.NoError(t, emailRepo.Upsert(context.Background(), domain.Email{
		ID: "pre", AccountID: backfillAcct, MessageUID: 40, Subject: "hi",
		FromEmail: "vip@shop.com", Status: domain.StatusNew, Date: time.Now(), ReceivedAt: time.Now(),
	}))

	runOnce(t, sched)

	assert.Empty(t, notifier.getSent(), "already-ingested unread is not reprocessed or re-notified")
}

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/filter"
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

const ruleAcct = "test@example.com"

// buildRuleScheduler returns a scheduler plus the handles needed to seed rules
// and inspect the resulting email decisions.
func buildRuleScheduler(
	t *testing.T, msg email.Message, llmProvider llm.Provider, notifier *mockNotifier,
) (*Scheduler, *repo.EmailRepo, *repo.RuleRepo, *repo.SyncStateRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)
	ruleRepo := repo.NewRuleRepo(sqlDB)
	importanceFilter := importance.NewFilter(repo.NewSenderRepo(sqlDB), repo.NewDomainRepo(sqlDB))

	sched := New(Config{
		AccountID:          ruleAcct,
		PollInterval:       time.Hour,
		MinImportance:      domain.LevelImportant,
		EmailRepo:          emailRepo,
		SyncRepo:           syncRepo,
		ClassificationRepo: repo.NewClassificationRepo(sqlDB),
		Filter:             importanceFilter,
		LLMProvider:        llmProvider,
		Provider:           &mockProvider{messages: []email.Message{msg}},
		Notifier:           notifier,
		Logger:             log.Noop{},
		RuleRepo:           ruleRepo,
		ClauseRepo:         repo.NewClauseRepo(sqlDB),
		RuleEngine:         filter.NewEngine(),
		BaselineFloor:      domain.LevelMaybe,
	})
	return sched, emailRepo, ruleRepo, syncRepo
}

func runRulePoll(t *testing.T, sched *Scheduler, syncRepo *repo.SyncStateRepo, lastUID uint32) {
	t.Helper()
	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: ruleAcct, LastUID: lastUID, SyncedAt: time.Now(),
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, sched.poll(ctx))
}

func TestScheduler_IgnoreRule_SkipsLLM_SetsProvenance(t *testing.T) {
	msg := email.Message{UID: 10, Subject: "urgent meeting scheduled", FromEmail: "boss@work.com", Date: time.Now()}
	// LLM would say important if consulted; the ignore rule must skip it.
	mockLLM := &mockLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 90}}
	notifier := &mockNotifier{}
	sched, emailRepo, ruleRepo, syncRepo := buildRuleScheduler(t, msg, mockLLM, notifier)

	require.NoError(t, ruleRepo.Add(context.Background(), domain.FilterRule{
		ID: "r1", AccountID: ruleAcct, Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeSender, Value: "boss@work.com", Enabled: true,
	}))

	runRulePoll(t, sched, syncRepo, 9)

	assert.Empty(t, notifier.getSent())
	assert.Equal(t, 0, mockLLM.getCalls(), "ignore rule must skip the LLM")
	e, err := emailRepo.GetByAccountAndUID(context.Background(), ruleAcct, 10)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIgnored, e.Status)
	assert.Equal(t, "rule:r1", e.DecidedBy)
}

func TestScheduler_AllowRule_ForcesNotify(t *testing.T) {
	// A newsletter that the baseline would drop, rescued by an allow rule.
	msg := email.Message{
		UID: 10, Subject: "Big sale!", FromEmail: "vip@shop.com", Date: time.Now(),
		ListUnsubscribe: "<unsub@shop.com>",
	}
	notifier := &mockNotifier{}
	sched, emailRepo, ruleRepo, syncRepo := buildRuleScheduler(t, msg, nil, notifier)

	require.NoError(t, ruleRepo.Add(context.Background(), domain.FilterRule{
		ID: "a1", AccountID: ruleAcct, Action: domain.RuleActionAllow,
		Type: domain.RuleTypeSender, Value: "vip@shop.com", Enabled: true,
	}))

	runRulePoll(t, sched, syncRepo, 9)

	require.Len(t, notifier.getSent(), 1, "allow rule forces a notification")
	e, err := emailRepo.GetByAccountAndUID(context.Background(), ruleAcct, 10)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusNotified, e.Status)
	assert.Empty(t, e.DecidedBy)
}

func TestScheduler_BaselineFloor_SetsProvenance(t *testing.T) {
	msg := email.Message{
		UID: 10, Subject: "Big sale!", FromEmail: "news@shop.com", Date: time.Now(),
		ListUnsubscribe: "<unsub@shop.com>",
	}
	notifier := &mockNotifier{}
	sched, emailRepo, _, syncRepo := buildRuleScheduler(t, msg, nil, notifier)

	runRulePoll(t, sched, syncRepo, 9)

	assert.Empty(t, notifier.getSent())
	e, err := emailRepo.GetByAccountAndUID(context.Background(), ruleAcct, 10)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIgnored, e.Status)
	assert.Equal(t, "baseline", e.DecidedBy)
}

func TestScheduler_LLMLow_SetsProvenance(t *testing.T) {
	msg := email.Message{UID: 10, Subject: "urgent meeting scheduled", FromEmail: "boss@work.com", Date: time.Now()}
	mockLLM := &mockLLMProvider{result: llm.ClassifyResult{Level: domain.LevelIgnore, Score: 5}}
	notifier := &mockNotifier{}
	sched, emailRepo, _, syncRepo := buildRuleScheduler(t, msg, mockLLM, notifier)

	runRulePoll(t, sched, syncRepo, 9)

	assert.Empty(t, notifier.getSent())
	assert.Equal(t, 1, mockLLM.getCalls())
	e, err := emailRepo.GetByAccountAndUID(context.Background(), ruleAcct, 10)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIgnored, e.Status)
	assert.Equal(t, "llm:low", e.DecidedBy)
}

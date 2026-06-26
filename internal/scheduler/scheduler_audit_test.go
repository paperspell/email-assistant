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

// capturingLLMProvider records every ClassifyRequest it receives.
type capturingLLMProvider struct {
	requests []llm.ClassifyRequest
	result   llm.ClassifyResult
}

func (c *capturingLLMProvider) Name() string { return "mock" }

func (c *capturingLLMProvider) Classify(_ context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error) {
	c.requests = append(c.requests, req)
	return c.result, nil
}

func (c *capturingLLMProvider) lastRequest() llm.ClassifyRequest {
	if len(c.requests) == 0 {
		return llm.ClassifyRequest{}
	}
	return c.requests[len(c.requests)-1]
}

// buildContentModeScheduler creates a Scheduler for testing body content mode
// behavior. AuditRepo is set to nil so only the body handling is exercised.
func buildContentModeScheduler(
	t *testing.T,
	msg email.Message,
	llmMock *capturingLLMProvider,
	contentMode string,
) (*Scheduler, *repo.SyncStateRepo) {
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
	importanceFilter := importance.NewFilter(senderRepo, domainRepo)

	sched := New(Config{
		AccountID:          "test@example.com",
		PollInterval:       time.Hour,
		MinImportance:      domain.LevelIgnore,
		EmailRepo:          emailRepo,
		SyncRepo:           syncRepo,
		ClassificationRepo: classRepo,
		Filter:             importanceFilter,
		LLMProvider:        llmMock,
		ContentMode:        contentMode,
		Provider:           &mockProvider{messages: []email.Message{msg}},
		Notifier:           &mockNotifier{},
		Logger:             log.Noop{},
		RuleRepo:           repo.NewRuleRepo(sqlDB),
		ClauseRepo:         repo.NewClauseRepo(sqlDB),
		RuleEngine:         filter.NewEngine(),
		BaselineFloor:      domain.LevelIgnore,
	})
	return sched, syncRepo
}

func pollOnce(t *testing.T, sched *Scheduler, syncRepo *repo.SyncStateRepo, lastUID uint32) {
	t.Helper()
	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com", LastUID: lastUID, SyncedAt: time.Now(),
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, sched.poll(ctx))
}

// TestScheduler_RedactedBody_BodyIsRedactedBeforeLLM verifies that PII in the
// body is replaced with placeholders when content_mode is redacted_body.
func TestScheduler_RedactedBody_BodyIsRedactedBeforeLLM(t *testing.T) {
	msg := email.Message{
		UID:       20,
		Subject:   "Invoice",
		FromEmail: "billing@company.com",
		Date:      time.Now(),
		Body:      "Please pay to IBAN DE89 3704 0044 0532 0130 00 or email john@example.com",
	}
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, msg, llmMock, "redacted_body")
	pollOnce(t, sched, syncRepo, 19)

	req := llmMock.lastRequest()
	assert.NotContains(t, req.Body, "john@example.com")
	assert.Contains(t, req.Body, "[EMAIL]")
	assert.NotContains(t, req.Body, "DE89")
	assert.Contains(t, req.Body, "[FINANCIAL]")
}

// TestScheduler_FullBody_BodySentVerbatim verifies that the body is not
// modified when content_mode is full_body.
func TestScheduler_FullBody_BodySentVerbatim(t *testing.T) {
	originalBody := "Please pay to IBAN DE89 3704 0044 0532 0130 00 or email john@example.com"
	msg := email.Message{
		UID:       21,
		Subject:   "Invoice",
		FromEmail: "billing@company.com",
		Date:      time.Now(),
		Body:      originalBody,
	}
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, msg, llmMock, "full_body")
	pollOnce(t, sched, syncRepo, 20)

	assert.Equal(t, originalBody, llmMock.lastRequest().Body)
}

// TestScheduler_HeadersOnly_BodyNotSentToLLM verifies that the body field is
// empty in the LLM request when content_mode is headers_only.
func TestScheduler_HeadersOnly_BodyNotSentToLLM(t *testing.T) {
	msg := email.Message{
		UID:       22,
		Subject:   "urgent meeting",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
		Body:      "Call me at +48 600 123 456",
	}
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, msg, llmMock, "headers_only")
	pollOnce(t, sched, syncRepo, 21)

	assert.Empty(t, llmMock.lastRequest().Body)
}

// TestScheduler_AuditEntryWrittenAfterLLMCall verifies that one row is inserted
// into llm_audit_log after a successful LLM classification.
func TestScheduler_AuditEntryWrittenAfterLLMCall(t *testing.T) {
	msg := email.Message{
		UID:       30,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	notifier := &mockNotifier{}

	// All repos share one DB so the email FK in llm_audit_log is satisfied.
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)
	classRepo := repo.NewClassificationRepo(sqlDB)
	auditRepo := repo.NewAuditRepo(sqlDB)
	senderRepo := repo.NewSenderRepo(sqlDB)
	domainRepo := repo.NewDomainRepo(sqlDB)
	importanceFilter := importance.NewFilter(senderRepo, domainRepo)

	sched := New(Config{
		AccountID:          "test@example.com",
		PollInterval:       time.Hour,
		MinImportance:      domain.LevelIgnore,
		EmailRepo:          emailRepo,
		SyncRepo:           syncRepo,
		ClassificationRepo: classRepo,
		AuditRepo:          auditRepo,
		Filter:             importanceFilter,
		LLMProvider:        llmMock,
		ContentMode:        "headers_only",
		Provider:           &mockProvider{messages: []email.Message{msg}},
		Notifier:           notifier,
		Logger:             log.Noop{},
		RuleRepo:           repo.NewRuleRepo(sqlDB),
		ClauseRepo:         repo.NewClauseRepo(sqlDB),
		RuleEngine:         filter.NewEngine(),
		BaselineFloor:      domain.LevelIgnore,
	})

	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com", LastUID: 29, SyncedAt: time.Now(),
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, sched.poll(ctx))

	var count int
	row := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_audit_log`)
	require.NoError(t, row.Scan(&count))
	assert.Equal(t, 1, count)
}

// TestScheduler_AuditWriteFailure_DoesNotAbortProcessing verifies that a
// failing audit write does not prevent the notification from being sent.
func TestScheduler_AuditWriteFailure_DoesNotAbortProcessing(t *testing.T) {
	msg := email.Message{
		UID:       31,
		Subject:   "urgent meeting scheduled",
		FromEmail: "boss@work.com",
		Date:      time.Now(),
	}
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	notifier := &mockNotifier{}

	// Open audit DB and immediately close it so Save fails.
	auditSQLDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), auditSQLDB))
	auditRepo := repo.NewAuditRepo(auditSQLDB)
	auditSQLDB.Close() // causes future saves to return an error

	mainSQLDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = mainSQLDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), mainSQLDB))

	emailRepo := repo.NewEmailRepo(mainSQLDB)
	syncRepo := repo.NewSyncStateRepo(mainSQLDB)
	classRepo := repo.NewClassificationRepo(mainSQLDB)
	senderRepo := repo.NewSenderRepo(mainSQLDB)
	domainRepo := repo.NewDomainRepo(mainSQLDB)
	importanceFilter := importance.NewFilter(senderRepo, domainRepo)

	sched := New(Config{
		AccountID:          "test@example.com",
		PollInterval:       time.Hour,
		MinImportance:      domain.LevelIgnore,
		EmailRepo:          emailRepo,
		SyncRepo:           syncRepo,
		ClassificationRepo: classRepo,
		AuditRepo:          auditRepo,
		Filter:             importanceFilter,
		LLMProvider:        llmMock,
		ContentMode:        "headers_only",
		Provider:           &mockProvider{messages: []email.Message{msg}},
		Notifier:           notifier,
		Logger:             log.Noop{},
		RuleRepo:           repo.NewRuleRepo(mainSQLDB),
		ClauseRepo:         repo.NewClauseRepo(mainSQLDB),
		RuleEngine:         filter.NewEngine(),
		BaselineFloor:      domain.LevelIgnore,
	})

	require.NoError(t, syncRepo.Upsert(context.Background(), domain.SyncState{
		AccountID: "test@example.com", LastUID: 30, SyncedAt: time.Now(),
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, sched.poll(ctx))

	assert.Len(t, notifier.getSent(), 1, "notification must be sent even when audit write fails")
}

package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/features"
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/pkg/timex"
	"github.com/paperspell/email-assistant/internal/telegram"
)

// Config holds all dependencies for the Scheduler.
type Config struct {
	AccountID           string
	PollInterval        time.Duration
	MinImportance       domain.ImportanceLevel
	EmailRepo           *repo.EmailRepo
	SyncRepo            *repo.SyncStateRepo
	ClassificationRepo  *repo.ClassificationRepo
	Filter              *importance.Filter
	LLMProvider         llm.Provider // nil when LLM is disabled
	ScoreDivergenceWarn int
	Provider            email.Provider
	Notifier            telegram.Notifier
	Logger              log.Logger
}

// Scheduler polls an IMAP account and sends Telegram notifications for new emails.
type Scheduler struct {
	cfg    Config
	stopCh chan struct{}
}

// New creates a Scheduler. Call Start to begin polling.
func New(cfg Config) *Scheduler {
	return &Scheduler{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Start begins the polling loop. It blocks until ctx is cancelled or Stop is called.
func (s *Scheduler) Start(ctx context.Context) error {
	s.cfg.Logger.Info("scheduler starting", "account_id", s.cfg.AccountID, "poll_interval", s.cfg.PollInterval)

	if err := s.cfg.Provider.Connect(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	s.pollWithBackoff(ctx)

	for {
		select {
		case <-ticker.C:
			s.pollWithBackoff(ctx)
		case <-s.stopCh:
			s.cfg.Logger.Info("scheduler stopped", "account_id", s.cfg.AccountID)
			return nil
		case <-ctx.Done():
			s.cfg.Logger.Info("scheduler context cancelled", "account_id", s.cfg.AccountID)
			return nil
		}
	}
}

// Stop signals the scheduler to stop after the current poll completes.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) pollWithBackoff(ctx context.Context) {
	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 2 * time.Minute

	err := backoff.Retry(func() error {
		return s.poll(ctx)
	}, backoff.WithContext(bo, ctx))

	if err != nil {
		s.cfg.Logger.Error(err, "account_id", s.cfg.AccountID)
	}
}

func (s *Scheduler) poll(ctx context.Context) error {
	state, err := s.cfg.SyncRepo.Get(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}

	var lastUID uint32
	if state != nil {
		lastUID = state.LastUID
	}

	messages, err := s.cfg.Provider.FetchSince(ctx, lastUID)
	if err != nil {
		return err
	}

	// First run: no sync state exists yet. Record the current highest UID and
	// skip processing — only emails arriving after this first poll will notify.
	if state == nil {
		maxUID := lastUID
		for _, msg := range messages {
			if msg.UID > maxUID {
				maxUID = msg.UID
			}
		}
		if maxUID > 0 {
			s.cfg.Logger.Info("first run: skipping existing emails",
				"account_id", s.cfg.AccountID,
				"existing_count", len(messages),
				"starting_from_uid", maxUID+1,
			)
			return s.cfg.SyncRepo.Upsert(ctx, domain.SyncState{
				AccountID: s.cfg.AccountID,
				LastUID:   maxUID,
				SyncedAt:  timex.NowUTC(),
			})
		}
		return nil
	}

	s.cfg.Logger.Debug("poll completed", "account_id", s.cfg.AccountID, "new_messages", len(messages))

	var maxUID = lastUID
	for _, msg := range messages {
		if err := s.processMessage(ctx, msg); err != nil {
			s.cfg.Logger.Error(err, "account_id", s.cfg.AccountID, "uid", msg.UID)
			continue
		}
		if msg.UID > maxUID {
			maxUID = msg.UID
		}
	}

	if maxUID > lastUID {
		if err := s.cfg.SyncRepo.Upsert(ctx, domain.SyncState{
			AccountID: s.cfg.AccountID,
			LastUID:   maxUID,
			SyncedAt:  timex.NowUTC(),
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Scheduler) processMessage(ctx context.Context, msg email.Message) error {
	lang := features.DetectLanguage(msg.Subject)

	e := domain.Email{
		ID:         idx.GenerateID(),
		AccountID:  s.cfg.AccountID,
		MessageUID: msg.UID,
		Subject:    msg.Subject,
		FromEmail:  msg.FromEmail,
		FromName:   msg.FromName,
		Date:       msg.Date,
		Status:     domain.StatusNew,
		ReceivedAt: timex.NowUTC(),
		Language:   lang,
	}

	if err := s.cfg.EmailRepo.Upsert(ctx, e); err != nil {
		return err
	}

	// Rule-based classification
	ruleClass, err := s.cfg.Filter.Classify(ctx, e.ID, msg)
	if err != nil {
		return err
	}
	if err := s.cfg.ClassificationRepo.Save(ctx, ruleClass); err != nil {
		return err
	}

	// Rule-based is the gate: if it says ignore, skip LLM entirely
	if !s.shouldNotify(ruleClass.Level) {
		s.cfg.Logger.Info("email ignored",
			"account_id", s.cfg.AccountID,
			"uid", msg.UID,
			"from", msg.FromEmail,
			"subject", msg.Subject,
			"level", string(ruleClass.Level),
			"score", ruleClass.Score,
			"category", string(ruleClass.Category),
			"reason", strings.Join(ruleClass.Reason, "; "),
		)
		return s.cfg.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusIgnored)
	}

	// LLM classification (optional — non-fatal on error)
	classification := ruleClass
	if s.cfg.LLMProvider != nil {
		req := buildLLMRequest(msg, lang)
		llmResult, llmErr := s.cfg.LLMProvider.Classify(ctx, req)
		if llmErr != nil {
			s.cfg.Logger.Warn(fmt.Errorf("llm classify: %w", llmErr),
				"account_id", s.cfg.AccountID, "uid", msg.UID)
		} else {
			llmClass := domain.Classification{
				ID:           idx.GenerateID(),
				EmailID:      e.ID,
				Level:        llmResult.Level,
				Category:     llmResult.Category,
				Score:        llmResult.Score,
				Reason:       llmResult.Reasons,
				Summary:      llmResult.Summary,
				ClassifiedAt: timex.NowUTC(),
				Source:       domain.SourceLLM + ":" + s.cfg.LLMProvider.Name(),
			}
			if err := s.cfg.ClassificationRepo.Save(ctx, llmClass); err != nil {
				return err
			}
			s.logDivergence(ruleClass, llmClass)
			classification = llmClass
		}
	}

	// Final notification decision uses the LLM result (or rule-based if LLM was skipped/failed)
	if !s.shouldNotify(classification.Level) {
		s.cfg.Logger.Info("email ignored after llm",
			"account_id", s.cfg.AccountID,
			"uid", msg.UID,
			"from", msg.FromEmail,
			"subject", msg.Subject,
			"level", string(classification.Level),
			"score", classification.Score,
			"summary", classification.Summary,
		)
		return s.cfg.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusIgnored)
	}

	tgMsgID, err := s.cfg.Notifier.SendNewEmail(ctx, e, classification)
	if err != nil {
		return err
	}

	if err := s.cfg.EmailRepo.SetTelegramMessageID(ctx, e.ID, tgMsgID); err != nil {
		return err
	}

	if err := s.cfg.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusNotified); err != nil {
		return err
	}

	s.cfg.Logger.Info("notified",
		"account_id", s.cfg.AccountID,
		"uid", msg.UID,
		"from", msg.FromEmail,
		"subject", msg.Subject,
		"level", string(classification.Level),
		"score", classification.Score,
		"category", string(classification.Category),
		"source", classification.Source,
		"summary", classification.Summary,
	)

	return nil
}

func (s *Scheduler) shouldNotify(level domain.ImportanceLevel) bool {
	order := map[domain.ImportanceLevel]int{
		domain.LevelIgnore:    0,
		domain.LevelMaybe:     1,
		domain.LevelImportant: 2,
		domain.LevelCritical:  3,
	}
	threshold := order[s.cfg.MinImportance]
	got := order[level]
	return got >= threshold
}

func (s *Scheduler) logDivergence(rule, llmClass domain.Classification) {
	threshold := s.cfg.ScoreDivergenceWarn
	if threshold <= 0 {
		threshold = 30
	}
	diff := rule.Score - llmClass.Score
	if diff < 0 {
		diff = -diff
	}
	if diff >= threshold {
		s.cfg.Logger.Warn(
			fmt.Errorf("classification divergence: rule=%d llm=%d diff=%d", rule.Score, llmClass.Score, diff),
			"email_id", rule.EmailID,
			"rule_score", rule.Score,
			"rule_level", string(rule.Level),
			"llm_score", llmClass.Score,
			"llm_level", string(llmClass.Level),
			"provider", s.cfg.LLMProvider.Name(),
		)
	}
}

func buildLLMRequest(msg email.Message, lang string) llm.ClassifyRequest {
	return llm.ClassifyRequest{
		FromEmail:          msg.FromEmail,
		FromName:           msg.FromName,
		Subject:            msg.Subject,
		Body:               msg.Body,
		Language:           lang,
		IsReply:            msg.InReplyTo != "",
		HasListUnsubscribe: msg.ListUnsubscribe != "",
	}
}

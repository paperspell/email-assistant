package scheduler

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/paperspell/email-assistant/internal/auth/oauth"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/features"
	"github.com/paperspell/email-assistant/internal/filter"
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/pkg/timex"
	"github.com/paperspell/email-assistant/internal/privacy"
	"github.com/paperspell/email-assistant/internal/telegram"
)

// Config holds all dependencies for the Scheduler.
type Config struct {
	AccountID           string
	AccountName         string
	AccountEmail        string
	PollInterval        time.Duration
	MinImportance       domain.ImportanceLevel
	EmailRepo           *repo.EmailRepo
	SyncRepo            *repo.SyncStateRepo
	ClassificationRepo  *repo.ClassificationRepo
	AuditRepo           *repo.AuditRepo
	Filter              *importance.Filter
	LLMProvider         llm.Provider // nil when LLM is disabled
	ContentMode         string
	ScoreDivergenceWarn int
	Provider            email.Provider
	Notifier            telegram.Notifier
	Alerter             Alerter // operational alerts (e.g. re-auth needed); nil disables them
	Logger              log.Logger

	// Mechanical filtering layer (Stage 9).
	RuleRepo      *repo.RuleRepo
	ClauseRepo    *repo.ClauseRepo
	RuleEngine    filter.Engine
	BaselineFloor domain.ImportanceLevel // baseline level at/below which the LLM is skipped

	// BackfillWindow enables the first-run backfill: unread mail received within
	// this window is processed on the first poll. 0 disables it. Values above
	// maxBackfillWindow are clamped.
	BackfillWindow time.Duration
}

const (
	// maxBackfillWindow bounds the first-run backfill lookback to one week.
	maxBackfillWindow = 7 * 24 * time.Hour
	// maxBackfillMessages caps how many unread messages the first-run backfill
	// processes, so a busy mailbox cannot trigger a flood of LLM calls/notifications.
	maxBackfillMessages = 200
)

// Alerter delivers operational alerts to the user, distinct from new-email
// notifications — for example, an account whose OAuth token needs re-authorization.
type Alerter interface {
	SendAlert(ctx context.Context, text string) error
}

// Scheduler polls an IMAP account and sends Telegram notifications for new emails.
type Scheduler struct {
	cfg    Config
	stopCh chan struct{}
	// reauthAlerted is true once a re-authorization alert has been sent for the
	// current outage, so the alert fires once rather than every poll. It resets
	// after a successful poll. Accessed only from the single polling goroutine.
	reauthAlerted bool
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
		if oauth.IsReauthRequired(err) {
			// One expired mailbox must not take down the whole daemon (and, under a
			// restart-looping service, spam the alert). Notify once, then stop polling
			// this account until it is re-authorized and the daemon is restarted.
			s.cfg.Logger.Error(err, "account_id", s.cfg.AccountID)
			s.alertReauthIfNeeded(ctx, err)
			return nil
		}
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

	notify := func(err error, d time.Duration) {
		// Warn, not debug: a failing poll leaves the sync watermark unwritten, so
		// the next cycle replays the batch. At debug level that stays invisible on
		// a default install and the replay looks like the daemon misbehaving.
		s.cfg.Logger.Warn(fmt.Errorf("poll error, retrying: %w", err),
			"account_id", s.cfg.AccountID, "retry_in", d)
	}

	err := backoff.RetryNotify(func() error {
		return s.poll(ctx)
	}, backoff.WithContext(bo, ctx), notify)

	if err != nil {
		s.cfg.Logger.Error(err, "account_id", s.cfg.AccountID)
		s.alertReauthIfNeeded(ctx, err)
		return
	}
	// A successful poll clears the re-auth state so a future expiry alerts again.
	s.reauthAlerted = false
}

// alertReauthIfNeeded sends a one-time Telegram alert when err indicates the
// account's OAuth refresh token was rejected and the user must re-authorize.
// It de-duplicates via reauthAlerted so the alert fires once per outage rather
// than on every poll.
func (s *Scheduler) alertReauthIfNeeded(ctx context.Context, err error) {
	if err == nil || !oauth.IsReauthRequired(err) || s.reauthAlerted || s.cfg.Alerter == nil {
		return
	}
	if aerr := s.cfg.Alerter.SendAlert(ctx, s.reauthAlertText()); aerr != nil {
		s.cfg.Logger.Error(fmt.Errorf("send re-auth alert: %w", aerr), "account_id", s.cfg.AccountID)
		return
	}
	s.reauthAlerted = true
	s.cfg.Logger.Info("sent re-auth alert to Telegram", "account_id", s.cfg.AccountID)
}

// reauthAlertText builds the HTML-formatted re-authorization instructions.
func (s *Scheduler) reauthAlertText() string {
	label := s.cfg.AccountEmail
	if s.cfg.AccountName != "" && s.cfg.AccountName != s.cfg.AccountEmail {
		label = fmt.Sprintf("%s (%s)", s.cfg.AccountName, s.cfg.AccountEmail)
	}
	email := html.EscapeString(s.cfg.AccountEmail)
	return "⚠️ <b>Email authorization expired</b>\n\n" +
		"Account: " + html.EscapeString(label) + "\n\n" +
		"The Google sign-in for this mailbox has expired, so I can no longer check it. " +
		"New emails from this account won't be delivered until you re-authorize.\n\n" +
		"<b>To fix, on the machine running email-agent:</b>\n" +
		"1. Run <code>email-agent account edit " + email + "</code>\n" +
		"2. Answer <b>y</b> to \"Re-authorize with Google now?\" and complete the Google consent in the browser.\n" +
		"3. Restart the daemon: <code>email-agent run</code>\n\n" +
		"<i>Gmail in \"Testing\" mode expires this roughly every 7 days.</i>"
}

func (s *Scheduler) poll(ctx context.Context) error {
	state, err := s.cfg.SyncRepo.Get(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}

	// First run: no sync state exists yet. Establish the baseline from the
	// mailbox's current highest UID without downloading any existing mail (a full
	// FetchSince here would fetch every message — and every body — only to discard
	// them). Only emails arriving after this baseline are processed on later polls.
	if state == nil {
		baseline, err := s.cfg.Provider.LatestUID(ctx)
		if err != nil {
			return err
		}
		if baseline == 0 {
			// Empty mailbox: nothing to baseline yet, stay in first-run state.
			return nil
		}
		// Process recent unread mail before recording the baseline. On failure we
		// return without upserting so the next start retries; processMessage is
		// idempotent via the (account, uid) upsert + the existence check below.
		if err := s.backfillFirstRun(ctx); err != nil {
			return err
		}
		s.cfg.Logger.Info("first run: baseline set, skipping existing emails",
			"account_id", s.cfg.AccountID, "baseline_uid", baseline, "starting_from_uid", baseline+1)
		return s.cfg.SyncRepo.Upsert(ctx, domain.SyncState{
			AccountID: s.cfg.AccountID,
			LastUID:   baseline,
			SyncedAt:  timex.NowUTC(),
		})
	}

	lastUID := state.LastUID
	messages, err := s.cfg.Provider.FetchSince(ctx, lastUID)
	if err != nil {
		return err
	}

	s.cfg.Logger.Debug("poll completed", "account_id", s.cfg.AccountID, "new_messages", len(messages))

	// Load the account's enabled rules and active ignore clauses once per poll, so
	// CLI/Telegram edits take effect on the next cycle.
	rules, err := s.cfg.RuleRepo.ListEnabled(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}
	clauseTexts, err := s.cfg.ClauseRepo.ActiveTexts(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}

	var maxUID = lastUID
	for _, msg := range messages {
		if err := s.processMessage(ctx, msg, rules, clauseTexts); err != nil {
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

// backfillFirstRun processes unread mail received within the account's backfill
// window on the first run: important messages notify immediately, unimportant
// ones fall through to the digest (both via processMessage's normal routing).
// Bounded by maxBackfillWindow and maxBackfillMessages. It skips messages already
// ingested so a restart mid-backfill does not re-notify.
func (s *Scheduler) backfillFirstRun(ctx context.Context) error {
	if s.cfg.BackfillWindow <= 0 {
		return nil
	}
	window := s.cfg.BackfillWindow
	if window > maxBackfillWindow {
		window = maxBackfillWindow
	}
	since := timex.NowUTC().Add(-window)

	msgs, err := s.cfg.Provider.FetchUnseenSince(ctx, since, maxBackfillMessages)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	rules, err := s.cfg.RuleRepo.ListEnabled(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}
	clauseTexts, err := s.cfg.ClauseRepo.ActiveTexts(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}

	processed := 0
	for _, msg := range msgs {
		existing, err := s.cfg.EmailRepo.GetByAccountAndUID(ctx, s.cfg.AccountID, msg.UID)
		if err != nil {
			return err
		}
		if existing != nil {
			continue // already ingested (e.g. a prior interrupted backfill)
		}
		if err := s.processMessage(ctx, msg, rules, clauseTexts); err != nil {
			s.cfg.Logger.Error(err, "account_id", s.cfg.AccountID, "uid", msg.UID)
			continue
		}
		processed++
	}
	s.cfg.Logger.Info("first run: backfilled recent unread",
		"account_id", s.cfg.AccountID, "window", window, "found", len(msgs), "processed", processed)
	return nil
}

func (s *Scheduler) processMessage(
	ctx context.Context, msg email.Message, rules []domain.FilterRule, clauseTexts []string,
) error {
	// Skip a message whose decision is already persisted. The sync watermark
	// cannot carry that on its own: it is written after the whole batch, so a
	// restart or a failure mid-poll replays everything processed since the last
	// write, and each replay used to send another notification.
	//
	// This narrows the window rather than closing it: a crash between a delivered
	// Telegram message and UpdateStatus leaves the row StatusNew, and the replay
	// notifies again. Closing that needs the send and the status write to share a
	// transaction, which the Telegram API cannot join.
	if existing, err := s.cfg.EmailRepo.GetByAccountAndUID(ctx, s.cfg.AccountID, msg.UID); err != nil {
		return err
	} else if existing != nil && existing.Status != domain.StatusNew {
		s.cfg.Logger.Debug("already processed, skipping",
			"account_id", s.cfg.AccountID, "uid", msg.UID, "status", string(existing.Status))
		return nil
	}

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
		ListID:     msg.ListID,
	}

	if err := s.cfg.EmailRepo.Upsert(ctx, &e); err != nil {
		return err
	}

	// 1. Explicit per-account rules (Tier-0): allow forces notify, ignore skips the LLM.
	if action, matched, ok := s.cfg.RuleEngine.Evaluate(rules, msg); ok {
		if action == domain.RuleActionAllow {
			return s.notifyAllowed(ctx, e, msg, matched)
		}
		return s.ignoreEmail(ctx, e, msg, "rule:"+matched.ID,
			"rule_type", matched.Type, "rule_value", matched.Value)
	}

	// 2. Baseline rule-based scoring (the cheap gate).
	ruleClass, err := s.cfg.Filter.Classify(ctx, s.cfg.AccountID, e.ID, msg)
	if err != nil {
		return err
	}
	if err := s.cfg.ClassificationRepo.Save(ctx, ruleClass); err != nil {
		return err
	}
	if levelRank(ruleClass.Level) < levelRank(s.cfg.BaselineFloor) {
		return s.ignoreEmail(ctx, e, msg, "baseline",
			"level", string(ruleClass.Level), "score", ruleClass.Score,
			"reason", strings.Join(ruleClass.Reason, "; "))
	}

	// 3. LLM classification (optional — non-fatal on error), with per-account ignore clauses.
	classification := ruleClass
	llmDecided := false
	if s.cfg.LLMProvider != nil {
		req := buildLLMRequest(msg, lang, s.cfg.ContentMode)
		req.IgnoreClauses = clauseTexts
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
			if s.cfg.AuditRepo != nil {
				auditErr := s.cfg.AuditRepo.Save(ctx, repo.AuditEntry{
					ID:          idx.GenerateID(),
					EmailID:     e.ID,
					Provider:    s.cfg.LLMProvider.Name(),
					Model:       llmClass.Source,
					ContentMode: s.cfg.ContentMode,
					BytesSent:   len(llm.FormatUserMessage(req)),
					CreatedAt:   timex.NowUTC(),
				})
				if auditErr != nil {
					s.cfg.Logger.Warn(fmt.Errorf("audit log: %w", auditErr),
						"account_id", s.cfg.AccountID, "uid", msg.UID)
				}
			}
			s.logDivergence(ruleClass, llmClass)
			classification = llmClass
			llmDecided = true
		}
	}

	// 4. Final notification decision uses the LLM result (or rule-based if LLM was skipped/failed).
	if !s.shouldNotify(classification.Level) {
		decidedBy := "baseline"
		if llmDecided {
			decidedBy = "llm:low"
		}
		return s.ignoreEmail(ctx, e, msg, decidedBy,
			"level", string(classification.Level), "score", classification.Score,
			"summary", classification.Summary)
	}

	return s.notify(ctx, e, msg, classification)
}

// notify sends a Telegram notification and marks the email notified.
func (s *Scheduler) notify(
	ctx context.Context, e domain.Email, msg email.Message, classification domain.Classification,
) error {
	tgMsgID, err := s.cfg.Notifier.SendNewEmail(ctx, e, classification, s.cfg.AccountName, s.cfg.AccountEmail)
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

// notifyAllowed forces an allow-rule match to be treated as important and notified.
func (s *Scheduler) notifyAllowed(
	ctx context.Context, e domain.Email, msg email.Message, rule *domain.FilterRule,
) error {
	c := domain.Classification{
		ID:           idx.GenerateID(),
		EmailID:      e.ID,
		Level:        domain.LevelImportant,
		Category:     domain.CategoryOther,
		Score:        100,
		Reason:       []string{"allow rule: " + rule.Type + "=" + rule.Value},
		ClassifiedAt: timex.NowUTC(),
		Source:       domain.SourceRuleBased,
	}
	if err := s.cfg.ClassificationRepo.Save(ctx, c); err != nil {
		return err
	}
	s.cfg.Logger.Info("allow rule matched",
		"account_id", s.cfg.AccountID, "uid", msg.UID,
		"rule_id", rule.ID, "rule_type", rule.Type, "rule_value", rule.Value)
	return s.notify(ctx, e, msg, c)
}

// ignoreEmail marks an email ignored and records its provenance (decided_by).
func (s *Scheduler) ignoreEmail(
	ctx context.Context, e domain.Email, msg email.Message, decidedBy string, kv ...any,
) error {
	fields := append([]any{
		"account_id", s.cfg.AccountID,
		"uid", msg.UID,
		"from", msg.FromEmail,
		"subject", msg.Subject,
		"decided_by", decidedBy,
	}, kv...)
	s.cfg.Logger.Info("email ignored", fields...)
	return s.cfg.EmailRepo.UpdateStatusDecidedBy(ctx, e.ID, domain.StatusIgnored, decidedBy)
}

// levelRank orders importance levels for threshold comparisons. Unknown levels
// (including the empty string) rank lowest.
func levelRank(level domain.ImportanceLevel) int {
	switch level {
	case domain.LevelMaybe:
		return 1
	case domain.LevelImportant:
		return 2
	case domain.LevelCritical:
		return 3
	default: // LevelIgnore and unknown
		return 0
	}
}

func (s *Scheduler) shouldNotify(level domain.ImportanceLevel) bool {
	return levelRank(level) >= levelRank(s.cfg.MinImportance)
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

func buildLLMRequest(msg email.Message, lang, contentMode string) llm.ClassifyRequest {
	var body string
	switch contentMode {
	case "redacted_body":
		body = privacy.Redact(msg.Body)
	case "full_body":
		body = msg.Body
	}
	return llm.ClassifyRequest{
		FromEmail:          msg.FromEmail,
		FromName:           msg.FromName,
		Subject:            msg.Subject,
		Body:               body,
		Language:           lang,
		IsReply:            msg.InReplyTo != "",
		HasListUnsubscribe: msg.ListUnsubscribe != "",
	}
}

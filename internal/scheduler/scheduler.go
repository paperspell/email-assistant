package scheduler

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/features"
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/pkg/timex"
	"github.com/paperspell/email-assistant/internal/telegram"
)

// Config holds all dependencies for the Scheduler.
type Config struct {
	AccountID          string
	PollInterval       time.Duration
	MinImportance      domain.ImportanceLevel
	EmailRepo          *repo.EmailRepo
	SyncRepo           *repo.SyncStateRepo
	ClassificationRepo *repo.ClassificationRepo
	Filter             *importance.Filter
	Provider           email.Provider
	Notifier           telegram.Notifier
	Logger             log.Logger
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

	classification, err := s.cfg.Filter.Classify(ctx, e.ID, msg)
	if err != nil {
		return err
	}

	if err := s.cfg.ClassificationRepo.Save(ctx, classification); err != nil {
		return err
	}

	if !s.shouldNotify(classification.Level) {
		s.cfg.Logger.Debug("email ignored",
			"account_id", s.cfg.AccountID,
			"uid", msg.UID,
			"level", string(classification.Level),
			"score", classification.Score,
		)
		return s.cfg.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusIgnored)
	}

	if err := s.cfg.Notifier.SendNewEmail(ctx, e); err != nil {
		return err
	}

	if err := s.cfg.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusNotified); err != nil {
		return err
	}

	s.cfg.Logger.Info("notified",
		"account_id", s.cfg.AccountID,
		"uid", msg.UID,
		"subject", msg.Subject,
		"level", string(classification.Level),
		"score", classification.Score,
		"category", string(classification.Category),
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
	min := order[s.cfg.MinImportance]
	got := order[level]
	return got >= min
}

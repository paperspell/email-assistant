package digest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

// Sender delivers a built digest to the user and returns the message id used to
// map replies/buttons back to it. The digestID is embedded in the button data.
type Sender interface {
	SendDigest(ctx context.Context, text, digestID string) (int64, error)
}

// Config holds the dependencies for one account's digest scheduler.
type Config struct {
	AccountID    string
	AccountEmail string
	Time         string // "HH:MM" in Location
	Location     *time.Location
	EmailRepo    *repo.EmailRepo
	ClassRepo    *repo.ClassificationRepo
	DigestRepo   *repo.DigestRepo
	Sender       Sender
	Logger       log.Logger
	Now          func() time.Time // injectable clock; defaults to time.Now
}

// Scheduler sends one account's daily digest at a fixed local time.
type Scheduler struct {
	cfg    Config
	stopCh chan struct{}
}

// New creates a digest Scheduler.
func New(cfg Config) *Scheduler {
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Scheduler{cfg: cfg, stopCh: make(chan struct{})}
}

// Start blocks, sending the digest at each scheduled time until ctx is cancelled
// or Stop is called.
func (s *Scheduler) Start(ctx context.Context) error {
	s.cfg.Logger.Info("digest scheduler starting",
		"account_id", s.cfg.AccountID, "time", s.cfg.Time, "tz", s.cfg.Location.String())

	for {
		next := s.nextFire(s.cfg.Now())
		wait := time.Until(next)
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			if err := s.runOnce(ctx); err != nil {
				s.cfg.Logger.Error(err, "account_id", s.cfg.AccountID)
			}
		case <-s.stopCh:
			timer.Stop()
			return nil
		case <-ctx.Done():
			timer.Stop()
			return nil
		}
	}
}

// Stop signals the scheduler to stop.
func (s *Scheduler) Stop() { close(s.stopCh) }

// runOnce builds and sends the digest for the current day, then persists it.
func (s *Scheduler) runOnce(ctx context.Context) error {
	date := s.cfg.Now().In(s.cfg.Location).Format("2006-01-02")

	existing, err := s.cfg.DigestRepo.GetByAccountAndDate(ctx, s.cfg.AccountID, date)
	if err != nil {
		return err
	}
	if existing != nil {
		s.cfg.Logger.Debug("digest already sent today", "account_id", s.cfg.AccountID, "date", date)
		return nil
	}

	d, err := Build(ctx, s.cfg.EmailRepo, s.cfg.ClassRepo, s.cfg.AccountID, date, s.cfg.Location)
	if err != nil {
		return err
	}
	if d.Empty() {
		s.cfg.Logger.Info("digest skipped: nothing to report", "account_id", s.cfg.AccountID, "date", date)
		return nil
	}

	digestID := idx.GenerateID()
	msgID, err := s.cfg.Sender.SendDigest(ctx, FormatTelegram(d, s.cfg.AccountEmail), digestID)
	if err != nil {
		return fmt.Errorf("send digest: %w", err)
	}

	items := make([]domain.DigestItem, 0, len(d.Items))
	for _, it := range d.Items {
		items = append(items, domain.DigestItem{DigestID: digestID, SeqNo: it.SeqNo, EmailID: it.Email.ID})
	}
	if err := s.cfg.DigestRepo.Save(ctx, domain.Digest{
		ID:          digestID,
		AccountID:   s.cfg.AccountID,
		Date:        date,
		TGMessageID: msgID,
		SentAt:      s.cfg.Now().UTC(),
	}, items); err != nil {
		return fmt.Errorf("save digest: %w", err)
	}
	s.cfg.Logger.Info("digest sent",
		"account_id", s.cfg.AccountID, "date", date, "items", len(d.Items), "filtered", d.Counter.Total)
	return nil
}

// nextFire returns the next occurrence of the configured time, in Location.
func (s *Scheduler) nextFire(now time.Time) time.Time {
	now = now.In(s.cfg.Location)
	hour, minute := parseHM(s.cfg.Time)
	fire := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, s.cfg.Location)
	if !fire.After(now) {
		fire = fire.AddDate(0, 0, 1)
	}
	return fire
}

// parseHM parses a pre-validated "HH:MM" string, defaulting to 20:00 on error.
func parseHM(s string) (hour, minute int) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 20, 0
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 20, 0
	}
	return h, m
}

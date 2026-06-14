package importance

import (
	"context"
	"fmt"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/features"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/timex"
)

// Filter classifies incoming emails using feature extraction and rule-based scoring.
type Filter struct {
	senderRepo *repo.SenderRepo
	domainRepo *repo.DomainRepo
}

// NewFilter creates a Filter backed by the given repositories.
func NewFilter(senderRepo *repo.SenderRepo, domainRepo *repo.DomainRepo) *Filter {
	return &Filter{senderRepo: senderRepo, domainRepo: domainRepo}
}

// Classify scores msg and returns a Classification.
// It also increments the sender's seen_count in the database.
func (f *Filter) Classify(ctx context.Context, emailID string, msg email.Message) (domain.Classification, error) {
	sender, err := f.senderRepo.Get(ctx, msg.FromEmail)
	if err != nil {
		return domain.Classification{}, fmt.Errorf("load sender: %w", err)
	}

	domainStr := features.ExtractDomain(msg.FromEmail)
	domRec, err := f.domainRepo.Get(ctx, domainStr)
	if err != nil {
		return domain.Classification{}, fmt.Errorf("load domain: %w", err)
	}

	senderScore, domainScore, seenCount := 0, 0, 0
	if sender != nil {
		senderScore = sender.ImportanceScore
		seenCount = sender.SeenCount
	}
	if domRec != nil {
		domainScore = domRec.ImportanceScore
	}

	feats := features.Extract(msg, senderScore, domainScore, seenCount)
	score, reasons := Score(feats)
	level := Level(score)
	category := Category(feats)

	// Increment sender seen_count
	now := timex.NowUTC()
	newSender := domain.Sender{
		Email:           msg.FromEmail,
		ImportanceScore: senderScore,
		SeenCount:       seenCount + 1,
		UpdatedAt:       now,
	}
	if sender != nil {
		newSender.ID = sender.ID
	} else {
		newSender.ID = idx.GenerateID()
	}
	if err := f.senderRepo.Upsert(ctx, newSender); err != nil {
		return domain.Classification{}, fmt.Errorf("update sender: %w", err)
	}

	return domain.Classification{
		ID:           idx.GenerateID(),
		EmailID:      emailID,
		Level:        level,
		Category:     category,
		Score:        score,
		Reason:       reasons,
		ClassifiedAt: now,
	}, nil
}

package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/timex"
)

const replyHint = "Reply to a digest message with /important <n,…>."

// handleMessage dispatches message updates. Only the `/important` promote command
// (sent as a reply to a digest) is handled; everything else is ignored.
func (h *Handler) handleMessage(ctx context.Context, msg *gotgbot.Message) error {
	if msg == nil {
		return nil
	}
	text := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(text, "/important") {
		return nil
	}

	if msg.ReplyToMessage == nil || h.DigestRepo == nil {
		return h.Bot.SendFollowUp(ctx, replyHint)
	}
	d, err := h.DigestRepo.GetByTGMessageID(ctx, msg.ReplyToMessage.MessageId)
	if err != nil {
		return err
	}
	if d == nil {
		return h.Bot.SendFollowUp(ctx, replyHint)
	}

	seqNos := parseSeqNos(strings.TrimPrefix(text, "/important"))
	if len(seqNos) == 0 {
		return h.Bot.SendFollowUp(ctx, "Usage: /important 3,7")
	}

	promoted, err := h.promote(ctx, d, seqNos)
	if err != nil {
		return err
	}
	return h.Bot.SendFollowUp(ctx, fmt.Sprintf("Promoted %d item(s) to important.", promoted))
}

// promote re-sends the selected digest items as important and marks them promoted.
func (h *Handler) promote(ctx context.Context, d *domain.Digest, seqNos []int) (int, error) {
	if h.Notifier == nil {
		return 0, nil
	}
	items, err := h.DigestRepo.Items(ctx, d.ID)
	if err != nil {
		return 0, err
	}
	bySeq := make(map[int]domain.DigestItem, len(items))
	for _, it := range items {
		bySeq[it.SeqNo] = it
	}

	promoted := 0
	for _, n := range seqNos {
		it, ok := bySeq[n]
		if !ok || it.Promoted {
			continue
		}
		e, err := h.EmailRepo.GetByID(ctx, it.EmailID)
		if err != nil {
			return promoted, err
		}
		if e == nil {
			continue
		}

		c := h.promoteClassification(ctx, e)
		info := h.Accounts[e.AccountID]
		tgMsgID, err := h.Notifier.SendNewEmail(ctx, *e, c, info.Name, info.Email)
		if err != nil {
			return promoted, err
		}
		if err := h.ClassificationRepo.Save(ctx, c); err != nil {
			return promoted, err
		}
		if err := h.EmailRepo.SetTelegramMessageID(ctx, e.ID, tgMsgID); err != nil {
			return promoted, err
		}
		if err := h.EmailRepo.UpdateStatusDecidedBy(ctx, e.ID, domain.StatusNotified, ""); err != nil {
			return promoted, err
		}
		if err := h.adjustSenderScore(ctx, e.AccountID, e.FromEmail, feedbackPositiveDelta); err != nil {
			return promoted, err
		}
		if err := h.DigestRepo.MarkPromoted(ctx, d.ID, n); err != nil {
			return promoted, err
		}
		h.Logger.Info("digest item promoted", "email_id", e.ID, "from", e.FromEmail, "seq_no", n)
		promoted++
	}
	return promoted, nil
}

// promoteClassification builds an "important" classification for a promoted email,
// reusing the prior LLM summary when available.
func (h *Handler) promoteClassification(ctx context.Context, e *domain.Email) domain.Classification {
	summary := ""
	if all, err := h.ClassificationRepo.GetAllByEmailID(ctx, e.ID); err == nil {
		for _, c := range all {
			if strings.HasPrefix(c.Source, domain.SourceLLM) && c.Summary != "" {
				summary = c.Summary
				break
			}
		}
	}
	return domain.Classification{
		ID:           idx.GenerateID(),
		EmailID:      e.ID,
		Level:        domain.LevelImportant,
		Category:     domain.CategoryOther,
		Score:        70,
		Reason:       []string{"promoted from digest"},
		Summary:      summary,
		ClassifiedAt: timex.NowUTC(),
		Source:       domain.SourceRuleBased,
	}
}

// parseSeqNos extracts comma/space-separated positive integers from s.
func parseSeqNos(s string) []int {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	var nums []int
	seen := map[int]bool{}
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 1 || seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	return nums
}

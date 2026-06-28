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

// handleMessage dispatches message updates: free-text input the bot is waiting
// for (ignore-reason / subject-edit), then the `/important` promote command.
func (h *Handler) handleMessage(ctx context.Context, msg *gotgbot.Message) error {
	if msg == nil {
		return nil
	}
	text := strings.TrimSpace(msg.Text)

	// A non-command message may complete a pending multi-step action.
	if !strings.HasPrefix(text, "/") && text != "" {
		if p, err := h.pending(ctx, msg.Chat.Id); err != nil {
			return err
		} else if p != nil {
			return h.consumePending(ctx, p, text)
		}
	}

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

		// Capture provenance before clearing it, to drive the follow-up offer.
		prevDecidedBy := e.DecidedBy

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
		if err := h.sendPromoteFollowup(ctx, e, prevDecidedBy); err != nil {
			h.Logger.Error(err, "email_id", e.ID)
		}
		h.Logger.Info("digest item promoted", "email_id", e.ID, "from", e.FromEmail, "seq_no", n)
		promoted++
	}
	return promoted, nil
}

// sendPromoteFollowup offers rule learning after a promote: remove/exception when
// an explicit rule hid the email, or an always-important allow rule otherwise.
func (h *Handler) sendPromoteFollowup(ctx context.Context, e *domain.Email, decidedBy string) error {
	if h.RuleRepo == nil {
		return nil
	}
	if ruleID, ok := strings.CutPrefix(decidedBy, "rule:"); ok {
		text := fmt.Sprintf("Promoted. It was hidden by rule (%s).", h.ruleDescription(ctx, e.AccountID, ruleID))
		return h.Bot.SendPrompt(ctx, text, promoteRuleMenu(ruleID, e.ID))
	}
	text := fmt.Sprintf("Promoted. Always treat mail from %s as important?", e.FromEmail)
	return h.Bot.SendPrompt(ctx, text, promoteAllowMenu(e.ID))
}

// consumePending completes a pending free-text action with the user's message.
func (h *Handler) consumePending(ctx context.Context, p *domain.PendingAction, text string) error {
	defer h.clearPending(ctx, p.ChatID)

	switch p.Kind {
	case domain.PendingClause:
		if h.ClauseRepo == nil {
			return nil
		}
		if err := h.ClauseRepo.Add(ctx, domain.LLMClause{
			ID: idx.GenerateID(), AccountID: p.AccountID, Text: text,
			Enabled: true, Source: domain.RuleSourceUser,
		}); err != nil {
			return err
		}
		h.Logger.Info("ignore clause added", "account_id", p.AccountID)
		return h.Bot.SendFollowUp(ctx, "Ignore clause added (applies to future mail via the LLM).")

	case domain.PendingSubjectEdit:
		if h.RuleRepo == nil {
			return nil
		}
		e, err := h.EmailRepo.GetByID(ctx, p.EmailID)
		if err != nil {
			return err
		}
		scopeValue := ""
		if e != nil {
			scopeValue = e.FromEmail
		}
		ruleID, err := h.addIgnoreRule(ctx, p.AccountID, domain.RuleTypeSubject, text,
			domain.RuleTypeSender, scopeValue)
		if err != nil {
			return err
		}
		if e != nil {
			if err := h.EmailRepo.UpdateStatusDecidedBy(ctx, e.ID, domain.StatusIgnored, "rule:"+ruleID); err != nil {
				h.Logger.Error(err, "email_id", e.ID)
			}
		}
		return h.Bot.SendFollowUp(ctx, fmt.Sprintf("Rule added: ignore subject ~ %q.", text))

	default:
		return nil // subject_confirm is resolved by a button, not a message
	}
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

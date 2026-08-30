package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/features"
	"github.com/paperspell/email-assistant/internal/filter"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
)

// handleIgnoreLeaf processes a choice from the ignore menu: it creates the matching
// per-account rule (or none, for "once") and ignores the email.
func (h *Handler) handleIgnoreLeaf(ctx context.Context, action string, chatID, msgID int64, e *domain.Email) error {
	switch action {
	case "ign_cancel":
		// Restore the original keyboard; take no action.
		return h.Bot.EditKeyboard(msgID, actionKeyboard(h.P, e.ID))

	case "ign_once":
		if err := h.ignoreWithProvenance(ctx, e, ""); err != nil {
			return err
		}
		return h.finishLeaf(ctx, msgID, h.P.T("flow_ignored_no_rule"))

	case "ign_sender":
		return h.createRuleAndIgnore(ctx, msgID, e, domain.RuleTypeSender, e.FromEmail, "", "",
			"flow_rule_added_sender")

	case "ign_domain":
		dom := features.ExtractDomain(e.FromEmail)
		return h.createRuleAndIgnore(ctx, msgID, e, domain.RuleTypeDomain, dom, "", "",
			"flow_rule_added_domain")

	case "ign_listid":
		if e.ListID == "" {
			return h.finishLeaf(ctx, msgID, h.P.T("flow_no_list_id"))
		}
		return h.createRuleAndIgnore(ctx, msgID, e, domain.RuleTypeListID, e.ListID, "", "",
			"flow_rule_added_list")

	case "ign_subject":
		return h.startSubjectFlow(ctx, chatID, msgID, e)

	case "ign_reason":
		if h.PendingRepo == nil {
			return h.finishLeaf(ctx, msgID, h.P.T("flow_reason_unavailable"))
		}
		if err := h.ignoreWithProvenance(ctx, e, ""); err != nil {
			return err
		}
		if err := h.PendingRepo.Set(ctx, domain.PendingAction{
			ChatID: chatID, Kind: domain.PendingClause, EmailID: e.ID, AccountID: e.AccountID,
		}); err != nil {
			return err
		}
		if err := h.Bot.RemoveKeyboard(msgID); err != nil {
			h.Logger.Error(err, "email_id", e.ID)
		}
		return h.Bot.SendFollowUp(ctx, h.P.T("flow_send_reason"))

	default:
		return nil
	}
}

// createRuleAndIgnore adds an ignore rule, ignores the email with rule provenance,
// and confirms with the message named by confirmID, which receives the rule
// value as {{.Value}}. The confirmation is a whole sentence per rule type: an
// English fragment assembled at the call site could not be translated.
func (h *Handler) createRuleAndIgnore(
	ctx context.Context, msgID int64, e *domain.Email,
	ruleType, value, scopeKind, scopeValue, confirmID string,
) error {
	ruleID, err := h.addIgnoreRule(ctx, e.AccountID, ruleType, value, scopeKind, scopeValue)
	if err != nil {
		return err
	}
	if err := h.ignoreWithProvenance(ctx, e, "rule:"+ruleID); err != nil {
		return err
	}
	h.Logger.Info("ignore rule added", "email_id", e.ID, "rule_id", ruleID, "type", ruleType, "value", value)
	return h.finishLeaf(ctx, msgID, h.P.T(confirmID, "Value", value))
}

// startSubjectFlow ignores the email and offers a suggested subject pattern.
func (h *Handler) startSubjectFlow(ctx context.Context, chatID, msgID int64, e *domain.Email) error {
	if h.PendingRepo == nil {
		return h.finishLeaf(ctx, msgID, h.P.T("flow_subject_unavailable"))
	}
	if err := h.ignoreWithProvenance(ctx, e, ""); err != nil {
		return err
	}
	pattern := filter.SuggestSubjectPattern(e.Subject)
	if err := h.PendingRepo.Set(ctx, domain.PendingAction{
		ChatID: chatID, Kind: domain.PendingSubjectConfirm, EmailID: e.ID, AccountID: e.AccountID, Payload: pattern,
	}); err != nil {
		return err
	}
	if err := h.Bot.EditKeyboard(msgID, subjectConfirmMenu(h.P, e.ID)); err != nil {
		return err
	}
	return h.Bot.SendFollowUp(ctx, h.P.T("flow_suggested_pattern", "Pattern", strconv.Quote(pattern)))
}

// handleSubjectChoice processes Use/Edit/Cancel on a suggested subject pattern.
func (h *Handler) handleSubjectChoice(ctx context.Context, action string, chatID, msgID int64, e *domain.Email) error {
	switch action {
	case "subj_use":
		p, err := h.pending(ctx, chatID)
		if err != nil || p == nil || p.Kind != domain.PendingSubjectConfirm {
			return h.finishLeaf(ctx, msgID, h.P.T("flow_nothing_to_confirm"))
		}
		ruleID, err := h.addIgnoreRule(ctx, e.AccountID, domain.RuleTypeSubject, p.Payload,
			domain.RuleTypeSender, e.FromEmail)
		if err != nil {
			return err
		}
		h.clearPending(ctx, chatID)
		if err := h.EmailRepo.UpdateStatusDecidedBy(ctx, e.ID, domain.StatusIgnored, "rule:"+ruleID); err != nil {
			return err
		}
		return h.finishLeaf(ctx, msgID, h.P.T("flow_rule_added_subject_from",
			"Pattern", strconv.Quote(p.Payload), "Sender", e.FromEmail))

	case "subj_edit":
		if err := h.PendingRepo.Set(ctx, domain.PendingAction{
			ChatID: chatID, Kind: domain.PendingSubjectEdit, EmailID: e.ID, AccountID: e.AccountID,
		}); err != nil {
			return err
		}
		if err := h.Bot.RemoveKeyboard(msgID); err != nil {
			h.Logger.Error(err, "email_id", e.ID)
		}
		return h.Bot.SendFollowUp(ctx, h.P.T("flow_send_subject_pattern"))

	case "subj_cancel":
		h.clearPending(ctx, chatID)
		return h.finishLeaf(ctx, msgID, h.P.T("flow_subject_cancelled"))

	default:
		return nil
	}
}

// handlePromoteFollowup processes the choices offered after a digest promote.
func (h *Handler) handlePromoteFollowup(ctx context.Context, action, arg string, msgID int64) error {
	switch action {
	case "prom_rmrule":
		if h.RuleRepo != nil {
			if err := h.RuleRepo.Delete(ctx, arg); err != nil {
				return err
			}
		}
		return h.finishLeaf(ctx, msgID, h.P.T("flow_rule_removed"))

	case "prom_allow":
		e, err := h.EmailRepo.GetByID(ctx, arg)
		if err != nil {
			return err
		}
		if e == nil {
			return h.finishLeaf(ctx, msgID, h.P.T("flow_email_unavailable"))
		}
		if _, err := h.addAllowSenderRule(ctx, e.AccountID, e.FromEmail); err != nil {
			return err
		}
		return h.finishLeaf(ctx, msgID, h.P.T("flow_allow_sender_added", "Sender", e.FromEmail))

	case "prom_keep", "prom_no":
		return h.finishLeaf(ctx, msgID, h.P.T("flow_ok"))

	default:
		return nil
	}
}

// addIgnoreRule creates a user ignore rule for the account.
func (h *Handler) addIgnoreRule(
	ctx context.Context, accountID, ruleType, value, scopeKind, scopeValue string,
) (string, error) {
	matcher := domain.MatcherExact
	if ruleType == domain.RuleTypeSubject {
		matcher = domain.MatcherSubstring
	}
	id := idx.GenerateID()
	err := h.RuleRepo.Add(ctx, domain.FilterRule{
		ID: id, AccountID: accountID, Action: domain.RuleActionIgnore, Type: ruleType,
		Matcher: matcher, Value: value, ScopeKind: scopeKind, ScopeValue: scopeValue,
		Enabled: true, Source: domain.RuleSourceUser,
	})
	return id, err
}

// addAllowSenderRule creates a user allow rule keyed on the sender.
func (h *Handler) addAllowSenderRule(ctx context.Context, accountID, sender string) (string, error) {
	id := idx.GenerateID()
	err := h.RuleRepo.Add(ctx, domain.FilterRule{
		ID: id, AccountID: accountID, Action: domain.RuleActionAllow, Type: domain.RuleTypeSender,
		Matcher: domain.MatcherExact, Value: sender, Enabled: true, Source: domain.RuleSourceUser,
	})
	return id, err
}

// finishLeaf drops the keyboard and reports the outcome.
func (h *Handler) finishLeaf(ctx context.Context, msgID int64, text string) error {
	if err := h.Bot.RemoveKeyboard(msgID); err != nil {
		h.Logger.Error(err, "msg_id", msgID)
	}
	return h.Bot.SendFollowUp(ctx, text)
}

// pending returns the chat's pending action, tolerating a nil repo.
func (h *Handler) pending(ctx context.Context, chatID int64) (*domain.PendingAction, error) {
	if h.PendingRepo == nil {
		return nil, nil
	}
	return h.PendingRepo.Get(ctx, chatID)
}

// clearPending deletes the chat's pending action, logging (not failing) on error.
func (h *Handler) clearPending(ctx context.Context, chatID int64) {
	if h.PendingRepo == nil {
		return
	}
	if err := h.PendingRepo.Delete(ctx, chatID); err != nil {
		h.Logger.Error(err, "chat_id", chatID)
	}
}

// ruleDescription renders a short human label for a rule id (for promote prompts).
func (h *Handler) ruleDescription(ctx context.Context, accountID, ruleID string) string {
	rules, err := h.RuleRepo.List(ctx, accountID)
	if err != nil {
		return ruleID
	}
	for _, r := range rules {
		if r.ID == ruleID {
			return fmt.Sprintf("%s %s %s", r.Action, r.Type, strings.TrimSpace(r.Value))
		}
	}
	return ruleID
}

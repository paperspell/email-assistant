package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/pkg/timex"
)

const (
	feedbackPositiveDelta = 25
	feedbackNegativeDelta = 25
)

// BotClient abstracts the Telegram API calls needed by the Handler.
type BotClient interface {
	AnswerCallback(queryID, text string) error
	RemoveKeyboard(msgID int64) error
	SendFollowUp(ctx context.Context, text string) error
}

// Mailbox is the subset of email.Provider the Handler needs to act on a mailbox.
// It is satisfied structurally by the per-account providers, so the handler
// reaches the mailbox only through the provider abstraction, never IMAP directly.
type Mailbox interface {
	MarkRead(ctx context.Context, uid uint32) error
	FetchBody(ctx context.Context, uid uint32) (string, error)
}

// Handler processes incoming Telegram callback queries and dispatches actions.
type Handler struct {
	Bot                BotClient
	EmailRepo          *repo.EmailRepo
	SenderRepo         *repo.SenderRepo
	ClassificationRepo *repo.ClassificationRepo
	Mailboxes          map[string]Mailbox // keyed by account ID
	Logger             log.Logger
}

// Handle dispatches a single Telegram update.
func (h *Handler) Handle(ctx context.Context, update gotgbot.Update) error {
	if update.CallbackQuery == nil {
		return nil
	}
	q := update.CallbackQuery

	action, emailID, ok := parseCallbackData(q.Data)
	if !ok {
		h.Logger.Info("ignoring malformed callback data", "data", q.Data)
		return nil
	}

	if err := h.Bot.AnswerCallback(q.Id, ""); err != nil {
		h.Logger.Error(err, "callback_query_id", q.Id)
	}

	e, err := h.EmailRepo.GetByID(ctx, emailID)
	if err != nil {
		return err
	}
	if e == nil {
		h.Logger.Info("callback references unknown email", "email_id", emailID)
		return h.Bot.RemoveKeyboard(q.Message.GetMessageId())
	}

	msgID := q.Message.GetMessageId()

	switch action {
	case "handled":
		return h.handleHandled(ctx, msgID, e)
	case "ignore":
		return h.handleIgnore(ctx, msgID, e)
	case "details":
		return h.handleDetails(ctx, e)
	default:
		h.Logger.Info("unknown callback action", "action", action, "email_id", emailID)
		return nil
	}
}

func (h *Handler) handleHandled(ctx context.Context, msgID int64, e *domain.Email) error {
	if err := h.adjustSenderScore(ctx, e.FromEmail, feedbackPositiveDelta); err != nil {
		return err
	}
	if err := h.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusHandled); err != nil {
		return err
	}
	h.markRead(ctx, e)
	h.Logger.Info("email marked handled", "email_id", e.ID, "from", e.FromEmail)
	return h.Bot.RemoveKeyboard(msgID)
}

func (h *Handler) handleIgnore(ctx context.Context, msgID int64, e *domain.Email) error {
	if err := h.adjustSenderScore(ctx, e.FromEmail, -feedbackNegativeDelta); err != nil {
		return err
	}
	if err := h.EmailRepo.UpdateStatus(ctx, e.ID, domain.StatusIgnored); err != nil {
		return err
	}
	h.markRead(ctx, e)
	h.Logger.Info("email marked ignored via feedback", "email_id", e.ID, "from", e.FromEmail)
	return h.Bot.RemoveKeyboard(msgID)
}

// markRead flags the email as read in the mailbox. Best-effort: a missing
// provider or a connection error is logged and never fails the button.
func (h *Handler) markRead(ctx context.Context, e *domain.Email) {
	mb, ok := h.Mailboxes[e.AccountID]
	if !ok {
		return
	}
	if err := mb.MarkRead(ctx, e.MessageUID); err != nil {
		h.Logger.Error(err, "email_id", e.ID, "account_id", e.AccountID, "uid", e.MessageUID)
	}
}

func (h *Handler) handleDetails(ctx context.Context, e *domain.Email) error {
	all, err := h.ClassificationRepo.GetAllByEmailID(ctx, e.ID)
	if err != nil {
		return err
	}
	body := h.fetchBody(ctx, e)
	text := formatDetails(e, all, body)
	return h.Bot.SendFollowUp(ctx, text)
}

// fetchBody retrieves the email body on demand. Best-effort: a missing provider
// or fetch error returns "" so Details still shows the metadata/classification.
func (h *Handler) fetchBody(ctx context.Context, e *domain.Email) string {
	mb, ok := h.Mailboxes[e.AccountID]
	if !ok {
		return ""
	}
	body, err := mb.FetchBody(ctx, e.MessageUID)
	if err != nil {
		h.Logger.Error(err, "email_id", e.ID, "account_id", e.AccountID, "uid", e.MessageUID)
		return ""
	}
	return body
}

func (h *Handler) adjustSenderScore(ctx context.Context, emailAddr string, delta int) error {
	sender, err := h.SenderRepo.Get(ctx, emailAddr)
	if err != nil {
		return fmt.Errorf("load sender: %w", err)
	}

	now := timex.NowUTC()
	if sender == nil {
		sender = &domain.Sender{
			ID:        idx.GenerateID(),
			Email:     emailAddr,
			UpdatedAt: now,
		}
	}

	sender.ImportanceScore = clampScore(sender.ImportanceScore+delta, 0, 100)
	sender.UpdatedAt = now

	if err := h.SenderRepo.Upsert(ctx, *sender); err != nil {
		return fmt.Errorf("update sender score: %w", err)
	}
	return nil
}

func parseCallbackData(data string) (action, emailID string, ok bool) {
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func formatDetails(e *domain.Email, all []domain.Classification, body string) string {
	from := e.FromEmail
	if e.FromName != "" {
		from = fmt.Sprintf("%s <%s>", e.FromName, e.FromEmail)
	}
	date := e.Date.UTC().Format("02 Jan 2006 15:04 UTC")

	var b strings.Builder
	fmt.Fprintf(&b, "ℹ Email details\n\nFrom: %s\nSubject: %s\nDate: %s\n", from, e.Subject, date)

	// Body (fetched on demand). Empty when the provider is unavailable or the
	// message has no text part.
	if body != "" {
		fmt.Fprintf(&b, "\n%s\n", body)
	} else {
		fmt.Fprint(&b, "\n(body unavailable)\n")
	}

	// Separate LLM and rule-based results
	var llmClass, ruleClass *domain.Classification
	for i := range all {
		c := &all[i]
		if strings.HasPrefix(c.Source, domain.SourceLLM) {
			llmClass = c
		} else {
			ruleClass = c
		}
	}

	// Show LLM result first if available
	if llmClass != nil {
		fmt.Fprintf(&b, "\nLLM classification: %s (score %d)\nCategory: %s\n",
			string(llmClass.Level), llmClass.Score, string(llmClass.Category))
		if llmClass.Summary != "" {
			fmt.Fprintf(&b, "Summary: %s\n", llmClass.Summary)
		}
	}

	// Always show rule-based result
	if ruleClass != nil {
		fmt.Fprintf(&b, "\nRule-based classification: %s (score %d)\nReasons:\n",
			string(ruleClass.Level), ruleClass.Score)
		for _, r := range ruleClass.Reason {
			fmt.Fprintf(&b, "  • %s\n", r)
		}
	} else if llmClass == nil && len(all) > 0 {
		// Fallback: single classification of unknown source
		c := all[0]
		fmt.Fprintf(&b, "\nClassification: %s (score %d)\nCategory: %s\nReasons:\n",
			string(c.Level), c.Score, string(c.Category))
		for _, r := range c.Reason {
			fmt.Fprintf(&b, "  • %s\n", r)
		}
	}

	return b.String()
}

func clampScore(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ensure Bot satisfies BotClient at compile time.
var _ BotClient = (*Bot)(nil)

package telegram

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/domain"
)

// Bot implements Notifier using the Telegram Bot API.
type Bot struct {
	bot    *gotgbot.Bot
	chatID int64
}

// NewBot creates a Telegram Bot notifier.
func NewBot(token string, chatID int64) (*Bot, error) {
	bot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	return &Bot{bot: bot, chatID: chatID}, nil
}

// SendNewEmail sends a notification with an inline action keyboard.
// Returns the Telegram message ID of the sent message.
func (b *Bot) SendNewEmail(
	_ context.Context, e domain.Email, c domain.Classification, accountName, accountEmail string,
) (int64, error) {
	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			{Text: "✓ Handled", CallbackData: "handled:" + e.ID},
			{Text: "✗ Ignore", CallbackData: "ignore:" + e.ID},
			{Text: "ℹ Details", CallbackData: "details:" + e.ID},
		}},
	}
	msg, err := b.bot.SendMessage(b.chatID, formatMessage(e, c, accountName, accountEmail), &gotgbot.SendMessageOpts{
		ReplyMarkup: keyboard,
		ParseMode:   "HTML",
	})
	if err != nil {
		return 0, fmt.Errorf("telegram send message: %w", err)
	}
	return msg.MessageId, nil
}

// SendDigest sends the daily digest with bulk Mark read / Remove buttons that
// carry the digest id. Returns the Telegram message id (used to map replies).
func (b *Bot) SendDigest(_ context.Context, text, digestID string) (int64, error) {
	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			{Text: "✓ Mark read", CallbackData: "digest_read:" + digestID},
			{Text: "🗑 Remove", CallbackData: "digest_remove:" + digestID},
		}},
	}
	msg, err := b.bot.SendMessage(b.chatID, text, &gotgbot.SendMessageOpts{ReplyMarkup: keyboard})
	if err != nil {
		return 0, fmt.Errorf("telegram send digest: %w", err)
	}
	return msg.MessageId, nil
}

// AnswerCallback dismisses the loading spinner on a callback query.
// text is shown as a brief pop-up notification to the user; pass "" for silent.
func (b *Bot) AnswerCallback(queryID, text string) error {
	var opts *gotgbot.AnswerCallbackQueryOpts
	if text != "" {
		opts = &gotgbot.AnswerCallbackQueryOpts{Text: text}
	}
	if _, err := b.bot.AnswerCallbackQuery(queryID, opts); err != nil {
		return fmt.Errorf("answer callback: %w", err)
	}
	return nil
}

// RemoveKeyboard removes the inline keyboard from a sent message.
func (b *Bot) RemoveKeyboard(msgID int64) error {
	if _, _, err := b.bot.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
		ChatId:    b.chatID,
		MessageId: msgID,
	}); err != nil {
		return fmt.Errorf("remove keyboard: %w", err)
	}
	return nil
}

// SendFollowUp sends a plain-text message in the configured chat.
func (b *Bot) SendFollowUp(_ context.Context, text string) error {
	if _, err := b.bot.SendMessage(b.chatID, text, nil); err != nil {
		return fmt.Errorf("send follow-up: %w", err)
	}
	return nil
}

func formatMessage(e domain.Email, c domain.Classification, accountName, accountEmail string) string {
	from := e.FromEmail
	if e.FromName != "" {
		from = fmt.Sprintf("%s <%s>", e.FromName, e.FromEmail)
	}
	date := e.Date.UTC().Format("02 Jan 2006 15:04 UTC")

	// Message is sent with HTML parse mode, so dynamic content must be escaped.
	body := fmt.Sprintf(
		"📧 New email\n<b>%s Importance: %s (score %d)</b>\n\n",
		importanceIcon(c.Level), html.EscapeString(string(c.Level)), c.Score,
	)
	if label := accountLabel(accountName, accountEmail); label != "" {
		body += "Account: " + html.EscapeString(label) + "\n"
	}
	body += fmt.Sprintf(
		"From: %s\nSubject: %s\nDate: %s",
		html.EscapeString(from), html.EscapeString(e.Subject), date,
	)
	if c.Summary != "" {
		body += "\nSummary: " + html.EscapeString(c.Summary)
	} else {
		body += "\nWhy: " + html.EscapeString(strings.Join(c.Reason, "; "))
	}
	return body
}

// accountLabel renders the source account as "Name <email>", falling back to
// the email alone when no name is set, or "" when neither is provided. The email
// keeps accounts distinguishable when several share the same name.
func accountLabel(name, email string) string {
	switch {
	case name != "" && email != "":
		return name + " <" + email + ">"
	case email != "":
		return email
	default:
		return name
	}
}

// importanceIcon returns an emoji that conveys the importance level at a glance.
func importanceIcon(level domain.ImportanceLevel) string {
	switch level {
	case domain.LevelCritical:
		return "🔴"
	case domain.LevelImportant:
		return "🟠"
	case domain.LevelMaybe:
		return "🟡"
	case domain.LevelIgnore:
		return "⚪"
	default:
		return "⚫"
	}
}

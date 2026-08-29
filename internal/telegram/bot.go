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
	msg, err := b.bot.SendMessage(b.chatID, formatMessage(e, c, accountName, accountEmail), &gotgbot.SendMessageOpts{
		ReplyMarkup: actionKeyboard(e.ID),
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

// EditKeyboard replaces the inline keyboard of a sent message.
func (b *Bot) EditKeyboard(msgID int64, kb gotgbot.InlineKeyboardMarkup) error {
	if _, _, err := b.bot.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
		ChatId:      b.chatID,
		MessageId:   msgID,
		ReplyMarkup: kb,
	}); err != nil {
		return fmt.Errorf("edit keyboard: %w", err)
	}
	return nil
}

// SendPrompt sends a message with an inline keyboard (used for follow-up choices).
func (b *Bot) SendPrompt(_ context.Context, text string, kb gotgbot.InlineKeyboardMarkup) error {
	if _, err := b.bot.SendMessage(b.chatID, text, &gotgbot.SendMessageOpts{ReplyMarkup: kb}); err != nil {
		return fmt.Errorf("send prompt: %w", err)
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

// SendAlert sends an operational alert (e.g. an account that needs OAuth
// re-authorization) as an HTML-formatted message in the configured chat.
func (b *Bot) SendAlert(_ context.Context, text string) error {
	if _, err := b.bot.SendMessage(b.chatID, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err != nil {
		return fmt.Errorf("send alert: %w", err)
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
	// Layout: the subject sits directly under the importance line, because that
	// pair alone decides whether the message is worth opening. Addresses and date
	// form the second block, the summary a third — each separated by a blank line.
	body := fmt.Sprintf(
		"📧 New email\n<b>%s Importance: %s (score %d)</b>\n\nSubject: %s\n\n",
		importanceIcon(c.Level), html.EscapeString(string(c.Level)), c.Score,
		html.EscapeString(e.Subject),
	)
	if label := accountLabel(accountName, accountEmail); label != "" {
		body += "Account: " + html.EscapeString(label) + "\n"
	}
	body += "From: " + html.EscapeString(from) + "\nDate: " + date
	if lang := languageName(e.Language); lang != "" {
		body += "\nOriginal language: " + html.EscapeString(lang)
	}
	if c.Summary != "" {
		body += "\n\nSummary: " + html.EscapeString(c.Summary)
	} else {
		body += "\n\nWhy: " + html.EscapeString(strings.Join(c.Reason, "; "))
	}
	return body
}

// accountLabel renders the source account as "Name <email>", falling back to
// the email alone when no name is set, or "" when neither is provided. The email
// keeps accounts distinguishable when several share the same name.
func accountLabel(name, email string) string {
	switch {
	// The setup wizard defaults the account name to the address, which rendered
	// as "user@example.com <user@example.com>".
	case name == email:
		return email
	case name != "" && email != "":
		return name + " <" + email + ">"
	case email != "":
		return email
	default:
		return name
	}
}

// languageName renders a detected ISO 639-1 code as a readable name for the
// notification. "und" means detection failed (or the subject was empty), which
// is not worth a line of its own; unknown codes are shown as-is.
func languageName(code string) string {
	switch code {
	case "", "und":
		return ""
	case "ru":
		return "Russian"
	case "en":
		return "English"
	case "pl":
		return "Polish"
	case "de":
		return "German"
	case "fr":
		return "French"
	case "es":
		return "Spanish"
	case "it":
		return "Italian"
	case "uk":
		return "Ukrainian"
	case "be":
		return "Belarusian"
	case "pt":
		return "Portuguese"
	case "nl":
		return "Dutch"
	default:
		return strings.ToUpper(code)
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

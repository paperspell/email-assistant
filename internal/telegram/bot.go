package telegram

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
)

// Bot implements Notifier using the Telegram Bot API.
type Bot struct {
	bot    *gotgbot.Bot
	chatID int64
	// p renders every user-facing string in the configured language. A nil
	// printer falls back to English, so the bot still sends readable messages.
	p *i18n.Printer
}

// NewBot creates a Telegram Bot notifier that writes in the given locale.
func NewBot(token string, chatID int64, p *i18n.Printer) (*Bot, error) {
	bot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	return &Bot{bot: bot, chatID: chatID, p: p}, nil
}

// SendNewEmail sends a notification with an inline action keyboard.
// Returns the Telegram message ID of the sent message.
func (b *Bot) SendNewEmail(
	_ context.Context, e domain.Email, c domain.Classification, accountName, accountEmail string,
) (int64, error) {
	msg, err := b.bot.SendMessage(b.chatID, formatMessage(b.p, e, c, accountName, accountEmail), &gotgbot.SendMessageOpts{
		ReplyMarkup: actionKeyboard(b.p, e.ID),
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
			{Text: b.p.T("btn_digest_read"), CallbackData: "digest_read:" + digestID},
			{Text: b.p.T("btn_digest_remove"), CallbackData: "digest_remove:" + digestID},
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

func formatMessage(
	p *i18n.Printer, e domain.Email, c domain.Classification, accountName, accountEmail string,
) string {
	from := e.FromEmail
	if e.FromName != "" {
		from = fmt.Sprintf("%s <%s>", e.FromName, e.FromEmail)
	}
	date := e.Date.UTC().Format("02 Jan 2006 15:04 UTC")

	// Message is sent with HTML parse mode. Catalog strings are authored here and
	// safe; only dynamic content is escaped.
	// Layout: the subject sits directly under the importance line, because that
	// pair alone decides whether the message is worth opening. Addresses and date
	// form the second block, the summary a third — each separated by a blank line.
	body := fmt.Sprintf(
		"%s\n<b>%s %s</b>\n\n%s: %s\n\n",
		p.T("notification_header"),
		importanceIcon(c.Level),
		p.T("notification_importance", "Level", levelName(p, c.Level), "Score", c.Score),
		p.T("field_subject"), html.EscapeString(e.Subject),
	)
	if label := accountLabel(accountName, accountEmail); label != "" {
		body += p.T("field_account") + ": " + html.EscapeString(label) + "\n"
	}
	body += p.T("field_from") + ": " + html.EscapeString(from) + "\n" + p.T("field_date") + ": " + date
	if lang := p.EmailLanguage(e.Language); lang != "" {
		body += "\n" + p.T("field_original_language") + ": " + html.EscapeString(lang)
	}
	if c.Summary != "" {
		body += "\n\n" + p.T("field_summary") + ": " + html.EscapeString(c.Summary)
	} else {
		body += "\n\n" + p.T("field_why") + ": " + html.EscapeString(strings.Join(c.Reason, "; "))
	}
	return body
}

// levelName renders an importance level in the user's language. The stored and
// prompt-facing values stay the English identifiers — only the display changes.
func levelName(p *i18n.Printer, level domain.ImportanceLevel) string {
	id := "level_" + string(level)
	if name := p.T(id); name != id {
		return name
	}
	return string(level)
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

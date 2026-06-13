package telegram

import (
	"context"
	"fmt"
	"time"

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

// SendNewEmail sends a notification about a newly detected email.
func (b *Bot) SendNewEmail(_ context.Context, e domain.Email) error {
	text := formatMessage(e)
	if _, err := b.bot.SendMessage(b.chatID, text, nil); err != nil {
		return fmt.Errorf("telegram send message: %w", err)
	}
	return nil
}

func formatMessage(e domain.Email) string {
	from := e.FromEmail
	if e.FromName != "" {
		from = fmt.Sprintf("%s <%s>", e.FromName, e.FromEmail)
	}
	date := e.Date.UTC().Format(time.RFC1123)
	return fmt.Sprintf("New email\n\nFrom: %s\nSubject: %s\nDate: %s", from, e.Subject, date)
}

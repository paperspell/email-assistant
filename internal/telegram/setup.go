package telegram

import (
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// SetupClient supports first-time interactive Telegram configuration: it
// validates the bot token, exposes the bot's @username, discovers the chat id
// from an incoming message, and sends a verification message. Unlike Bot it
// does not need a chat id up front — the whole point is to find one.
type SetupClient struct {
	bot *gotgbot.Bot
}

// NewSetupClient creates a setup client. Constructing it calls getMe, which
// validates the token and populates the bot's username.
func NewSetupClient(token string) (*SetupClient, error) {
	bot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	return &SetupClient{bot: bot}, nil
}

// Username returns the bot's @username (without the leading @).
func (c *SetupClient) Username() string {
	return c.bot.Username
}

// LatestChatID returns the chat id of the most recent incoming message, or 0 if
// none is pending yet. It reads updates without confirming them (no offset), so
// it does not consume messages the daemon's poller will later process.
func (c *SetupClient) LatestChatID() (int64, error) {
	updates, err := c.bot.GetUpdates(nil)
	if err != nil {
		return 0, fmt.Errorf("get updates: %w", err)
	}
	var chatID int64
	for _, u := range updates {
		if u.Message != nil {
			chatID = u.Message.Chat.Id
		}
	}
	return chatID, nil
}

// SendVerification sends a confirmation message to chatID, proving the bot can
// actually deliver to it (a wrong or unstarted chat yields "chat not found").
func (c *SetupClient) SendVerification(chatID int64) error {
	if _, err := c.bot.SendMessage(chatID, "email-agent: Telegram setup verified ✅", nil); err != nil {
		return fmt.Errorf("send verification: %w", err)
	}
	return nil
}

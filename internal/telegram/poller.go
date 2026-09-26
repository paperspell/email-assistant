package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

const (
	longPollTimeout = 25               // seconds — Telegram long-poll window
	requestTimeout  = 30 * time.Second // HTTP timeout — must exceed longPollTimeout
	retryDelay      = 5 * time.Second
)

// Poller long-polls the Telegram Bot API for callback_query updates
// and dispatches each one to the Handler.
type Poller struct {
	Bot          *Bot
	Handler      *Handler
	SettingsRepo *repo.SettingsRepo
	Logger       log.Logger
	// AllowedChatID is the one chat the bot serves. A Telegram bot is reachable
	// by anyone who learns its username, so every update from another chat is
	// dropped before it can reach the handler. Zero disables the check, which
	// only tests should rely on.
	AllowedChatID int64
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	if err := p.ensureNoWebhook(ctx); err != nil {
		return err
	}

	offset := p.loadOffset(ctx)
	p.Logger.Info("telegram poller starting", "offset", offset)

	for {
		updates, err := p.Bot.bot.GetUpdatesWithContext(ctx, &gotgbot.GetUpdatesOpts{
			Offset:         offset,
			Timeout:        longPollTimeout,
			AllowedUpdates: []string{"callback_query", "message"},
			RequestOpts:    &gotgbot.RequestOpts{Timeout: requestTimeout},
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			p.Logger.Error(err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(retryDelay):
			}
			continue
		}

		for _, update := range updates {
			if chat, ok := p.allowed(update); !ok {
				p.Logger.Info("dropped update from an unknown chat", "chat_id", chat, "update_id", update.UpdateId)
			} else if err := p.Handler.Handle(ctx, update); err != nil {
				p.Logger.Error(err, "update_id", update.UpdateId)
			}
			if update.UpdateId >= offset {
				offset = update.UpdateId + 1
			}
		}

		if len(updates) > 0 {
			p.saveOffset(ctx, offset)
		}
	}
}

func (p *Poller) ensureNoWebhook(ctx context.Context) error {
	info, err := p.Bot.bot.GetWebhookInfoWithContext(ctx, nil)
	if err != nil {
		return fmt.Errorf("get webhook info: %w", err)
	}
	if info.Url == "" {
		return nil
	}
	p.Logger.Warn(fmt.Errorf("active webhook detected (%s) — deleting it to enable long polling", info.Url))
	if _, err := p.Bot.bot.DeleteWebhookWithContext(ctx, nil); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	p.Logger.Info("webhook deleted; long polling is now active")
	return nil
}

func (p *Poller) loadOffset(ctx context.Context) int64 {
	v, err := p.SettingsRepo.Get(ctx, config.KeyTelegramUpdateOffset)
	if err != nil || v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func (p *Poller) saveOffset(ctx context.Context, offset int64) {
	if err := p.SettingsRepo.Set(ctx, config.KeyTelegramUpdateOffset, strconv.FormatInt(offset, 10)); err != nil {
		p.Logger.Error(err)
	}
}

// allowed reports whether an update may reach the handler, and the chat it
// came from. An update with no chat at all — a type the poller never asked
// for — is dropped too.
func (p *Poller) allowed(u gotgbot.Update) (int64, bool) {
	chat, ok := updateChatID(u)
	if !ok {
		return 0, false
	}
	if p.AllowedChatID == 0 {
		return chat, true
	}
	return chat, chat == p.AllowedChatID
}

// updateChatID returns the chat an update came from. A message's chat is its
// own; a button press belongs to the chat holding the message it was pressed
// in, falling back to the presser when Telegram withholds the message.
func updateChatID(u gotgbot.Update) (int64, bool) {
	switch {
	case u.Message != nil:
		return u.Message.Chat.Id, true
	case u.CallbackQuery != nil:
		if u.CallbackQuery.Message != nil {
			return u.CallbackQuery.Message.GetChat().Id, true
		}
		return u.CallbackQuery.From.Id, true
	}
	return 0, false
}

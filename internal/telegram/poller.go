package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

const (
	offsetSettingKey = "telegram.update_offset"
	longPollTimeout  = 25               // seconds — Telegram long-poll window
	requestTimeout   = 30 * time.Second // HTTP timeout — must exceed longPollTimeout
	retryDelay       = 5 * time.Second
)

// Poller long-polls the Telegram Bot API for callback_query updates
// and dispatches each one to the Handler.
type Poller struct {
	Bot          *Bot
	Handler      *Handler
	SettingsRepo *repo.SettingsRepo
	Logger       log.Logger
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
			AllowedUpdates: []string{"callback_query"},
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
			if err := p.Handler.Handle(ctx, update); err != nil {
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
	v, err := p.SettingsRepo.Get(ctx, offsetSettingKey)
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
	if err := p.SettingsRepo.Set(ctx, offsetSettingKey, strconv.FormatInt(offset, 10)); err != nil {
		p.Logger.Error(err)
	}
}

package telegram

import (
	"context"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

func newOffsetPoller(t *testing.T) (*Poller, *repo.SettingsRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	sr := repo.NewSettingsRepo(sqlDB)
	return &Poller{SettingsRepo: sr, Logger: log.Noop{}}, sr
}

func TestPoller_Offset_RoundTrip(t *testing.T) {
	p, _ := newOffsetPoller(t)
	ctx := context.Background()

	assert.Equal(t, int64(0), p.loadOffset(ctx), "unset offset defaults to 0")

	p.saveOffset(ctx, 123456)
	assert.Equal(t, int64(123456), p.loadOffset(ctx))
}

func TestPoller_LoadOffset_MalformedValue(t *testing.T) {
	p, sr := newOffsetPoller(t)
	ctx := context.Background()
	require.NoError(t, sr.Set(ctx, config.KeyTelegramUpdateOffset, "not-a-number"))

	assert.Equal(t, int64(0), p.loadOffset(ctx), "a malformed stored offset falls back to 0")
}

func TestPoller_AllowedDropsOtherChats(t *testing.T) {
	p := &Poller{AllowedChats: map[int64]bool{1001: true}}
	mine := gotgbot.Update{Message: &gotgbot.Message{Chat: gotgbot.Chat{Id: 1001}, Text: "/important 3"}}
	stranger := gotgbot.Update{Message: &gotgbot.Message{Chat: gotgbot.Chat{Id: 4242}, Text: "/important 3"}}
	myButton := gotgbot.Update{CallbackQuery: &gotgbot.CallbackQuery{
		From: gotgbot.User{Id: 1001}, Message: &gotgbot.Message{Chat: gotgbot.Chat{Id: 1001}}, Data: "handled:abc"}}
	strangerButton := gotgbot.Update{CallbackQuery: &gotgbot.CallbackQuery{
		From: gotgbot.User{Id: 4242}, Message: &gotgbot.Message{Chat: gotgbot.Chat{Id: 4242}}, Data: "handled:abc"}}
	withheld := gotgbot.Update{CallbackQuery: &gotgbot.CallbackQuery{From: gotgbot.User{Id: 4242}, Data: "x"}}

	chat, ok := p.allowed(mine)
	assert.True(t, ok)
	assert.Equal(t, int64(1001), chat)
	_, ok = p.allowed(myButton)
	assert.True(t, ok)

	// Anyone who learns the bot's username can send it a message; the poller
	// must not let that reach the handler, whatever the text.
	chat, ok = p.allowed(stranger)
	assert.False(t, ok)
	assert.Equal(t, int64(4242), chat, "the dropped chat is reported for the log")
	_, ok = p.allowed(strangerButton)
	assert.False(t, ok)
	// A callback whose message Telegram withheld is judged by who pressed it.
	_, ok = p.allowed(withheld)
	assert.False(t, ok)

	// An update of a kind the poller never asked for carries no chat: dropped.
	_, ok = p.allowed(gotgbot.Update{})
	assert.False(t, ok)
}

func TestPoller_ZeroAllowedChatMeansUnfiltered(t *testing.T) {
	// The escape hatch tests use; production always sets the configured chat.
	p := &Poller{}
	_, ok := p.allowed(gotgbot.Update{Message: &gotgbot.Message{Chat: gotgbot.Chat{Id: 7}}})
	assert.True(t, ok)
}

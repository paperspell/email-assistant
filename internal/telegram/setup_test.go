package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBotClient is a gotgbot.BotClient that returns canned per-method responses
// instead of hitting the network, and records the params it was called with.
type fakeBotClient struct {
	responses  map[string]json.RawMessage
	errs       map[string]error
	lastParams map[string]map[string]any
}

func newFakeBotClient() *fakeBotClient {
	return &fakeBotClient{
		responses:  map[string]json.RawMessage{},
		errs:       map[string]error{},
		lastParams: map[string]map[string]any{},
	}
}

func (f *fakeBotClient) RequestWithContext(
	_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts,
) (json.RawMessage, error) {
	f.lastParams[method] = params
	if err := f.errs[method]; err != nil {
		return nil, err
	}
	return f.responses[method], nil
}

func (f *fakeBotClient) GetAPIURL(*gotgbot.RequestOpts) string               { return "https://api.telegram.org" }
func (f *fakeBotClient) FileURL(string, string, *gotgbot.RequestOpts) string { return "" }

// newTestSetupClient builds a SetupClient backed by fake, with getMe stubbed so
// the bot's username is populated on construction.
func newTestSetupClient(t *testing.T, fake *fakeBotClient) *SetupClient {
	t.Helper()
	if _, ok := fake.responses["getMe"]; !ok {
		fake.responses["getMe"] = json.RawMessage(
			`{"id":123,"is_bot":true,"first_name":"Test","username":"test_bot"}`)
	}
	bot, err := gotgbot.NewBot("123:abc", &gotgbot.BotOpts{BotClient: fake})
	require.NoError(t, err)
	return &SetupClient{bot: bot}
}

func TestSetupClient_Username(t *testing.T) {
	c := newTestSetupClient(t, newFakeBotClient())
	assert.Equal(t, "test_bot", c.Username())
}

func TestSetupClient_LatestChatID_ReturnsMostRecent(t *testing.T) {
	fake := newFakeBotClient()
	fake.responses["getUpdates"] = json.RawMessage(`[
		{"update_id":1,"message":{"message_id":1,"date":0,"chat":{"id":10,"type":"private"}}},
		{"update_id":2,"message":{"message_id":2,"date":0,"chat":{"id":42,"type":"private"}}}
	]`)
	c := newTestSetupClient(t, fake)

	id, err := c.LatestChatID()
	require.NoError(t, err)
	assert.Equal(t, int64(42), id, "the last message's chat id should win")
}

func TestSetupClient_LatestChatID_NoMessages(t *testing.T) {
	fake := newFakeBotClient()
	fake.responses["getUpdates"] = json.RawMessage(`[]`)
	c := newTestSetupClient(t, fake)

	id, err := c.LatestChatID()
	require.NoError(t, err)
	assert.Zero(t, id)
}

func TestSetupClient_LatestChatID_SkipsNonMessageUpdates(t *testing.T) {
	fake := newFakeBotClient()
	// An edited_message (not message) carries no fresh chat to notify.
	fake.responses["getUpdates"] = json.RawMessage(
		`[{"update_id":1,"edited_message":{"message_id":1,"date":0,"chat":{"id":10,"type":"private"}}}]`)
	c := newTestSetupClient(t, fake)

	id, err := c.LatestChatID()
	require.NoError(t, err)
	assert.Zero(t, id)
}

func TestSetupClient_LatestChatID_Error(t *testing.T) {
	fake := newFakeBotClient()
	fake.errs["getUpdates"] = errors.New("boom")
	c := newTestSetupClient(t, fake)

	_, err := c.LatestChatID()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get updates")
}

func TestSetupClient_SendVerification_OK(t *testing.T) {
	fake := newFakeBotClient()
	fake.responses["sendMessage"] = json.RawMessage(
		`{"message_id":1,"date":0,"chat":{"id":42,"type":"private"}}`)
	c := newTestSetupClient(t, fake)

	require.NoError(t, c.SendVerification(42))
	assert.Equal(t, int64(42), fake.lastParams["sendMessage"]["chat_id"])
	assert.Contains(t, fake.lastParams["sendMessage"]["text"], "verified")
}

func TestSetupClient_SendVerification_Error(t *testing.T) {
	fake := newFakeBotClient()
	fake.errs["sendMessage"] = errors.New("chat not found")
	c := newTestSetupClient(t, fake)

	err := c.SendVerification(42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send verification")
}

package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Valid(t *testing.T) {
	f := writeTemp(t, `
log_level: debug
dev_mode: true
db:
  path: test.db
account:
  name: Test
  email: user@example.com
  host: imap.example.com
  port: 993
  username: user@example.com
  password: secret
  tls: true
  poll_interval: 2m
telegram:
  bot_token: "token123"
  chat_id: 12345
`)
	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.True(t, cfg.DevMode)
	assert.Equal(t, "test.db", cfg.DB.Path)
	assert.Equal(t, "imap.example.com", cfg.Account.Host)
	assert.Equal(t, 2*time.Minute, cfg.Account.PollInterval)
	assert.Equal(t, "token123", cfg.Telegram.BotToken)
	assert.Equal(t, int64(12345), cfg.Telegram.ChatID)
}

func TestLoad_Defaults(t *testing.T) {
	f := writeTemp(t, `
account:
  host: imap.example.com
  username: user@example.com
  password: secret
  port: 993
telegram:
  bot_token: "token"
  chat_id: 1
`)
	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "email-agent.db", cfg.DB.Path)
	assert.Equal(t, time.Minute, cfg.Account.PollInterval)
	assert.True(t, cfg.Account.TLS)
}

func TestLoad_MissingHost(t *testing.T) {
	f := writeTemp(t, `
account:
  username: user@example.com
  password: secret
  port: 993
telegram:
  bot_token: "token"
  chat_id: 1
`)
	_, err := Load(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account.host")
}

func TestLoad_MissingToken(t *testing.T) {
	f := writeTemp(t, `
account:
  host: imap.example.com
  username: user@example.com
  password: secret
  port: 993
telegram:
  chat_id: 1
`)
	_, err := Load(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bot_token")
}

func TestLoad_EnvOverride(t *testing.T) {
	f := writeTemp(t, `
account:
  host: imap.example.com
  username: user@example.com
  password: original
  port: 993
telegram:
  bot_token: "original"
  chat_id: 1
`)
	t.Setenv("IMAP_PASSWORD", "overridden")
	t.Setenv("TELEGRAM_BOT_TOKEN", "newtoken")

	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "overridden", cfg.Account.Password)
	assert.Equal(t, "newtoken", cfg.Telegram.BotToken)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	require.Error(t, err)
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

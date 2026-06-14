package config

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
)

func setupRepo(t *testing.T, settings map[string]string) *repo.SettingsRepo {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	r := repo.NewSettingsRepo(sqlDB)
	for k, v := range settings {
		require.NoError(t, r.Set(context.Background(), k, v))
	}
	return r
}

func validSettings() map[string]string {
	return map[string]string{
		"account.imap.host":     "imap.example.com",
		"account.imap.port":     "993",
		"account.imap.username": "user@example.com",
		"account.imap.password": "secret",
		"account.imap.tls":      "true",
		"account.poll_interval": "1m",
		"telegram.bot_token":    "token123",
		"telegram.chat_id":      "12345",
	}
}

func TestLoad_Valid(t *testing.T) {
	s := validSettings()
	s["log.level"] = "debug"
	s["dev_mode"] = "true"
	s["account.name"] = "My Account"
	s["account.email"] = "user@example.com"
	s["account.poll_interval"] = "2m"

	r := setupRepo(t, s)
	cfg, err := Load(context.Background(), r)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.LogLevel)
	assert.True(t, cfg.DevMode)
	assert.Equal(t, "imap.example.com", cfg.Account.Host)
	assert.Equal(t, 993, cfg.Account.Port)
	assert.Equal(t, 2*time.Minute, cfg.Account.PollInterval)
	assert.Equal(t, "token123", cfg.Telegram.BotToken)
	assert.Equal(t, int64(12345), cfg.Telegram.ChatID)
}

func TestLoad_Defaults(t *testing.T) {
	r := setupRepo(t, validSettings())
	cfg, err := Load(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.False(t, cfg.DevMode)
	assert.True(t, cfg.Account.TLS)
	assert.Equal(t, time.Minute, cfg.Account.PollInterval)
}

func TestLoad_Empty_ReturnsError(t *testing.T) {
	r := setupRepo(t, map[string]string{})
	_, err := Load(context.Background(), r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init")
}

func TestLoad_MissingHost(t *testing.T) {
	s := validSettings()
	delete(s, "account.imap.host")
	r := setupRepo(t, s)
	_, err := Load(context.Background(), r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account.imap.host")
}

func TestLoad_MissingToken(t *testing.T) {
	s := validSettings()
	delete(s, "telegram.bot_token")
	r := setupRepo(t, s)
	_, err := Load(context.Background(), r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bot_token")
}

func TestLoad_ZeroChatID(t *testing.T) {
	s := validSettings()
	s["telegram.chat_id"] = "0"
	r := setupRepo(t, s)
	_, err := Load(context.Background(), r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat_id")
}

func TestLoad_ZeroPollInterval(t *testing.T) {
	s := validSettings()
	s["account.poll_interval"] = "0s"
	r := setupRepo(t, s)
	_, err := Load(context.Background(), r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll_interval")
}

func TestLoad_EnvOverride_LogLevel(t *testing.T) {
	r := setupRepo(t, validSettings())
	t.Setenv("LOG_LEVEL", "warn")
	cfg, err := Load(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "warn", cfg.LogLevel)
}

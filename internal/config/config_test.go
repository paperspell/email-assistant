package config

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
)

func setupRepos(
	t *testing.T, settings map[string]string, accounts []domain.Account,
) (*repo.SettingsRepo, *repo.AccountRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	sr := repo.NewSettingsRepo(sqlDB)
	for k, v := range settings {
		require.NoError(t, sr.Set(context.Background(), k, v))
	}
	ar := repo.NewAccountRepo(sqlDB)
	for _, a := range accounts {
		require.NoError(t, ar.Upsert(context.Background(), a))
	}
	return sr, ar
}

func validSettings() map[string]string {
	return map[string]string{
		"telegram.bot_token": "token123",
		"telegram.chat_id":   "12345",
	}
}

func validAccount() domain.Account {
	return domain.Account{
		ID:           "user@example.com",
		Name:         "My Account",
		Email:        "user@example.com",
		Host:         "imap.example.com",
		Port:         993,
		Username:     "user@example.com",
		Password:     "secret",
		TLS:          true,
		PollInterval: time.Minute,
		Enabled:      true,
	}
}

func TestLoad_Valid(t *testing.T) {
	s := validSettings()
	s["log.level"] = "debug"
	s["dev_mode"] = "true"
	acc := validAccount()
	acc.PollInterval = 2 * time.Minute

	sr, ar := setupRepos(t, s, []domain.Account{acc})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.LogLevel)
	assert.True(t, cfg.DevMode)
	require.Len(t, cfg.Accounts, 1)
	assert.Equal(t, "imap.example.com", cfg.Accounts[0].Host)
	assert.Equal(t, 993, cfg.Accounts[0].Port)
	assert.Equal(t, 2*time.Minute, cfg.Accounts[0].PollInterval)
	assert.Equal(t, "token123", cfg.Telegram.BotToken)
	assert.Equal(t, int64(12345), cfg.Telegram.ChatID)
}

func TestLoad_Defaults(t *testing.T) {
	sr, ar := setupRepos(t, validSettings(), []domain.Account{validAccount()})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.False(t, cfg.DevMode)
	require.Len(t, cfg.Accounts, 1)
	assert.True(t, cfg.Accounts[0].TLS)
	assert.Equal(t, time.Minute, cfg.Accounts[0].PollInterval)
}

func TestLoad_PollDefaultInterval_DefaultsTo10m(t *testing.T) {
	sr, ar := setupRepos(t, validSettings(), []domain.Account{validAccount()})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	assert.Equal(t, DefaultPollInterval, cfg.Poll.DefaultInterval)
	assert.Equal(t, 10*time.Minute, cfg.Poll.DefaultInterval)
}

func TestLoad_PollDefaultInterval_FromSetting(t *testing.T) {
	s := validSettings()
	s[KeyPollDefaultInterval] = "15m"
	sr, ar := setupRepos(t, s, []domain.Account{validAccount()})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, cfg.Poll.DefaultInterval)
}

func TestLoad_PollDefaultInterval_InvalidFallsBack(t *testing.T) {
	s := validSettings()
	s[KeyPollDefaultInterval] = "not-a-duration"
	sr, ar := setupRepos(t, s, []domain.Account{validAccount()})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	assert.Equal(t, DefaultPollInterval, cfg.Poll.DefaultInterval)
}

func TestPollIntervalOrDefault(t *testing.T) {
	assert.Equal(t, DefaultPollInterval, PollIntervalOrDefault(""))
	assert.Equal(t, DefaultPollInterval, PollIntervalOrDefault("garbage"))
	assert.Equal(t, DefaultPollInterval, PollIntervalOrDefault("0s"))
	assert.Equal(t, DefaultPollInterval, PollIntervalOrDefault("-5m"))
	assert.Equal(t, 30*time.Second, PollIntervalOrDefault("30s"))
	assert.Equal(t, 20*time.Minute, PollIntervalOrDefault("20m"))
}

func TestLoad_OnlyEnabledAccounts(t *testing.T) {
	disabled := validAccount()
	disabled.ID = "off@example.com"
	disabled.Email = "off@example.com"
	disabled.Enabled = false

	sr, ar := setupRepos(t, validSettings(), []domain.Account{validAccount(), disabled})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	require.Len(t, cfg.Accounts, 1)
	assert.Equal(t, "user@example.com", cfg.Accounts[0].Email)
}

func TestLoad_Empty_ReturnsError(t *testing.T) {
	sr, ar := setupRepos(t, map[string]string{}, nil)
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init")
}

func TestLoad_NoAccounts(t *testing.T) {
	sr, ar := setupRepos(t, validSettings(), nil)
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account")
}

func TestLoad_AccountMissingHost(t *testing.T) {
	acc := validAccount()
	acc.Host = ""
	sr, ar := setupRepos(t, validSettings(), []domain.Account{acc})
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host")
}

func TestLoad_MissingToken(t *testing.T) {
	s := validSettings()
	delete(s, "telegram.bot_token")
	sr, ar := setupRepos(t, s, []domain.Account{validAccount()})
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bot_token")
}

func TestLoad_ZeroChatID(t *testing.T) {
	s := validSettings()
	s["telegram.chat_id"] = "0"
	sr, ar := setupRepos(t, s, []domain.Account{validAccount()})
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat_id")
}

func TestLoad_ZeroPollInterval(t *testing.T) {
	acc := validAccount()
	acc.PollInterval = 0
	sr, ar := setupRepos(t, validSettings(), []domain.Account{acc})
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll_interval")
}

func oauthAccount() domain.Account {
	a := validAccount()
	a.ID = "g@example.com"
	a.Email = "g@example.com"
	a.Host = "imap.gmail.com"
	a.AuthType = domain.AuthOAuth
	a.Password = ""
	a.OAuthRefreshToken = "refresh-token"
	return a
}

func TestLoad_OAuthAccount_RequiresClientCreds(t *testing.T) {
	// OAuth account present but no global Google client configured.
	sr, ar := setupRepos(t, validSettings(), []domain.Account{oauthAccount()})
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oauth.google")
}

func TestLoad_OAuthAccount_Valid(t *testing.T) {
	s := validSettings()
	s["oauth.google.client_id"] = "cid"
	s["oauth.google.client_secret"] = "csecret"
	sr, ar := setupRepos(t, s, []domain.Account{oauthAccount()})
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	require.Len(t, cfg.Accounts, 1)
	assert.Equal(t, domain.AuthOAuth, cfg.Accounts[0].AuthType)
	assert.Equal(t, "cid", cfg.OAuth.GoogleClientID)
	assert.Equal(t, "csecret", cfg.OAuth.GoogleClientSecret)
}

func TestLoad_OAuthAccount_MissingRefreshToken(t *testing.T) {
	s := validSettings()
	s["oauth.google.client_id"] = "cid"
	s["oauth.google.client_secret"] = "csecret"
	acc := oauthAccount()
	acc.OAuthRefreshToken = ""
	sr, ar := setupRepos(t, s, []domain.Account{acc})
	_, err := Load(context.Background(), sr, ar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh token")
}

func TestLoad_PasswordOnly_NoClientCredsNeeded(t *testing.T) {
	// A password-only config stays valid without any OAuth settings.
	sr, ar := setupRepos(t, validSettings(), []domain.Account{validAccount()})
	_, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
}

func TestLoad_EnvOverride_LogLevel(t *testing.T) {
	sr, ar := setupRepos(t, validSettings(), []domain.Account{validAccount()})
	t.Setenv("LOG_LEVEL", "warn")
	cfg, err := Load(context.Background(), sr, ar)
	require.NoError(t, err)
	assert.Equal(t, "warn", cfg.LogLevel)
}

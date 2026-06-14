package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/paperspell/email-assistant/internal/db/repo"
)

// Config holds all application settings loaded from the database.
type Config struct {
	LogLevel     string
	DevMode      bool
	Account      IMAPAccount
	Telegram     TelegramConfig
	Notification NotificationConfig
}

// NotificationConfig controls when notifications are sent.
type NotificationConfig struct {
	MinImportance string // "critical", "important", "maybe" — default "important"
}

// IMAPAccount holds configuration for one IMAP account.
type IMAPAccount struct {
	Name         string
	Email        string
	Host         string
	Port         int
	Username     string
	Password     string
	TLS          bool
	PollInterval time.Duration
}

// TelegramConfig holds Telegram bot configuration.
type TelegramConfig struct {
	BotToken string
	ChatID   int64
}

// KnownKeys is the set of all valid settings keys.
var KnownKeys = map[string]bool{
	"account.name":                true,
	"account.email":               true,
	"account.imap.host":           true,
	"account.imap.port":           true,
	"account.imap.username":       true,
	"account.imap.password":       true,
	"account.imap.tls":            true,
	"account.poll_interval":       true,
	"telegram.bot_token":          true,
	"telegram.chat_id":            true,
	"notification.min_importance": true,
	"log.level":                   true,
	"dev_mode":                    true,
}

// Load reads all settings from the database and returns a validated Config.
func Load(ctx context.Context, r *repo.SettingsRepo) (*Config, error) {
	all, err := r.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("database not initialized — run 'email-agent init' first")
	}

	cfg := defaults()
	applySettings(cfg, all)
	applyEnvOverrides(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		LogLevel: "info",
		Account: IMAPAccount{
			Port:         993,
			TLS:          true,
			PollInterval: time.Minute,
		},
		Notification: NotificationConfig{
			MinImportance: "important",
		},
	}
}

func applySettings(cfg *Config, s map[string]string) {
	if v := s["log.level"]; v != "" {
		cfg.LogLevel = v
	}
	if v := s["dev_mode"]; v != "" {
		cfg.DevMode = v == "true"
	}
	if v := s["account.name"]; v != "" {
		cfg.Account.Name = v
	}
	if v := s["account.email"]; v != "" {
		cfg.Account.Email = v
	}
	if v := s["account.imap.host"]; v != "" {
		cfg.Account.Host = v
	}
	if v := s["account.imap.port"]; v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Account.Port = p
		}
	}
	if v := s["account.imap.username"]; v != "" {
		cfg.Account.Username = v
	}
	if v := s["account.imap.password"]; v != "" {
		cfg.Account.Password = v
	}
	if v := s["account.imap.tls"]; v != "" {
		cfg.Account.TLS = v == "true"
	}
	if v := s["account.poll_interval"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Account.PollInterval = d
		}
	}
	if v := s["telegram.bot_token"]; v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := s["telegram.chat_id"]; v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Telegram.ChatID = id
		}
	}
	if v := s["notification.min_importance"]; v != "" {
		cfg.Notification.MinImportance = v
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}

func (c *Config) validate() error {
	if c.Account.Host == "" {
		return fmt.Errorf("config: account.imap.host is required")
	}
	if c.Account.Username == "" {
		return fmt.Errorf("config: account.imap.username is required")
	}
	if c.Account.Password == "" {
		return fmt.Errorf("config: account.imap.password is required")
	}
	if c.Account.Port == 0 {
		return fmt.Errorf("config: account.imap.port is required")
	}
	if c.Account.PollInterval <= 0 {
		return fmt.Errorf("config: account.poll_interval must be positive")
	}
	if c.Telegram.BotToken == "" {
		return fmt.Errorf("config: telegram.bot_token is required")
	}
	if c.Telegram.ChatID == 0 {
		return fmt.Errorf("config: telegram.chat_id is required")
	}
	return nil
}

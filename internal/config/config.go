package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	LogLevel string         `yaml:"log_level"`
	DevMode  bool           `yaml:"dev_mode"`
	DB       DBConfig       `yaml:"db"`
	Account  IMAPAccount    `yaml:"account"`
	Telegram TelegramConfig `yaml:"telegram"`
}

// DBConfig holds database configuration.
type DBConfig struct {
	Path string `yaml:"path"`
}

// IMAPAccount holds configuration for one IMAP account.
type IMAPAccount struct {
	Name         string        `yaml:"name"`
	Email        string        `yaml:"email"`
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	TLS          bool          `yaml:"tls"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

// TelegramConfig holds Telegram bot configuration.
type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   int64  `yaml:"chat_id"`
}

// Load reads the config file at path and applies environment variable overrides.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyEnvOverrides(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		LogLevel: "info",
		DB: DBConfig{
			Path: "email-agent.db",
		},
		Account: IMAPAccount{
			Port:         993,
			TLS:          true,
			PollInterval: time.Minute,
		},
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DB.Path = v
	}
	if v := os.Getenv("IMAP_PASSWORD"); v != "" {
		cfg.Account.Password = v
	}
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
}

func (c *Config) validate() error {
	if c.Account.Host == "" {
		return fmt.Errorf("config: account.host is required")
	}
	if c.Account.Username == "" {
		return fmt.Errorf("config: account.username is required")
	}
	if c.Account.Password == "" {
		return fmt.Errorf("config: account.password is required (or set IMAP_PASSWORD env var)")
	}
	if c.Account.Port == 0 {
		return fmt.Errorf("config: account.port is required")
	}
	if c.Account.PollInterval <= 0 {
		return fmt.Errorf("config: account.poll_interval must be positive")
	}
	if c.Telegram.BotToken == "" {
		return fmt.Errorf("config: telegram.bot_token is required (or set TELEGRAM_BOT_TOKEN env var)")
	}
	if c.Telegram.ChatID == 0 {
		return fmt.Errorf("config: telegram.chat_id is required")
	}
	return nil
}

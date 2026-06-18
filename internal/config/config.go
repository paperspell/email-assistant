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
	LLM          LLMConfig
	Content      ContentConfig
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

// LLMConfig controls optional LLM-based classification.
type LLMConfig struct {
	Provider            string // "anthropic" | "openai" | "" (disabled)
	AnthropicAPIKey     string
	OpenAIAPIKey        string
	Model               string // empty means use provider default
	ScoreDivergenceWarn int    // log WARN when |llm_score - rule_score| >= this
}

// ContentConfig controls what content is sent to the LLM.
type ContentConfig struct {
	Mode string // "headers_only" | "redacted_body" | "full_body" — default "headers_only"
}

// DefaultValues returns the default display string for each setting that has one.
// Values are derived from defaults() so they always match what the application uses.
func DefaultValues() map[string]string {
	d := defaults()
	return map[string]string{
		KeyLogLevel:                  d.LogLevel,
		KeyAccountPort:               strconv.Itoa(d.Account.Port),
		KeyAccountTLS:                strconv.FormatBool(d.Account.TLS),
		KeyAccountPollInterval:       d.Account.PollInterval.String(),
		KeyNotificationMinImportance: d.Notification.MinImportance,
		KeyLLMScoreDivergenceWarn:    strconv.Itoa(d.LLM.ScoreDivergenceWarn),
		KeyContentMode:               d.Content.Mode,
	}
}

// KnownKeys is the set of all valid settings keys.
var KnownKeys = map[string]bool{
	KeyAccountName:               true,
	KeyAccountEmail:              true,
	KeyAccountHost:               true,
	KeyAccountPort:               true,
	KeyAccountUsername:           true,
	KeyAccountPassword:           true,
	KeyAccountTLS:                true,
	KeyAccountPollInterval:       true,
	KeyTelegramBotToken:          true,
	KeyTelegramChatID:            true,
	KeyTelegramUpdateOffset:      true,
	KeyNotificationMinImportance: true,
	KeyLLMProvider:               true,
	KeyLLMAnthropicAPIKey:        true,
	KeyLLMOpenAIAPIKey:           true,
	KeyLLMModel:                  true,
	KeyLLMScoreDivergenceWarn:    true,
	KeyContentMode:               true,
	KeyLogLevel:                  true,
	KeyDevMode:                   true,
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
		LLM: LLMConfig{
			ScoreDivergenceWarn: 30,
		},
		Content: ContentConfig{
			Mode: "headers_only",
		},
	}
}

func applySettings(cfg *Config, s map[string]string) {
	if v := s[KeyLogLevel]; v != "" {
		cfg.LogLevel = v
	}
	if v := s[KeyDevMode]; v != "" {
		cfg.DevMode = v == "true"
	}
	if v := s[KeyAccountName]; v != "" {
		cfg.Account.Name = v
	}
	if v := s[KeyAccountEmail]; v != "" {
		cfg.Account.Email = v
	}
	if v := s[KeyAccountHost]; v != "" {
		cfg.Account.Host = v
	}
	if v := s[KeyAccountPort]; v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Account.Port = p
		}
	}
	if v := s[KeyAccountUsername]; v != "" {
		cfg.Account.Username = v
	}
	if v := s[KeyAccountPassword]; v != "" {
		cfg.Account.Password = v
	}
	if v := s[KeyAccountTLS]; v != "" {
		cfg.Account.TLS = v == "true"
	}
	if v := s[KeyAccountPollInterval]; v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Account.PollInterval = d
		}
	}
	if v := s[KeyTelegramBotToken]; v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := s[KeyTelegramChatID]; v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Telegram.ChatID = id
		}
	}
	if v := s[KeyNotificationMinImportance]; v != "" {
		cfg.Notification.MinImportance = v
	}
	if v := s[KeyLLMProvider]; v != "" {
		cfg.LLM.Provider = v
	}
	if v := s[KeyLLMAnthropicAPIKey]; v != "" {
		cfg.LLM.AnthropicAPIKey = v
	}
	if v := s[KeyLLMOpenAIAPIKey]; v != "" {
		cfg.LLM.OpenAIAPIKey = v
	}
	if v := s[KeyLLMModel]; v != "" {
		cfg.LLM.Model = v
	}
	if v := s[KeyLLMScoreDivergenceWarn]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LLM.ScoreDivergenceWarn = n
		}
	}
	if v := s[KeyContentMode]; v != "" {
		cfg.Content.Mode = v
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}

func (c *Config) validate() error {
	if c.Account.Host == "" {
		return fmt.Errorf("config: %s is required", KeyAccountHost)
	}
	if c.Account.Username == "" {
		return fmt.Errorf("config: %s is required", KeyAccountUsername)
	}
	if c.Account.Password == "" {
		return fmt.Errorf("config: %s is required", KeyAccountPassword)
	}
	if c.Account.Port == 0 {
		return fmt.Errorf("config: %s is required", KeyAccountPort)
	}
	if c.Account.PollInterval <= 0 {
		return fmt.Errorf("config: %s must be positive", KeyAccountPollInterval)
	}
	if c.Telegram.BotToken == "" {
		return fmt.Errorf("config: %s is required", KeyTelegramBotToken)
	}
	if c.Telegram.ChatID == 0 {
		return fmt.Errorf("config: %s is required", KeyTelegramChatID)
	}
	switch c.Content.Mode {
	case "", "headers_only", "redacted_body", "full_body":
		// valid
	default:
		return fmt.Errorf("config: unknown %s %q", KeyContentMode, c.Content.Mode)
	}
	// LLM config is optional; only validate when a provider is set
	if c.LLM.Provider == "anthropic" && c.LLM.AnthropicAPIKey == "" {
		return fmt.Errorf("config: %s is required when provider is anthropic", KeyLLMAnthropicAPIKey)
	}
	if c.LLM.Provider == "openai" && c.LLM.OpenAIAPIKey == "" {
		return fmt.Errorf("config: %s is required when provider is openai", KeyLLMOpenAIAPIKey)
	}
	return nil
}

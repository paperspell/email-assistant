package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
)

// DefaultPollInterval is the scan interval seeded for new accounts when no
// global poll.default_interval setting is configured.
const DefaultPollInterval = 10 * time.Minute

// DefaultDigestTime is the time of day (account timezone) the daily digest is
// sent when no digest.time setting or per-account override is configured.
const DefaultDigestTime = "20:00"

// Config holds all application settings loaded from the database.
type Config struct {
	LogLevel     string
	DevMode      bool
	Accounts     []domain.Account
	Telegram     TelegramConfig
	Notification NotificationConfig
	LLM          LLMConfig
	Content      ContentConfig
	OAuth        OAuthConfig
	Poll         PollConfig
	Filter       FilterConfig
	Digest       DigestConfig
}

// DigestConfig controls the daily digest schedule.
type DigestConfig struct {
	// Time is the global default send time, "HH:MM" in Location. Accounts may
	// override it via accounts.digest_time.
	Time string
	// Location is the timezone the digest time and day boundaries are computed in.
	Location *time.Location
}

// FilterConfig controls the mechanical filtering layer.
type FilterConfig struct {
	// BaselineFloor is the importance level at or below which the rule-based
	// scorer drops mail before the LLM runs. Default LevelMaybe.
	BaselineFloor domain.ImportanceLevel
}

// PollConfig holds polling defaults. The default interval seeds new accounts;
// each account stores and may override its own interval in the accounts table.
type PollConfig struct {
	DefaultInterval time.Duration
}

// OAuthConfig holds the global Google OAuth client credentials shared by all
// OAuth accounts. Per-account tokens live on the account record.
type OAuthConfig struct {
	GoogleClientID     string
	GoogleClientSecret string
}

// NotificationConfig controls when notifications are sent.
type NotificationConfig struct {
	MinImportance string // "critical", "important", "maybe" — default "important"
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
		KeyNotificationMinImportance: d.Notification.MinImportance,
		KeyLLMScoreDivergenceWarn:    strconv.Itoa(d.LLM.ScoreDivergenceWarn),
		KeyContentMode:               d.Content.Mode,
		KeyPollDefaultInterval:       d.Poll.DefaultInterval.String(),
		KeyFilterBaselineFloor:       string(d.Filter.BaselineFloor),
		KeyDigestTime:                d.Digest.Time,
		KeyDigestTimezone:            "Local",
	}
}

// PollIntervalOrDefault parses a Go duration string, falling back to
// DefaultPollInterval when empty or invalid. Shared by config loading and the
// CLI prompt so they agree on what "the default" is.
func PollIntervalOrDefault(s string) time.Duration {
	if s == "" {
		return DefaultPollInterval
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return DefaultPollInterval
	}
	return d
}

// ParseDigestTime parses an "HH:MM" 24-hour time, returning the hour and minute.
func ParseDigestTime(s string) (hour, minute int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return hour, minute, nil
}

// KnownKeys is the set of all valid settings keys. Account fields are stored in
// the accounts table (managed via the `account` subcommands), not here.
var KnownKeys = map[string]bool{
	KeyOAuthGoogleClientID:       true,
	KeyOAuthGoogleClientSecret:   true,
	KeyTelegramBotToken:          true,
	KeyTelegramChatID:            true,
	KeyTelegramUpdateOffset:      true,
	KeyNotificationMinImportance: true,
	KeyPollDefaultInterval:       true,
	KeyFilterBaselineFloor:       true,
	KeyDigestTime:                true,
	KeyDigestTimezone:            true,
	KeyLLMProvider:               true,
	KeyLLMAnthropicAPIKey:        true,
	KeyLLMOpenAIAPIKey:           true,
	KeyLLMModel:                  true,
	KeyLLMScoreDivergenceWarn:    true,
	KeyContentMode:               true,
	KeyLogLevel:                  true,
	KeyDevMode:                   true,
}

// Load reads settings and enabled accounts from the database and returns a
// validated Config.
func Load(ctx context.Context, r *repo.SettingsRepo, a *repo.AccountRepo) (*Config, error) {
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

	cfg.Accounts, err = a.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("load accounts: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		LogLevel: "info",
		Notification: NotificationConfig{
			MinImportance: "important",
		},
		LLM: LLMConfig{
			ScoreDivergenceWarn: 30,
		},
		Content: ContentConfig{
			Mode: "headers_only",
		},
		Poll: PollConfig{
			DefaultInterval: DefaultPollInterval,
		},
		Filter: FilterConfig{
			BaselineFloor: domain.LevelMaybe,
		},
		Digest: DigestConfig{
			Time:     DefaultDigestTime,
			Location: time.Local,
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
	if v := s[KeyPollDefaultInterval]; v != "" {
		cfg.Poll.DefaultInterval = PollIntervalOrDefault(v)
	}
	if v := s[KeyFilterBaselineFloor]; v != "" {
		cfg.Filter.BaselineFloor = domain.ImportanceLevel(v)
	}
	if v := s[KeyDigestTime]; v != "" {
		cfg.Digest.Time = v
	}
	if v := s[KeyDigestTimezone]; v != "" {
		if loc, err := time.LoadLocation(v); err == nil {
			cfg.Digest.Location = loc
		}
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
	if v := s[KeyOAuthGoogleClientID]; v != "" {
		cfg.OAuth.GoogleClientID = v
	}
	if v := s[KeyOAuthGoogleClientSecret]; v != "" {
		cfg.OAuth.GoogleClientSecret = v
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}

func (c *Config) validate() error {
	if len(c.Accounts) == 0 {
		return fmt.Errorf("config: no enabled accounts configured — run 'email-agent account add'")
	}
	if c.Poll.DefaultInterval <= 0 {
		return fmt.Errorf("config: %s must be a positive duration", KeyPollDefaultInterval)
	}
	switch c.Filter.BaselineFloor {
	case domain.LevelIgnore, domain.LevelMaybe, domain.LevelImportant, domain.LevelCritical:
		// valid
	default:
		return fmt.Errorf("config: unknown %s %q", KeyFilterBaselineFloor, c.Filter.BaselineFloor)
	}
	if _, _, err := ParseDigestTime(c.Digest.Time); err != nil {
		return fmt.Errorf("config: invalid %s: %w", KeyDigestTime, err)
	}
	hasOAuthAccount := false
	for _, acc := range c.Accounts {
		if acc.Host == "" || acc.Username == "" {
			return fmt.Errorf("config: account %q is missing host or username", acc.Email)
		}
		switch acc.AuthType {
		case domain.AuthOAuth:
			hasOAuthAccount = true
			if acc.OAuthRefreshToken == "" {
				return fmt.Errorf("config: OAuth account %q has no refresh token — run 'email-agent account add'", acc.Email)
			}
		default: // password
			if acc.Password == "" {
				return fmt.Errorf("config: account %q is missing password", acc.Email)
			}
		}
		if acc.Port == 0 {
			return fmt.Errorf("config: account %q has invalid port", acc.Email)
		}
		if acc.PollInterval <= 0 {
			return fmt.Errorf("config: account %q has non-positive poll_interval", acc.Email)
		}
		if acc.DigestTime != "" {
			if _, _, err := ParseDigestTime(acc.DigestTime); err != nil {
				return fmt.Errorf("config: account %q has invalid digest_time: %w", acc.Email, err)
			}
		}
	}
	if hasOAuthAccount && (c.OAuth.GoogleClientID == "" || c.OAuth.GoogleClientSecret == "") {
		return fmt.Errorf("config: %s and %s are required for OAuth accounts — run 'email-agent init oauth'",
			KeyOAuthGoogleClientID, KeyOAuthGoogleClientSecret)
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

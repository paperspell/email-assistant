package config

// Settings key constants — every place that reads or writes a setting must use
// these rather than raw string literals so that renames are caught at compile time.
const (
	KeyLogLevel = "log.level"
	KeyDevMode  = "dev_mode"

	// Account fields live in the accounts table (see internal/db/repo/account_repo.go),
	// managed via the `account` subcommands, not the settings key/value store.

	KeyTelegramBotToken     = "telegram.bot_token"
	KeyTelegramChatID       = "telegram.chat_id"
	KeyTelegramUpdateOffset = "telegram.update_offset"

	KeyNotificationMinImportance = "notification.min_importance"
	// KeyNotificationLanguage is the language summaries are written in, e.g.
	// "Russian". Empty keeps the model's default (English).
	KeyNotificationLanguage = "notification.language"

	// KeyPollDefaultInterval is the default scan interval seeded for new accounts.
	// Each account stores its own interval in the accounts table and may override it.
	KeyPollDefaultInterval = "poll.default_interval"

	// KeyFilterBaselineFloor is the importance level at or below which the baseline
	// scorer drops mail without invoking the LLM. Default "maybe".
	KeyFilterBaselineFloor = "filter.baseline_floor"

	// KeyDigestTime is the global daily digest send time ("HH:MM"). Default 20:00.
	KeyDigestTime = "digest.time"
	// KeyDigestTimezone is the timezone for digest scheduling (IANA name or
	// "Local"/"UTC"). Default: system local time.
	KeyDigestTimezone = "digest.timezone"

	KeyLLMProvider            = "llm.provider"
	KeyLLMAnthropicAPIKey     = "llm.anthropic.api_key"
	KeyLLMOpenAIAPIKey        = "llm.openai.api_key"
	KeyLLMModel               = "llm.model"
	KeyLLMScoreDivergenceWarn = "llm.score_divergence_warn"

	KeyContentMode = "content.mode"

	KeyOAuthGoogleClientID     = "oauth.google.client_id"
	KeyOAuthGoogleClientSecret = "oauth.google.client_secret"
)

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

	KeyLLMProvider            = "llm.provider"
	KeyLLMAnthropicAPIKey     = "llm.anthropic.api_key"
	KeyLLMOpenAIAPIKey        = "llm.openai.api_key"
	KeyLLMModel               = "llm.model"
	KeyLLMScoreDivergenceWarn = "llm.score_divergence_warn"

	KeyContentMode = "content.mode"
)

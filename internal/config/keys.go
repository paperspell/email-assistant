package config

// Settings key constants — every place that reads or writes a setting must use
// these rather than raw string literals so that renames are caught at compile time.
const (
	KeyLogLevel = "log.level"
	KeyDevMode  = "dev_mode"

	KeyAccountName         = "account.name"
	KeyAccountEmail        = "account.email"
	KeyAccountHost         = "account.imap.host"
	KeyAccountPort         = "account.imap.port"
	KeyAccountUsername     = "account.imap.username"
	KeyAccountPassword     = "account.imap.password"
	KeyAccountTLS          = "account.imap.tls"
	KeyAccountPollInterval = "account.poll_interval"

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

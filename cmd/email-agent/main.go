package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
	"github.com/paperspell/email-assistant/internal/auth/oauth"
	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/digest"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/filter"
	"github.com/paperspell/email-assistant/internal/i18n"
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/scheduler"
	"github.com/paperspell/email-assistant/internal/telegram"

	imapmail "github.com/paperspell/email-assistant/internal/email/imap"
	llmanthropic "github.com/paperspell/email-assistant/internal/llm/anthropic"
	llmgemini "github.com/paperspell/email-assistant/internal/llm/gemini"
	llmopenai "github.com/paperspell/email-assistant/internal/llm/openai"
)

var version = "dev"

func main() {
	var dbPath string

	root := &cobra.Command{
		Use:   "email-agent",
		Short: "Local-first email monitoring daemon",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	root.PersistentFlags().StringVar(&dbPath, "db", "", "path to database file (default: ~/.email-agent/email-agent.db)")

	var localDev bool
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the email monitoring daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemon(cmd.Context(), resolveDBPath(dbPath), localDev)
		},
	}
	runCmd.Flags().BoolVar(&localDev, "local-dev", false,
		"human-readable coloured logs at debug level (overrides DB settings)")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("email-agent", version)
		},
	}

	root.AddCommand(
		runCmd, versionCmd,
		newInitCmd(&dbPath), newConfigCmd(&dbPath), newAuditCmd(&dbPath), newAccountCmd(&dbPath),
		newRulesCmd(&dbPath), newClausesCmd(&dbPath), newDigestCmd(&dbPath), newServiceCmd(),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	err := root.ExecuteContext(ctx)
	stop()
	if err != nil {
		os.Exit(1)
	}
}

func runDaemon(ctx context.Context, path string, localDev bool) error {
	hexKey, err := keychain.Load()
	if err != nil {
		return err
	}

	sqlDB, err := db.Open(path, hexKey)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.FromContext(ctx).Error(err)
		}
	}()

	if err := db.Migrate(ctx, sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	settingsRepo := repo.NewSettingsRepo(sqlDB)
	accountRepo := repo.NewAccountRepo(sqlDB)

	cfg, err := config.Load(ctx, settingsRepo, accountRepo)
	if err != nil {
		return err
	}

	logCfg := log.LoggerConfig{Dev: cfg.DevMode, Level: cfg.LogLevel}
	if localDev {
		logCfg.Dev = true
		logCfg.Level = log.LevelDebug
	}
	logger := log.NewLogger(logCfg)
	ctx = log.IntoContext(ctx, logger)
	logger.Info("email-agent starting", "version", version)

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)
	classificationRepo := repo.NewClassificationRepo(sqlDB)
	auditRepo := repo.NewAuditRepo(sqlDB)
	senderRepo := repo.NewSenderRepo(sqlDB)
	domainRepo := repo.NewDomainRepo(sqlDB)
	ruleRepo := repo.NewRuleRepo(sqlDB)
	clauseRepo := repo.NewClauseRepo(sqlDB)
	digestRepo := repo.NewDigestRepo(sqlDB)
	pendingRepo := repo.NewPendingRepo(sqlDB)

	importanceFilter := importance.NewFilter(senderRepo, domainRepo)
	ruleEngine := filter.NewEngine()

	// One setting drives both halves of the user's language: the Telegram text
	// the bot writes itself, and the language it asks the LLM to summarise in.
	locale := i18n.ResolveLocale(cfg.Notification.Language)
	printer, err := i18n.NewPrinter(locale)
	if err != nil {
		return fmt.Errorf("load translations: %w", err)
	}
	logger.Info("notification language", "setting", cfg.Notification.Language, "locale", locale)

	bot, err := telegram.NewBot(cfg.Telegram.BotToken, cfg.Telegram.ChatID, printer)
	if err != nil {
		return fmt.Errorf("create telegram bot: %w", err)
	}

	var llmProvider llm.Provider
	switch cfg.LLM.Provider {
	case "anthropic":
		llmProvider = llmanthropic.New(cfg.LLM.AnthropicAPIKey, cfg.LLM.Model)
		logger.Info("LLM provider: anthropic", "model", cfg.LLM.Model)
	case "openai":
		llmProvider = llmopenai.New(cfg.LLM.OpenAIAPIKey, cfg.LLM.Model)
		logger.Info("LLM provider: openai", "model", cfg.LLM.Model)
	case "gemini":
		llmProvider = llmgemini.New(cfg.LLM.GeminiAPIKey, cfg.LLM.Model)
		logger.Info("LLM provider: gemini", "model", cfg.LLM.Model)
	}

	fetchBody := cfg.Content.Mode == "full_body" || cfg.Content.Mode == "redacted_body"

	// One alert for the whole daemon: every account classifies through the same
	// provider and spends the same balance. It goes to the main chat — the owner
	// pays for the provider, not the friends whose mailboxes it serves.
	providerName, topUpURL := providerBilling(cfg.LLM.Provider)
	creditAlert := &scheduler.CreditAlert{
		Alerter:  bot,
		Printer:  printer,
		Logger:   logger.With("component", "credit_alert"),
		Provider: providerName,
		TopUpURL: topUpURL,
	}

	g, gCtx := errgroup.WithContext(ctx)

	// Per-account providers are kept so the Telegram handler can act on the
	// mailbox (mark read, fetch body) through the same provider the scheduler uses.
	mailboxes := make(map[string]telegram.Mailbox, len(cfg.Accounts))
	accountInfos := make(map[string]telegram.AccountInfo, len(cfg.Accounts))
	// The bot answers only the chats it serves: the main one and each account's.
	allowedChats := map[int64]bool{cfg.Telegram.ChatID: true}
	chatFor := func(acc domain.Account) int64 {
		if acc.TelegramChatID != 0 {
			return acc.TelegramChatID
		}
		return cfg.Telegram.ChatID
	}

	for _, acc := range cfg.Accounts {
		provider, err := newProvider(gCtx, acc, cfg.OAuth, fetchBody, accountRepo, logger)
		if err != nil {
			return err
		}
		mailboxes[acc.ID] = provider
		accountInfos[acc.ID] = telegram.AccountInfo{Name: acc.Name, Email: acc.Email, ChatID: acc.TelegramChatID}
		allowedChats[chatFor(acc)] = true
		// Everything this account sends — notifications, alerts, the digest —
		// goes to the account's own chat.
		accBot := bot.ForChat(chatFor(acc))
		if acc.TelegramChatID != 0 {
			logger.Info("account notifies its own chat", "account", acc.Email, "chat_id", acc.TelegramChatID)
		}

		if acc.DigestEnabled {
			digestTime := acc.DigestTime
			if digestTime == "" {
				digestTime = cfg.Digest.Time
			}
			digestSched := digest.New(digest.Config{
				AccountID:    acc.ID,
				AccountEmail: acc.Email,
				Time:         digestTime,
				Location:     cfg.Digest.Location,
				EmailRepo:    emailRepo,
				ClassRepo:    classificationRepo,
				DigestRepo:   digestRepo,
				Sender:       accBot,
				Printer:      printer,
				ChatID:       chatFor(acc),
				Logger:       logger.With("component", "digest", "account", acc.Email),
			})
			g.Go(func() error { return digestSched.Start(gCtx) })
		} else {
			// Said out loud at startup: a digest that never arrives is otherwise
			// indistinguishable from one that failed to send.
			logger.Info("digest disabled for account", "account", acc.Email)
		}

		sched := scheduler.New(scheduler.Config{
			AccountID:           acc.ID,
			AccountName:         acc.Name,
			AccountEmail:        acc.Email,
			PollInterval:        acc.PollInterval,
			MinImportance:       domain.ImportanceLevel(cfg.Notification.MinImportance),
			EmailRepo:           emailRepo,
			SyncRepo:            syncRepo,
			ClassificationRepo:  classificationRepo,
			AuditRepo:           auditRepo,
			Filter:              importanceFilter,
			LLMProvider:         llmProvider,
			ContentMode:         cfg.Content.Mode,
			SummaryLanguage:     i18n.LanguageName(locale),
			Focus:               acc.Focus,
			Aliases:             acc.Aliases,
			BotHandles:          acc.BotHandles,
			ScoreDivergenceWarn: cfg.LLM.ScoreDivergenceWarn,
			Provider:            provider,
			Notifier:            accBot,
			Alerter:             accBot,
			CreditAlert:         creditAlert,
			Printer:             printer,
			Logger:              logger.With("component", "scheduler", "account", acc.Email),
			RuleRepo:            ruleRepo,
			ClauseRepo:          clauseRepo,
			RuleEngine:          ruleEngine,
			BaselineFloor:       cfg.Filter.BaselineFloor,
			BackfillWindow:      acc.BackfillWindow,
		})
		g.Go(func() error { return sched.Start(gCtx) })
	}

	handler := &telegram.Handler{
		Bot:                bot,
		Notifier:           bot,
		EmailRepo:          emailRepo,
		SenderRepo:         senderRepo,
		ClassificationRepo: classificationRepo,
		DigestRepo:         digestRepo,
		RuleRepo:           ruleRepo,
		ClauseRepo:         clauseRepo,
		PendingRepo:        pendingRepo,
		Mailboxes:          mailboxes,
		Accounts:           accountInfos,
		P:                  printer,
		Logger:             logger.With("component", "telegram_handler"),
		OwnerChatID:        cfg.Telegram.ChatID,
		BotFor:             func(chatID int64) telegram.BotClient { return bot.ForChat(chatID) },
		NotifierFor:        func(chatID int64) telegram.Notifier { return bot.ForChat(chatID) },
	}

	poller := &telegram.Poller{
		Bot:          bot,
		Handler:      handler,
		SettingsRepo: settingsRepo,
		Logger:       logger.With("component", "telegram_poller"),
		AllowedChats: allowedChats,
	}

	g.Go(func() error { return poller.Run(gCtx) })
	if err := g.Wait(); err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	return nil
}

// newProvider builds the email provider for an account based on its auth type:
// a password IMAP client, or an OAuth (XOAUTH2) IMAP client whose token source
// refreshes and persists tokens back through accountRepo.
func newProvider(
	ctx context.Context,
	acc domain.Account,
	oauthCfg config.OAuthConfig,
	fetchBody bool,
	accountRepo *repo.AccountRepo,
	logger log.Logger,
) (email.Provider, error) {
	switch acc.AuthType {
	case "", domain.AuthPassword:
		return imapmail.NewClient(imapmail.Config{
			Host:      acc.Host,
			Port:      acc.Port,
			Username:  acc.Username,
			Password:  acc.Password,
			TLS:       acc.TLS,
			FetchBody: fetchBody,
			Logger:    logger.With("component", "imap", "account", acc.Email),
		}), nil
	case domain.AuthOAuth:
		oc := oauth.GoogleConfig(oauthCfg.GoogleClientID, oauthCfg.GoogleClientSecret)
		accID := acc.ID
		// reload re-reads the account's tokens from the DB so a re-authorization
		// performed by `account edit` while the daemon runs is picked up on the
		// next refresh, without a restart (stage 008-04, phase 1).
		reload := func() (oauth.Tokens, error) {
			a, err := accountRepo.Get(ctx, accID)
			if err != nil {
				return oauth.Tokens{}, err
			}
			if a == nil {
				return oauth.Tokens{}, fmt.Errorf("account %q not found", accID)
			}
			return oauth.Tokens{
				AccessToken:  a.OAuthAccessToken,
				RefreshToken: a.OAuthRefreshToken,
				Expiry:       a.OAuthTokenExpiry,
			}, nil
		}
		ts := oauth.ReloadingTokenSource(ctx, oc, oauth.Tokens{
			AccessToken:  acc.OAuthAccessToken,
			RefreshToken: acc.OAuthRefreshToken,
			Expiry:       acc.OAuthTokenExpiry,
		}, func(t oauth.Tokens) error {
			return accountRepo.UpdateTokens(ctx, accID, t.AccessToken, t.RefreshToken, t.Expiry)
		}, reload)
		return imapmail.NewClient(imapmail.Config{
			Host:        acc.Host,
			Port:        acc.Port,
			Username:    acc.Username,
			TokenSource: ts,
			TLS:         acc.TLS,
			FetchBody:   fetchBody,
			Logger:      logger.With("component", "imap", "account", acc.Email),
		}), nil
	default:
		return nil, fmt.Errorf("account %q: unsupported auth_type %q", acc.Email, acc.AuthType)
	}
}

func resolveDBPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("EMAIL_AGENT_DB"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "email-agent.db"
	}
	return filepath.Join(home, ".email-agent", "email-agent.db")
}

// providerBilling names an LLM provider for the owner and links the page where
// its balance is topped up.
func providerBilling(provider string) (name, topUpURL string) {
	switch provider {
	case "gemini":
		return "Gemini", "https://aistudio.google.com/billing"
	case "anthropic":
		return "Anthropic", "https://console.anthropic.com/settings/billing"
	case "openai":
		return "OpenAI", "https://platform.openai.com/settings/organization/billing"
	default:
		return provider, ""
	}
}

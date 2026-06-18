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
	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/importance"
	"github.com/paperspell/email-assistant/internal/llm"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/scheduler"
	"github.com/paperspell/email-assistant/internal/telegram"

	imapmail "github.com/paperspell/email-assistant/internal/email/imap"
	llmanthropic "github.com/paperspell/email-assistant/internal/llm/anthropic"
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

	filter := importance.NewFilter(senderRepo, domainRepo)

	bot, err := telegram.NewBot(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
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
	}

	fetchBody := cfg.Content.Mode == "full_body" || cfg.Content.Mode == "redacted_body"

	g, gCtx := errgroup.WithContext(ctx)

	for _, acc := range cfg.Accounts {
		provider, err := newProvider(acc, fetchBody, logger)
		if err != nil {
			return err
		}
		sched := scheduler.New(scheduler.Config{
			AccountID:           acc.ID,
			AccountName:         acc.Name,
			PollInterval:        acc.PollInterval,
			MinImportance:       domain.ImportanceLevel(cfg.Notification.MinImportance),
			EmailRepo:           emailRepo,
			SyncRepo:            syncRepo,
			ClassificationRepo:  classificationRepo,
			AuditRepo:           auditRepo,
			Filter:              filter,
			LLMProvider:         llmProvider,
			ContentMode:         cfg.Content.Mode,
			ScoreDivergenceWarn: cfg.LLM.ScoreDivergenceWarn,
			Provider:            provider,
			Notifier:            bot,
			Logger:              logger.With("component", "scheduler", "account", acc.Email),
		})
		g.Go(func() error { return sched.Start(gCtx) })
	}

	handler := &telegram.Handler{
		Bot:                bot,
		EmailRepo:          emailRepo,
		SenderRepo:         senderRepo,
		ClassificationRepo: classificationRepo,
		Logger:             logger.With("component", "telegram_handler"),
	}

	poller := &telegram.Poller{
		Bot:          bot,
		Handler:      handler,
		SettingsRepo: settingsRepo,
		Logger:       logger.With("component", "telegram_poller"),
	}

	g.Go(func() error { return poller.Run(gCtx) })
	if err := g.Wait(); err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	return nil
}

// newProvider builds the email provider for an account based on its auth type.
// Only password IMAP is supported today; the switch is the seam where an OAuth
// (Gmail/Graph) backend slots in without changing the daemon wiring.
func newProvider(acc domain.Account, fetchBody bool, logger log.Logger) (email.Provider, error) {
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

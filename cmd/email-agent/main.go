package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/pkg/log"
	"github.com/paperspell/email-assistant/internal/scheduler"
	"github.com/paperspell/email-assistant/internal/telegram"

	imapmail "github.com/paperspell/email-assistant/internal/email/imap"
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

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the email monitoring daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemon(cmd.Context(), resolveDBPath(dbPath))
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("email-agent", version)
		},
	}

	root.AddCommand(runCmd, versionCmd, newInitCmd(&dbPath), newConfigCmd(&dbPath))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	err := root.ExecuteContext(ctx)
	stop()
	if err != nil {
		os.Exit(1)
	}
}

func runDaemon(ctx context.Context, path string) error {
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

	cfg, err := config.Load(ctx, settingsRepo)
	if err != nil {
		return err
	}

	logger := log.NewLogger(log.LoggerConfig{
		Dev:   cfg.DevMode,
		Level: cfg.LogLevel,
	})
	ctx = log.IntoContext(ctx, logger)
	logger.Info("email-agent starting", "version", version)

	emailRepo := repo.NewEmailRepo(sqlDB)
	syncRepo := repo.NewSyncStateRepo(sqlDB)

	imapClient := imapmail.NewClient(imapmail.Config{
		Host:     cfg.Account.Host,
		Port:     cfg.Account.Port,
		Username: cfg.Account.Username,
		Password: cfg.Account.Password,
		TLS:      cfg.Account.TLS,
	})

	bot, err := telegram.NewBot(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
	if err != nil {
		return fmt.Errorf("create telegram bot: %w", err)
	}

	sched := scheduler.New(scheduler.Config{
		AccountID:    cfg.Account.Email,
		PollInterval: cfg.Account.PollInterval,
		EmailRepo:    emailRepo,
		SyncRepo:     syncRepo,
		Provider:     imapClient,
		Notifier:     bot,
		Logger:       logger.With("component", "scheduler"),
	})

	return sched.Start(ctx)
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

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

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
	root := &cobra.Command{
		Use:   "email-agent",
		Short: "Local-first email monitoring daemon",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	var configPath string
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the email monitoring daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemon(cmd.Context(), configPath)
		},
	}
	runCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "path to config file")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("email-agent", version)
		},
	}

	root.AddCommand(runCmd, versionCmd)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func runDaemon(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := log.NewLogger(log.LoggerConfig{
		Dev:   cfg.DevMode,
		Level: cfg.LogLevel,
	})

	ctx = log.IntoContext(ctx, logger)
	logger.Info("email-agent starting", "version", version)

	sqlDB, err := db.Open(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Error(err)
		}
	}()

	if err := db.Migrate(ctx, sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

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

	accountID := cfg.Account.Email
	sched := scheduler.New(scheduler.Config{
		AccountID:    accountID,
		PollInterval: cfg.Account.PollInterval,
		EmailRepo:    emailRepo,
		SyncRepo:     syncRepo,
		Provider:     imapClient,
		Notifier:     bot,
		Logger:       logger.With("component", "scheduler"),
	})

	return sched.Start(ctx)
}

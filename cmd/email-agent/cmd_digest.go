package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/digest"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
)

func newDigestCmd(dbPath *string) *cobra.Command {
	digestCmd := &cobra.Command{
		Use:   "digest",
		Short: "Inspect daily digests",
	}
	digestCmd.AddCommand(newDigestShowCmd(dbPath))
	return digestCmd
}

func newDigestShowCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <date> [account]",
		Short: "Reprint a day's digest with the expanded filter counter",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			date := args[0]
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := resolveDigestAccount(ctx, sqlDB, args)
				if err != nil {
					return err
				}
				settings := repo.NewSettingsRepo(sqlDB)
				loc := digestLocation(ctx, settings)
				printer := digestPrinter(ctx, settings)
				d, err := digest.Build(ctx, repo.NewEmailRepo(sqlDB), repo.NewClassificationRepo(sqlDB), acc.ID, date, loc)
				if err != nil {
					return err
				}
				fmt.Println(digest.FormatTelegram(printer, d, acc.Email))
				fmt.Println()
				fmt.Println(digest.FormatCounter(d))
				return nil
			})
		},
	}
}

// resolveDigestAccount picks the account from args[1], or the only account when
// omitted.
func resolveDigestAccount(ctx context.Context, sqlDB *sql.DB, args []string) (*domain.Account, error) {
	ar := repo.NewAccountRepo(sqlDB)
	if len(args) == 2 {
		return mustAccount(ctx, sqlDB, args[1])
	}
	accts, err := ar.List(ctx)
	if err != nil {
		return nil, err
	}
	switch len(accts) {
	case 0:
		return nil, fmt.Errorf("no accounts configured")
	case 1:
		return &accts[0], nil
	default:
		return nil, fmt.Errorf("multiple accounts configured — specify one: digest show <date> <account>")
	}
}

// digestLocation reads the configured digest timezone, defaulting to local time.
// digestPrinter resolves the configured notification language, so `digest show`
// previews the digest exactly as Telegram would receive it. A missing or broken
// setting previews in English rather than failing the command.
func digestPrinter(ctx context.Context, sr *repo.SettingsRepo) *i18n.Printer {
	lang, err := sr.Get(ctx, config.KeyNotificationLanguage)
	if err != nil {
		return i18n.English()
	}
	p, err := i18n.NewPrinter(i18n.ResolveLocale(lang))
	if err != nil {
		return i18n.English()
	}
	return p
}

func digestLocation(ctx context.Context, sr *repo.SettingsRepo) *time.Location {
	tz, err := sr.Get(ctx, config.KeyDigestTimezone)
	if err == nil && tz != "" {
		if loc, lerr := time.LoadLocation(tz); lerr == nil {
			return loc
		}
	}
	return time.Local
}

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
)

func newAuditCmd(dbPath *string) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show recent LLM audit log entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAudit(cmd.Context(), resolveDBPath(*dbPath), limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of entries to show")
	return cmd
}

func runAudit(ctx context.Context, path string, limit int) error {
	hexKey, err := keychain.Load()
	if err != nil {
		return err
	}

	sqlDB, err := db.Open(path, hexKey)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close() //nolint:errcheck

	if err := db.Migrate(ctx, sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	entries, err := repo.NewAuditRepo(sqlDB).List(ctx, limit)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No LLM audit log entries found.")
		return nil
	}

	fmt.Printf("%-22s  %-10s  %-22s  %-14s  %6s  %s\n",
		"CREATED_AT", "PROVIDER", "MODEL", "MODE", "BYTES", "EMAIL_ID")
	fmt.Printf("%-22s  %-10s  %-22s  %-14s  %6s  %s\n",
		"----------------------", "----------", "----------------------", "--------------", "------", "--------")
	for _, e := range entries {
		fmt.Printf("%-22s  %-10s  %-22s  %-14s  %6d  %s\n",
			e.CreatedAt.Format("2006-01-02T15:04:05Z"),
			e.Provider,
			e.Model,
			e.ContentMode,
			e.BytesSent,
			e.EmailID,
		)
	}
	return nil
}

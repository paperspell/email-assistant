package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
)

func newConfigCmd(dbPath *string) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	configCmd.AddCommand(newConfigSetCmd(dbPath))
	return configCmd
}

func newConfigSetCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Update a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(cmd.Context(), resolveDBPath(*dbPath), args[0], args[1])
		},
	}
}

func runConfigSet(ctx context.Context, path, key, value string) error {
	if !config.KnownKeys[key] {
		return fmt.Errorf("unknown setting %q\nvalid keys: see 'email-agent config set --help'", key)
	}

	hexKey, err := keychain.Load()
	if err != nil {
		return err
	}

	sqlDB, err := db.Open(path, hexKey)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close() //nolint:errcheck

	r := repo.NewSettingsRepo(sqlDB)
	if err := r.Set(ctx, key, value); err != nil {
		return err
	}

	fmt.Printf("Set %s\n", key)
	return nil
}

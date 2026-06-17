package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	configCmd.AddCommand(newConfigSetCmd(dbPath), newConfigListCmd(dbPath))
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

func newConfigListCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print all configuration values (sensitive values are partially masked)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigList(cmd.Context(), resolveDBPath(*dbPath))
		},
	}
}

func runConfigList(ctx context.Context, path string) error {
	hexKey, err := keychain.Load()
	if err != nil {
		return err
	}

	sqlDB, err := db.Open(path, hexKey)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close() //nolint:errcheck

	values, err := repo.NewSettingsRepo(sqlDB).GetAll(ctx)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(config.KnownKeys))
	for k := range config.KnownKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Printf("%-35s  %s\n", "KEY", "VALUE")
	fmt.Printf("%-35s  %s\n", strings.Repeat("-", 35), strings.Repeat("-", 30))
	for _, k := range keys {
		v := values[k]
		fmt.Printf("%-35s  %s\n", k, maskValue(k, v))
	}
	return nil
}

// maskValue returns the display string for a setting value.
// Sensitive keys (password, token, api_key) show only the last 4 characters.
func maskValue(key, value string) string {
	if value == "" {
		return "(not set)"
	}
	if isSensitiveKey(key) {
		if len(value) <= 4 {
			return "****"
		}
		return "***" + value[len(value)-4:]
	}
	return value
}

func isSensitiveKey(key string) bool {
	for _, s := range []string{"password", "token", "api_key"} {
		if strings.Contains(key, s) {
			return true
		}
	}
	return false
}

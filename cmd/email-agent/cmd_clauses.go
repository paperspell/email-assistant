package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/db/repo"
)

// clauseWarnThreshold warns the user when an account accumulates many active
// clauses, since each one grows every classification prompt.
const clauseWarnThreshold = 12

func newClausesCmd(dbPath *string) *cobra.Command {
	clausesCmd := &cobra.Command{
		Use:   "clauses",
		Short: "Manage per-account LLM ignore clauses",
	}
	clausesCmd.AddCommand(
		newClausesListCmd(dbPath),
		newClausesToggleCmd(dbPath, true),
		newClausesToggleCmd(dbPath, false),
		newClausesRemoveCmd(dbPath),
	)
	return clausesCmd
}

func newClausesListCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list <account>",
		Short: "List an account's LLM ignore clauses",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				clauses, err := repo.NewClauseRepo(sqlDB).List(ctx, acc.ID)
				if err != nil {
					return err
				}
				if len(clauses) == 0 {
					fmt.Printf("No LLM clauses for %s.\n", acc.Email)
					return nil
				}
				active := 0
				for i, c := range clauses {
					state := "off"
					if c.Enabled {
						state = "on"
						active++
					}
					fmt.Printf("%-3d  [%-3s]  (%s)  %s\n", i+1, state, c.Source, c.Text)
				}
				if active > clauseWarnThreshold {
					fmt.Printf("\nWarning: %d active clauses — each is added to every "+
						"classification prompt, growing token cost.\n", active)
				}
				return nil
			})
		},
	}
}

func newClausesToggleCmd(dbPath *string, enable bool) *cobra.Command {
	use, short := "disable <account> <n>", "Disable clause number n"
	if enable {
		use, short = "enable <account> <n>", "Enable clause number n"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				cr := repo.NewClauseRepo(sqlDB)
				n, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("invalid clause number %q", args[1])
				}
				clause, err := cr.GetByIndex(ctx, acc.ID, n)
				if err != nil {
					return err
				}
				if err := cr.SetEnabled(ctx, clause.ID, enable); err != nil {
					return err
				}
				fmt.Printf("Clause %d %sd.\n", n, map[bool]string{true: "enable", false: "disable"}[enable])
				return nil
			})
		},
	}
}

func newClausesRemoveCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <account> <n>",
		Short: "Remove clause number n",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				cr := repo.NewClauseRepo(sqlDB)
				n, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("invalid clause number %q", args[1])
				}
				clause, err := cr.GetByIndex(ctx, acc.ID, n)
				if err != nil {
					return err
				}
				if err := cr.Delete(ctx, clause.ID); err != nil {
					return err
				}
				fmt.Printf("Removed clause %d.\n", n)
				return nil
			})
		},
	}
}

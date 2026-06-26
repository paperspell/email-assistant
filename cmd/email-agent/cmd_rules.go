package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
)

func newRulesCmd(dbPath *string) *cobra.Command {
	rulesCmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage per-account filter rules",
	}
	rulesCmd.AddCommand(
		newRulesListCmd(dbPath),
		newRulesToggleCmd(dbPath, true),
		newRulesToggleCmd(dbPath, false),
		newRulesEditCmd(dbPath),
		newRulesRemoveCmd(dbPath),
		newRulesWhyCmd(dbPath),
	)
	return rulesCmd
}

func newRulesListCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list <account>",
		Short: "List an account's filter rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				rules, err := repo.NewRuleRepo(sqlDB).List(ctx, acc.ID)
				if err != nil {
					return err
				}
				if len(rules) == 0 {
					fmt.Printf("No filter rules for %s.\n", acc.Email)
					return nil
				}
				fmt.Printf("%-3s  %-6s  %-8s  %-28s  %-22s  %-7s  %s\n",
					"#", "ACTION", "TYPE", "VALUE", "SCOPE", "ENABLED", "SOURCE")
				for i, r := range rules {
					scope := ""
					if r.ScopeKind != "" {
						scope = r.ScopeKind + "=" + r.ScopeValue
					}
					fmt.Printf("%-3d  %-6s  %-8s  %-28s  %-22s  %-7v  %s\n",
						i+1, r.Action, r.Type, truncate(r.Value, 28), truncate(scope, 22), r.Enabled, r.Source)
				}
				return nil
			})
		},
	}
}

func newRulesToggleCmd(dbPath *string, enable bool) *cobra.Command {
	use, short := "disable <account> <n>", "Disable rule number n"
	if enable {
		use, short = "enable <account> <n>", "Enable rule number n"
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
				rr := repo.NewRuleRepo(sqlDB)
				rule, err := ruleByArg(ctx, rr, acc.ID, args[1])
				if err != nil {
					return err
				}
				if err := rr.SetEnabled(ctx, rule.ID, enable); err != nil {
					return err
				}
				fmt.Printf("Rule %s %sd.\n", args[1], map[bool]string{true: "enable", false: "disable"}[enable])
				return nil
			})
		},
	}
}

func newRulesEditCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <account> <n>",
		Short: "Edit the matched value (and subject scope) of rule n",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				rr := repo.NewRuleRepo(sqlDB)
				rule, err := ruleByArg(ctx, rr, acc.ID, args[1])
				if err != nil {
					return err
				}
				sc := bufio.NewScanner(os.Stdin)
				value := promptText(sc, "  Value", rule.Value)
				scopeValue := rule.ScopeValue
				if rule.Type == domain.RuleTypeSubject {
					scopeValue = promptText(sc, "  Scope sender (blank for global)", rule.ScopeValue)
				}
				scopeKind := ""
				if scopeValue != "" {
					scopeKind = domain.RuleTypeSender
				}
				if err := rr.UpdateValue(ctx, rule.ID, value, scopeKind, scopeValue); err != nil {
					return err
				}
				fmt.Println("Rule updated.")
				return nil
			})
		},
	}
}

func newRulesRemoveCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <account> <n>",
		Short: "Remove rule number n",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				rr := repo.NewRuleRepo(sqlDB)
				rule, err := ruleByArg(ctx, rr, acc.ID, args[1])
				if err != nil {
					return err
				}
				if err := rr.Delete(ctx, rule.ID); err != nil {
					return err
				}
				fmt.Printf("Removed rule %s (%s %s).\n", args[1], rule.Action, rule.Type)
				return nil
			})
		},
	}
}

func newRulesWhyCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "why <account> <uid>",
		Short: "Explain why an email was filtered (provenance)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				acc, err := mustAccount(ctx, sqlDB, args[0])
				if err != nil {
					return err
				}
				uid, err := strconv.ParseUint(args[1], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid uid %q", args[1])
				}
				return explainEmail(ctx, sqlDB, acc.ID, uint32(uid))
			})
		},
	}
}

// explainEmail prints the provenance (decided_by) of an email and the supporting
// classification detail.
func explainEmail(ctx context.Context, sqlDB *sql.DB, accountID string, uid uint32) error {
	er := repo.NewEmailRepo(sqlDB)
	e, err := er.GetByAccountAndUID(ctx, accountID, uid)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("no email with uid %d for that account", uid)
	}
	fmt.Printf("Email: %s — %q\nStatus: %s\n", e.FromEmail, e.Subject, e.Status)

	switch {
	case e.DecidedBy == "":
		fmt.Println("Decided by: notified or allowed (not filtered).")
	case strings.HasPrefix(e.DecidedBy, "rule:"):
		id := strings.TrimPrefix(e.DecidedBy, "rule:")
		rules, rerr := repo.NewRuleRepo(sqlDB).List(ctx, accountID)
		if rerr != nil {
			return rerr
		}
		for _, r := range rules {
			if r.ID == id {
				fmt.Printf("Decided by: filter rule (%s %s = %q).\n", r.Action, r.Type, r.Value)
				return nil
			}
		}
		fmt.Printf("Decided by: filter rule %s (since deleted).\n", id)
	case e.DecidedBy == "baseline":
		fmt.Println("Decided by: baseline score gate.")
		printClassificationReasons(ctx, sqlDB, e.ID)
	case e.DecidedBy == "llm:low":
		fmt.Println("Decided by: LLM judged it unimportant.")
		printClassificationReasons(ctx, sqlDB, e.ID)
	default:
		fmt.Printf("Decided by: %s\n", e.DecidedBy)
	}
	return nil
}

func printClassificationReasons(ctx context.Context, sqlDB *sql.DB, emailID string) {
	all, err := repo.NewClassificationRepo(sqlDB).GetAllByEmailID(ctx, emailID)
	if err != nil {
		return
	}
	for _, c := range all {
		fmt.Printf("  [%s] level=%s score=%d\n", c.Source, c.Level, c.Score)
		if c.Summary != "" {
			fmt.Printf("    summary: %s\n", c.Summary)
		}
		for _, r := range c.Reason {
			fmt.Printf("    • %s\n", r)
		}
	}
}

// ruleByArg resolves a 1-based rule index argument to a rule for the account.
func ruleByArg(ctx context.Context, rr *repo.RuleRepo, accountID, arg string) (*domain.FilterRule, error) {
	n, err := strconv.Atoi(arg)
	if err != nil {
		return nil, fmt.Errorf("invalid rule number %q", arg)
	}
	return rr.GetByIndex(ctx, accountID, n)
}

// mustAccount resolves an account reference or returns a clear error.
func mustAccount(ctx context.Context, sqlDB *sql.DB, ref string) (*domain.Account, error) {
	acc, err := repo.NewAccountRepo(sqlDB).Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, fmt.Errorf("no account matching %q", ref)
	}
	return acc, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

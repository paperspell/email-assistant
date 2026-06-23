package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
	"github.com/paperspell/email-assistant/internal/auth/oauth"
	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
)

func newAccountCmd(dbPath *string) *cobra.Command {
	accountCmd := &cobra.Command{
		Use:   "account",
		Short: "Manage email accounts",
	}
	accountCmd.AddCommand(
		newAccountListCmd(dbPath),
		newAccountAddCmd(dbPath),
		newAccountEditCmd(dbPath),
		newAccountRemoveCmd(dbPath),
		newAccountToggleCmd(dbPath, true),
		newAccountToggleCmd(dbPath, false),
	)
	return accountCmd
}

func newAccountListCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				return runAccountList(ctx, repo.NewAccountRepo(sqlDB))
			})
		},
	}
}

func newAccountAddCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new email account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAccountAdd(cmd.Context(), resolveDBPath(*dbPath))
		},
	}
}

func newAccountEditCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <email|name>",
		Short: "Reconfigure an existing account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				ar := repo.NewAccountRepo(sqlDB)
				acc, err := ar.Get(ctx, args[0])
				if err != nil {
					return err
				}
				if acc == nil {
					return fmt.Errorf("no account matching %q", args[0])
				}
				sr := repo.NewSettingsRepo(sqlDB)
				return addOrEditAccount(ctx, bufio.NewScanner(os.Stdin), ar, sr, acc)
			})
		},
	}
}

func newAccountRemoveCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <email|name>",
		Short: "Remove an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				return runAccountRemove(ctx, repo.NewAccountRepo(sqlDB), args[0])
			})
		},
	}
}

func newAccountToggleCmd(dbPath *string, enable bool) *cobra.Command {
	use, short := "disable <email|name>", "Stop polling an account"
	if enable {
		use, short = "enable <email|name>", "Resume polling an account"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(cmd.Context(), resolveDBPath(*dbPath), func(ctx context.Context, sqlDB *sql.DB) error {
				ar := repo.NewAccountRepo(sqlDB)
				acc, err := ar.Get(ctx, args[0])
				if err != nil {
					return err
				}
				if acc == nil {
					return fmt.Errorf("no account matching %q", args[0])
				}
				if err := ar.SetEnabled(ctx, acc.ID, enable); err != nil {
					return err
				}
				state := "disabled"
				if enable {
					state = "enabled"
				}
				fmt.Printf("Account %s %s.\n", acc.Email, state)
				return nil
			})
		},
	}
}

func runAccountList(ctx context.Context, ar *repo.AccountRepo) error {
	accounts, err := ar.List(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Println("No accounts configured. Add one with: email-agent account add")
		return nil
	}

	fmt.Printf("%-20s  %-28s  %-24s  %-7s  %s\n", "NAME", "EMAIL", "HOST", "ENABLED", "AUTH")
	fmt.Printf("%-20s  %-28s  %-24s  %-7s  %s\n",
		strings.Repeat("-", 20), strings.Repeat("-", 28), strings.Repeat("-", 24), "-------", "----")
	for _, a := range accounts {
		fmt.Printf("%-20s  %-28s  %-24s  %-7v  %s\n", a.Name, a.Email, a.Host, a.Enabled, a.AuthType)
	}
	return nil
}

func runAccountAdd(ctx context.Context, path string) error {
	return withDB(ctx, path, func(ctx context.Context, sqlDB *sql.DB) error {
		return addOrEditAccount(ctx, bufio.NewScanner(os.Stdin),
			repo.NewAccountRepo(sqlDB), repo.NewSettingsRepo(sqlDB), nil)
	})
}

func runAccountRemove(ctx context.Context, ar *repo.AccountRepo, ref string) error {
	acc, err := ar.Get(ctx, ref)
	if err != nil {
		return err
	}
	if acc == nil {
		return fmt.Errorf("no account matching %q", ref)
	}

	sc := bufio.NewScanner(os.Stdin)
	if !confirm(sc, fmt.Sprintf("Remove account %s?", acc.Email), false) {
		fmt.Println("Cancelled.")
		return nil
	}
	if confirm(sc, "Also delete its stored emails and sync state?", false) {
		if err := ar.PurgeData(ctx, acc.ID); err != nil {
			return err
		}
		fmt.Println("Deleted stored emails and sync state.")
	}
	if err := ar.Delete(ctx, acc.ID); err != nil {
		return err
	}
	fmt.Printf("Removed account %s.\n", acc.Email)
	return nil
}

// addOrEditAccount prompts for account fields and upserts the result. When
// existing is non-nil the prompts are pre-filled with its values (edit flow);
// the account identity (id) always tracks the email address.
func addOrEditAccount(
	ctx context.Context, sc *bufio.Scanner, ar *repo.AccountRepo, sr *repo.SettingsRepo, existing *domain.Account,
) error {
	cur := domain.Account{
		Port:         993,
		TLS:          true,
		PollInterval: defaultPollInterval(ctx, sr),
		AuthType:     domain.AuthPassword,
		Enabled:      true,
	}
	if existing != nil {
		cur = *existing
	}

	fmt.Println("Email Account")
	authType := strings.ToLower(promptText(sc, "  Auth type (password/oauth)", cur.AuthType))
	if authType != domain.AuthPassword && authType != domain.AuthOAuth {
		return fmt.Errorf("auth type must be %q or %q", domain.AuthPassword, domain.AuthOAuth)
	}
	// Gmail OAuth has well-known IMAP defaults.
	if authType == domain.AuthOAuth && cur.Host == "" {
		cur.Host = "imap.gmail.com"
	}

	name := promptText(sc, "  Name", cur.Name)
	email := promptText(sc, "  Email", cur.Email)
	host := promptText(sc, "  Host", cur.Host)
	port, err := promptInt(sc, "  Port", cur.Port)
	if err != nil {
		return err
	}
	username := cur.Username
	if username == "" {
		username = email
	}
	username = promptText(sc, "  Username", username)
	tls := promptText(sc, "  TLS", strconv.FormatBool(cur.TLS)) == "true"
	poll, err := promptDuration(sc, "  Poll interval", cur.PollInterval)
	if err != nil {
		return err
	}

	acc := domain.Account{
		ID:           email, // identity = email
		Name:         name,
		Email:        email,
		Host:         host,
		Port:         port,
		Username:     username,
		TLS:          tls,
		PollInterval: poll,
		AuthType:     authType,
		Enabled:      cur.Enabled,
	}

	if acc.Email == "" || acc.Host == "" {
		return fmt.Errorf("email and host are required")
	}

	if authType == domain.AuthOAuth {
		if err := configureAccountOAuth(ctx, sc, sr, &cur, &acc); err != nil {
			return err
		}
	} else {
		password, perr := promptPassword("  Password (Enter to keep unchanged)", sc)
		if perr != nil {
			return fmt.Errorf("read password: %w", perr)
		}
		acc.Password = cur.Password
		if password != "" {
			acc.Password = password
		}
		if acc.Password == "" {
			return fmt.Errorf("password is required")
		}
	}

	if err := ar.Upsert(ctx, acc); err != nil {
		return err
	}
	fmt.Printf("\nSaved account %s.\n", acc.Email)
	return nil
}

// defaultPollInterval returns the global poll.default_interval setting, falling
// back to config.DefaultPollInterval when unset, invalid, or unreadable.
func defaultPollInterval(ctx context.Context, sr *repo.SettingsRepo) time.Duration {
	v, err := sr.Get(ctx, config.KeyPollDefaultInterval)
	if err != nil {
		return config.DefaultPollInterval
	}
	return config.PollIntervalOrDefault(v)
}

// configureAccountOAuth carries over existing tokens and runs the Google consent
// flow when needed, populating acc's OAuth fields.
func configureAccountOAuth(
	ctx context.Context, sc *bufio.Scanner, sr *repo.SettingsRepo, cur, acc *domain.Account,
) error {
	acc.OAuthRefreshToken = cur.OAuthRefreshToken
	acc.OAuthAccessToken = cur.OAuthAccessToken
	acc.OAuthTokenExpiry = cur.OAuthTokenExpiry

	needConsent := acc.OAuthRefreshToken == ""
	if !needConsent {
		needConsent = confirm(sc, "  Re-authorize with Google now?", false)
	}
	if needConsent {
		toks, err := runOAuthConsent(ctx, sr)
		if err != nil {
			return err
		}
		acc.OAuthRefreshToken = toks.RefreshToken
		acc.OAuthAccessToken = toks.AccessToken
		acc.OAuthTokenExpiry = toks.Expiry
	}
	if acc.OAuthRefreshToken == "" {
		return fmt.Errorf("OAuth authorization is required for an oauth account")
	}
	return nil
}

// runOAuthConsent loads the global Google client credentials and runs the
// browser consent flow.
func runOAuthConsent(ctx context.Context, sr *repo.SettingsRepo) (oauth.Tokens, error) {
	all, err := sr.GetAll(ctx)
	if err != nil {
		return oauth.Tokens{}, err
	}
	id := all[config.KeyOAuthGoogleClientID]
	secret := all[config.KeyOAuthGoogleClientSecret]
	if id == "" || secret == "" {
		return oauth.Tokens{}, fmt.Errorf(
			"no Google OAuth client configured — run 'email-agent init oauth' first")
	}
	return oauth.Consent(ctx, oauth.GoogleConfig(id, secret))
}

// withDB opens the (existing) database, runs migrations, and invokes fn.
func withDB(ctx context.Context, path string, fn func(context.Context, *sql.DB) error) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("database not found at %s — run 'email-agent init' first", path)
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
	if err := db.Migrate(ctx, sqlDB); err != nil {
		return err
	}
	return fn(ctx, sqlDB)
}

func promptInt(sc *bufio.Scanner, label string, def int) (int, error) {
	v := promptText(sc, label, strconv.Itoa(def))
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid number %q", strings.TrimSpace(label), v)
	}
	return n, nil
}

func promptDuration(sc *bufio.Scanner, label string, def time.Duration) (time.Duration, error) {
	v := promptText(sc, label, def.String())
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", strings.TrimSpace(label), v)
	}
	return d, nil
}

func confirm(sc *bufio.Scanner, question string, def bool) bool {
	d := "n"
	if def {
		d = "y"
	}
	answer := promptText(sc, question, d)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

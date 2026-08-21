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
	"github.com/paperspell/email-assistant/internal/filter"
	"github.com/paperspell/email-assistant/internal/pkg/idx"
)

// maxAccountBackfill bounds the first-run backfill window offered by the CLI to
// one week (mirrors scheduler.maxBackfillWindow).
const maxAccountBackfill = 7 * 24 * time.Hour

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
				return addOrEditAccount(ctx, bufio.NewScanner(os.Stdin), ar, sr,
					repo.NewClauseRepo(sqlDB), repo.NewRuleRepo(sqlDB), acc)
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
			repo.NewAccountRepo(sqlDB), repo.NewSettingsRepo(sqlDB),
			repo.NewClauseRepo(sqlDB), repo.NewRuleRepo(sqlDB), nil)
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
	ctx context.Context, sc *bufio.Scanner, ar *repo.AccountRepo, sr *repo.SettingsRepo,
	cr *repo.ClauseRepo, rr *repo.RuleRepo, existing *domain.Account,
) error {
	cur := domain.Account{
		Port:         993,
		TLS:          true,
		PollInterval: defaultPollInterval(ctx, sr),
		// Gmail over OAuth is the common case, so new accounts default to it and
		// the user can just press Enter.
		AuthType: domain.AuthOAuth,
		Enabled:  true,
	}
	if existing != nil {
		cur = *existing
	}

	fmt.Println("Email Account")
	authType := strings.ToLower(promptText(sc, "  Auth type (password/oauth)", cur.AuthType))
	if authType != domain.AuthPassword && authType != domain.AuthOAuth {
		return fmt.Errorf("auth type must be %q or %q", domain.AuthPassword, domain.AuthOAuth)
	}
	if authType == domain.AuthOAuth && !oauthClientConfigured(ctx, sr) {
		// The account is saved either way, but consent cannot run until the
		// Google client exists — say so here instead of failing later.
		fmt.Println("  (no Google OAuth client yet — run 'email-agent init oauth' before starting the daemon)")
	}

	// Email identifies the account, so ask it first; other fields default from it.
	email := promptText(sc, "  Email", cur.Email)

	name := cur.Name
	if name == "" {
		name = email
	}
	host, username := cur.Host, cur.Username
	port, tls := cur.Port, cur.TLS

	if authType == domain.AuthOAuth {
		// Gmail OAuth has well-known IMAP settings — no need to prompt for them.
		if host == "" {
			host = "imap.gmail.com"
		}
		if username == "" {
			username = email
		}
	} else {
		// Password IMAP (any provider): ask the connection details.
		name = promptText(sc, "  Name", name)
		host = promptText(sc, "  Host", host)
		p, perr := promptInt(sc, "  Port", port)
		if perr != nil {
			return perr
		}
		port = p
		if username == "" {
			username = email
		}
		username = promptText(sc, "  Username", username)
		tls = promptText(sc, "  TLS", strconv.FormatBool(tls)) == "true"
	}

	poll, err := promptDuration(sc, "  Poll interval (bare number = minutes)", cur.PollInterval, time.Minute)
	if err != nil {
		return err
	}

	backfill, err := promptDuration(sc,
		"  First-run backfill of unread mail (0 = off, max 168h; bare number = hours)",
		cur.BackfillWindow, time.Hour)
	if err != nil {
		return err
	}
	if backfill < 0 {
		backfill = 0
	}
	if backfill > maxAccountBackfill {
		backfill = maxAccountBackfill
		fmt.Printf("  (clamped to the %s maximum)\n", maxAccountBackfill)
	}

	acc := domain.Account{
		ID:             email, // identity = email
		Name:           name,
		Email:          email,
		Host:           host,
		Port:           port,
		Username:       username,
		TLS:            tls,
		PollInterval:   poll,
		AuthType:       authType,
		Enabled:        cur.Enabled,
		DigestTime:     cur.DigestTime,
		BackfillWindow: backfill,
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

	// Seed default rules/clauses for newly added accounts only.
	if existing == nil {
		if err := seedAccountDefaults(ctx, sc, sr, cr, rr, acc.ID); err != nil {
			return fmt.Errorf("seed defaults: %w", err)
		}
	}
	return nil
}

// oauthClientConfigured reports whether a Google OAuth client (client id +
// secret) has been configured, which is the signal to default new accounts to
// oauth instead of password.
func oauthClientConfigured(ctx context.Context, sr *repo.SettingsRepo) bool {
	all, err := sr.GetAll(ctx)
	if err != nil {
		return false
	}
	return all[config.KeyOAuthGoogleClientID] != "" && all[config.KeyOAuthGoogleClientSecret] != ""
}

// seedAccountDefaults seeds the Set A default ignore clauses (when an LLM provider
// is configured) and offers the Set B example rules. Idempotent: clauses are only
// seeded when the account has none yet.
func seedAccountDefaults(
	ctx context.Context, sc *bufio.Scanner,
	sr *repo.SettingsRepo, cr *repo.ClauseRepo, rr *repo.RuleRepo, accountID string,
) error {
	provider, err := sr.Get(ctx, config.KeyLLMProvider)
	if err != nil {
		return err
	}
	if provider != "" {
		seeded, err := seedDefaultClausesIfEmpty(ctx, cr, accountID)
		if err != nil {
			return err
		}
		if seeded {
			fmt.Printf("Seeded %d default ignore clauses (manage with 'clauses').\n",
				len(filter.DefaultIgnoreClauses()))
		}
	}

	examples := filter.ExampleRules()
	fmt.Println("\nExample filter rules (edit/enable later with 'rules'):")
	for _, r := range examples {
		fmt.Printf("  - ignore %s %q\n", r.Type, r.Value)
	}
	enable := confirm(sc, "Add these example rules (enabled)?", false)
	for _, r := range examples {
		r.ID = idx.GenerateID()
		r.AccountID = accountID
		r.Source = domain.RuleSourceDefault
		r.Enabled = enable
		if err := rr.Add(ctx, r); err != nil {
			return err
		}
	}
	if enable {
		fmt.Println("Example rules added (enabled).")
	} else {
		fmt.Println("Example rules added (disabled).")
	}
	return nil
}

// seedDefaultClausesIfEmpty inserts the Set A default ignore clauses for an
// account that has none yet. Returns whether it seeded. Shared by account
// creation and the post-LLM-config reconciliation in init.
func seedDefaultClausesIfEmpty(ctx context.Context, cr *repo.ClauseRepo, accountID string) (bool, error) {
	count, err := cr.Count(ctx, accountID)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	for _, text := range filter.DefaultIgnoreClauses() {
		if err := cr.Add(ctx, domain.LLMClause{
			ID:        idx.GenerateID(),
			AccountID: accountID,
			Text:      text,
			Enabled:   true,
			Source:    domain.RuleSourceDefault,
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// reconcileDefaultClauses seeds default ignore clauses for every account that has
// none, provided an LLM provider is configured. Called after the LLM section of
// init so accounts created before the LLM was enabled still get the defaults
// (replacing the old migration-time backfill).
func reconcileDefaultClauses(ctx context.Context, sqlDB *sql.DB) error {
	sr := repo.NewSettingsRepo(sqlDB)
	provider, err := sr.Get(ctx, config.KeyLLMProvider)
	if err != nil || provider == "" {
		return err
	}
	ar := repo.NewAccountRepo(sqlDB)
	cr := repo.NewClauseRepo(sqlDB)
	accounts, err := ar.List(ctx)
	if err != nil {
		return err
	}
	seeded := 0
	for _, a := range accounts {
		ok, err := seedDefaultClausesIfEmpty(ctx, cr, a.ID)
		if err != nil {
			return err
		}
		if ok {
			seeded++
		}
	}
	if seeded > 0 {
		fmt.Printf("Seeded default ignore clauses for %d account(s).\n", seeded)
	}
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

// promptDuration reads a duration, accepting a plain number as shorthand for
// unit — "48" means 48h when unit is time.Hour. Go's ParseDuration rejects
// unitless input, which is an easy mistake to make at an interactive prompt.
func promptDuration(sc *bufio.Scanner, label string, def time.Duration, unit time.Duration) (time.Duration, error) {
	v := promptText(sc, label, def.String())
	d, err := parseDurationDefaultUnit(v, unit)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", strings.TrimSpace(label), v)
	}
	return d, nil
}

// parseDurationDefaultUnit parses a Go duration string; a bare number is
// multiplied by unit instead of being rejected.
func parseDurationDefaultUnit(s string, unit time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(unit)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", s, err)
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

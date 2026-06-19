package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
)

// sectionFn configures one group of settings interactively.
// current holds all settings already stored in the DB.
type sectionFn func(
	ctx context.Context,
	sc *bufio.Scanner,
	r *repo.SettingsRepo,
	current map[string]string,
) error

func newInitCmd(dbPath *string) *cobra.Command {
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Set up or reconfigure the email agent (subcommands reconfigure individual sections)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFullInit(cmd.Context(), resolveDBPath(*dbPath))
		},
	}

	initCmd.AddCommand(
		&cobra.Command{
			Use:   "account",
			Short: "Add or reconfigure an email account",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runAccountAdd(cmd.Context(), resolveDBPath(*dbPath))
			},
		},
		newInitSectionCmd(dbPath, "telegram", "Reconfigure Telegram settings", configureTelegram),
		newInitSectionCmd(dbPath, "notifications", "Reconfigure notification settings", configureNotifications),
		newInitSectionCmd(dbPath, "llm", "Configure LLM classification (optional)", configureLLM),
		newInitSectionCmd(dbPath, "oauth", "Configure Google OAuth client (for Gmail accounts)", configureOAuth),
	)

	return initCmd
}

// --- full setup ---

func runFullInit(ctx context.Context, path string) error {
	sc := bufio.NewScanner(os.Stdin)
	_, dbExists := os.Stat(path)

	var hexKey string

	if dbExists == nil {
		fmt.Printf("Database already exists at %s.\n", path)
		fmt.Println("To reconfigure a single section use: email-agent init <account|telegram|notifications|llm>")
		answer := promptText(sc, "Override all settings?", "n")
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Cancelled.")
			return nil
		}
		fmt.Println()

		var err error
		hexKey, err = keychain.Load()
		if err != nil {
			return err
		}
	} else {
		fmt.Println("Setting up Email Agent.")
		fmt.Println()

		var err error
		hexKey, err = keychain.Generate()
		if err != nil {
			return err
		}

		saved, err := keychain.Save(hexKey)
		if err != nil {
			return err
		}
		if !saved {
			fmt.Printf("Keychain is not available. Set %s before running the daemon:\n", keychain.EnvKey)
			fmt.Printf("  export %s=%s\n", keychain.EnvKey, hexKey)
			fmt.Print("\nPress Enter after saving the key...")
			bufio.NewReader(os.Stdin).ReadString('\n') //nolint:errcheck
			fmt.Println()
		} else {
			fmt.Println("Encryption key saved to system keychain.")
			fmt.Println()
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
	}

	sqlDB, err := db.Open(path, hexKey)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close() //nolint:errcheck

	if err := db.Migrate(ctx, sqlDB); err != nil {
		return err
	}

	r := repo.NewSettingsRepo(sqlDB)
	ar := repo.NewAccountRepo(sqlDB)

	current, _ := r.GetAll(ctx) //nolint:errcheck
	applyDefaults(current)

	if err := addOrEditAccount(ctx, sc, ar, r, nil); err != nil {
		return err
	}
	fmt.Println()
	if err := configureTelegram(ctx, sc, r, current); err != nil {
		return err
	}
	fmt.Println()
	if err := configureNotifications(ctx, sc, r, current); err != nil {
		return err
	}

	// Persist defaults that are not prompted (LLM is never set during full init)
	for k, v := range map[string]string{config.KeyLogLevel: "info", config.KeyDevMode: "false"} {
		if current[k] == "" {
			if err := r.Set(ctx, k, v); err != nil {
				return fmt.Errorf("save %s: %w", k, err)
			}
		}
	}

	if _, err := config.Load(ctx, r, ar); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	action := "created"
	if dbExists == nil {
		action = "updated"
	}
	fmt.Printf("\nDone. Database %s at %s\nRun 'email-agent run' to start.\n", action, path)
	fmt.Println("Add more accounts with: email-agent account add")
	fmt.Println("To enable LLM classification run: email-agent init llm")
	return nil
}

// --- section subcommand infrastructure ---

func newInitSectionCmd(dbPath *string, use, short string, fn sectionFn) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSectionInit(cmd.Context(), resolveDBPath(*dbPath), fn)
		},
	}
}

func runSectionInit(ctx context.Context, path string, fn sectionFn) error {
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

	r := repo.NewSettingsRepo(sqlDB)
	current, err := r.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("load current settings: %w", err)
	}
	applyDefaults(current)

	sc := bufio.NewScanner(os.Stdin)
	if err := fn(ctx, sc, r, current); err != nil {
		return err
	}

	fmt.Println("\nDone.")
	return nil
}

// --- section functions ---

func configureTelegram(
	ctx context.Context, sc *bufio.Scanner, r *repo.SettingsRepo, current map[string]string,
) error {
	fmt.Println("Telegram")

	botToken, err := promptPassword("  Bot token (Enter to keep unchanged)", sc)
	if err != nil {
		return fmt.Errorf("read bot token: %w", err)
	}
	chatID := promptText(sc, "  Chat ID", current[config.KeyTelegramChatID])

	settings := map[string]string{
		config.KeyTelegramChatID: chatID,
	}
	if botToken != "" {
		settings[config.KeyTelegramBotToken] = botToken
	}
	return saveSettings(ctx, r, settings)
}

func configureNotifications(
	ctx context.Context, sc *bufio.Scanner, r *repo.SettingsRepo, current map[string]string,
) error {
	fmt.Println("Notifications")

	minImportance := promptText(
		sc,
		"  Min importance (critical/important/maybe)",
		current[config.KeyNotificationMinImportance],
	)
	return saveSettings(ctx, r, map[string]string{
		config.KeyNotificationMinImportance: minImportance,
	})
}

func configureLLM(
	ctx context.Context, sc *bufio.Scanner, r *repo.SettingsRepo, current map[string]string,
) error {
	fmt.Println("LLM Classification")
	fmt.Println("  (press Enter at provider prompt to disable LLM)")

	provider := promptText(sc, "  Provider (anthropic/openai)", current[config.KeyLLMProvider])
	provider = strings.ToLower(strings.TrimSpace(provider))

	settings := map[string]string{config.KeyLLMProvider: provider}

	if provider == "" {
		fmt.Println("  LLM disabled.")
		return saveSettings(ctx, r, settings)
	}

	switch provider {
	case "anthropic":
		apiKey, err := promptPassword("  Anthropic API key (Enter to keep unchanged)", sc)
		if err != nil {
			return fmt.Errorf("read api key: %w", err)
		}
		if apiKey != "" {
			settings[config.KeyLLMAnthropicAPIKey] = apiKey
		}
	case "openai":
		apiKey, err := promptPassword("  OpenAI API key (Enter to keep unchanged)", sc)
		if err != nil {
			return fmt.Errorf("read api key: %w", err)
		}
		if apiKey != "" {
			settings[config.KeyLLMOpenAIAPIKey] = apiKey
		}
	default:
		return fmt.Errorf("unknown provider %q (valid: anthropic, openai)", provider)
	}

	model := promptText(sc, "  Model override (Enter for default)", current[config.KeyLLMModel])
	if model != "" {
		settings[config.KeyLLMModel] = model
	}

	fmt.Println("  Body access:")
	fmt.Println("    headers_only  — subject and headers only (most private)")
	fmt.Println("    redacted_body — body sent with PII replaced by [EMAIL], [PHONE], etc.")
	fmt.Println("    full_body     — complete body, unmodified")
	contentMode := promptText(
		sc,
		"  Body access (headers_only/redacted_body/full_body)",
		current[config.KeyContentMode],
	)
	settings[config.KeyContentMode] = contentMode

	return saveSettings(ctx, r, settings)
}

func configureOAuth(
	ctx context.Context, sc *bufio.Scanner, r *repo.SettingsRepo, current map[string]string,
) error {
	fmt.Println("Google OAuth (for Gmail accounts)")
	fmt.Println("  Create a Desktop-app OAuth client in Google Cloud Console; see")
	fmt.Println("  docs/stages/rollout/008-01-gmail-oauth-setup.md")

	clientID := promptText(sc, "  Client ID", current[config.KeyOAuthGoogleClientID])
	clientSecret, err := promptPassword("  Client secret (Enter to keep unchanged)", sc)
	if err != nil {
		return fmt.Errorf("read client secret: %w", err)
	}

	settings := map[string]string{config.KeyOAuthGoogleClientID: clientID}
	if clientSecret != "" {
		settings[config.KeyOAuthGoogleClientSecret] = clientSecret
	}
	return saveSettings(ctx, r, settings)
}

// --- helpers ---

func saveSettings(ctx context.Context, r *repo.SettingsRepo, settings map[string]string) error {
	for k, v := range settings {
		if err := r.Set(ctx, k, v); err != nil {
			return fmt.Errorf("save %s: %w", k, err)
		}
	}
	return nil
}

// applyDefaults fills in any missing keys in current with their application defaults.
func applyDefaults(current map[string]string) {
	if current == nil {
		return
	}
	for k, v := range config.DefaultValues() {
		if current[k] == "" {
			current[k] = v
		}
	}
}

func promptText(sc *bufio.Scanner, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	if sc.Scan() {
		if v := strings.TrimSpace(sc.Text()); v != "" {
			return v
		}
	}
	return defaultVal
}

func promptPassword(label string, sc *bufio.Scanner) (string, error) {
	fmt.Printf("%s: ", label)

	// 1. stdin is a real TTY
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		pwd, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(pwd), nil
	}

	// 2. /dev/tty is available (IDE terminal, tmux, etc.)
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close() //nolint:errcheck
		pwd, err := term.ReadPassword(int(tty.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(pwd), nil
	}

	// 3. No TTY at all — read as plain text from stdin
	fmt.Print("(input visible) ")
	if sc.Scan() {
		fmt.Println()
		return strings.TrimSpace(sc.Text()), nil
	}
	return "", fmt.Errorf("read password: no input")
}

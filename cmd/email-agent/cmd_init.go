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

func newInitCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the encrypted database with an interactive setup wizard",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd.Context(), resolveDBPath(*dbPath))
		},
	}
}

func runInit(ctx context.Context, path string) error {
	sc := bufio.NewScanner(os.Stdin)
	_, dbExists := os.Stat(path)

	var hexKey string

	if dbExists == nil {
		// DB already exists — confirm override
		fmt.Printf("Database already exists at %s.\n", path)
		answer := promptText(sc, "Override settings?", "n")
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Cancelled.")
			return nil
		}
		fmt.Println()

		// Load existing key — do not regenerate (DB is already encrypted with it)
		var err error
		hexKey, err = keychain.Load()
		if err != nil {
			return err
		}
	} else {
		// Fresh setup
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

	fmt.Println("IMAP Account")
	name := promptText(sc, "  Name", "")
	email := promptText(sc, "  Email", "")
	host := promptText(sc, "  Host", "")
	port := promptText(sc, "  Port", "993")
	username := promptText(sc, "  Username", email)
	password, err := promptPassword("  Password", sc)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	tlsVal := promptText(sc, "  TLS", "true")
	pollInterval := promptText(sc, "  Poll interval", "1m")

	fmt.Println()
	fmt.Println("Telegram")
	botToken, err := promptPassword("  Bot token", sc)
	if err != nil {
		return fmt.Errorf("read bot token: %w", err)
	}
	chatID := promptText(sc, "  Chat ID", "")

	fmt.Println()
	fmt.Println("Notifications")
	minImportance := promptText(sc, "  Min importance (critical/important/maybe)", "important")

	settings := map[string]string{
		"account.name":                name,
		"account.email":               email,
		"account.imap.host":           host,
		"account.imap.port":           port,
		"account.imap.username":       username,
		"account.imap.password":       password,
		"account.imap.tls":            tlsVal,
		"account.poll_interval":       pollInterval,
		"telegram.bot_token":          botToken,
		"telegram.chat_id":            chatID,
		"notification.min_importance": minImportance,
		"log.level":                   "info",
		"dev_mode":                    "false",
	}

	settingsRepo := repo.NewSettingsRepo(sqlDB)
	for k, v := range settings {
		if err := settingsRepo.Set(ctx, k, v); err != nil {
			return fmt.Errorf("save %s: %w", k, err)
		}
	}

	if _, err := config.Load(ctx, settingsRepo); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	action := "created"
	if dbExists == nil {
		action = "updated"
	}
	fmt.Printf("\nDone. Database %s at %s\nRun 'email-agent run' to start.\n", action, path)
	return nil
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

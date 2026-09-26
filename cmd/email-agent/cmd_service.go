package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/paperspell/email-assistant/internal/auth/keychain"
)

const serviceUnitName = "email-agent.service"

// servicePaths are the files a user-level installation touches.
type servicePaths struct {
	unit string // the systemd user unit
	env  string // EnvironmentFile holding the database key, 0600
	data string // the database directory the daemon writes
}

func userServicePaths(home string) servicePaths {
	return servicePaths{
		unit: filepath.Join(home, ".config", "systemd", "user", serviceUnitName),
		env:  filepath.Join(home, ".config", "email-agent", "env"),
		data: filepath.Join(home, ".email-agent"),
	}
}

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Run the daemon as a systemd user service",
		Long: "Installs email-agent as a systemd *user* unit: no root, no sudo, and the unit follows\n" +
			"the binary that ran this command. The database key is kept in a 0600 file the unit\n" +
			"reads, never in the unit itself.\n\n" +
			"A hardened system-wide unit for root installs is in contrib/email-agent.service.",
	}
	cmd.AddCommand(newServiceInstallCmd(), newServiceUninstallCmd())
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	var start bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the unit and the key file, then print the systemctl steps",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServiceInstall(start)
		},
	}
	cmd.Flags().BoolVar(&start, "start", false,
		"also run 'systemctl --user daemon-reload' and 'enable --now'")
	return cmd
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the service and remove the unit; the database and key file stay",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServiceUninstall()
		},
	}
}

func runServiceInstall(start bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	// The unit must point at the real file: a symlink under ~/.local/bin may be
	// replaced by an upgrade, and `go run` leaves a temporary path that will not
	// exist tomorrow — which is why the README says to install the binary first.
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	p := userServicePaths(home)

	if err := os.MkdirAll(filepath.Dir(p.unit), 0o755); err != nil {
		return fmt.Errorf("create unit directory: %w", err)
	}
	if err := os.WriteFile(p.unit, []byte(renderUserUnit(exe, p)), 0o644); err != nil { //nolint:gosec // unit is public
		return fmt.Errorf("write unit: %w", err)
	}
	fmt.Printf("Wrote %s\n", p.unit)

	key := os.Getenv(keychain.EnvKey)
	created, err := ensureEnvFile(p.env, key)
	if err != nil {
		return err
	}
	switch {
	case created && key != "":
		fmt.Printf("Wrote %s (0600) with %s from the environment\n", p.env, keychain.EnvKey)
	case created:
		fmt.Printf("Wrote %s (0600) — %s is EMPTY.\n", p.env, keychain.EnvKey)
		fmt.Printf("  The daemon will not start until you put the key there (the one 'email-agent init' printed).\n")
	default:
		fmt.Printf("Kept existing %s\n", p.env)
	}

	fmt.Println("\nNext:")
	fmt.Println("  systemctl --user daemon-reload")
	fmt.Println("  systemctl --user enable --now " + serviceUnitName)
	fmt.Println("  loginctl enable-linger \"$USER\"   # keep it running after you log out, and start it at boot")
	fmt.Println("  journalctl --user -u email-agent -f")

	if !start {
		return nil
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", serviceUnitName},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %v: %w\n%s", args, err, out)
		}
	}
	fmt.Println("\nService enabled and started.")
	return nil
}

func runServiceUninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	p := userServicePaths(home)
	// Best effort: a unit that was never enabled makes systemctl complain, and
	// that is not a reason to leave the file behind.
	if out, err := exec.Command("systemctl", "--user", "disable", "--now", serviceUnitName).CombinedOutput(); err != nil {
		fmt.Printf("systemctl: %s\n", firstLine(out))
	}
	if err := os.Remove(p.unit); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove unit: %w", err)
	}
	fmt.Printf("Removed %s\n", p.unit)
	fmt.Printf("Kept %s and %s — delete them yourself if you mean it.\n", p.env, p.data)
	fmt.Println("\nNext:\n  systemctl --user daemon-reload")
	return nil
}

// ensureEnvFile creates the EnvironmentFile with the key, or with an empty
// placeholder when none is in the environment. An existing file is never
// touched: it may hold the only copy of the key that decrypts the database.
func ensureEnvFile(path, key string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	content := keychain.EnvKey + "=" + key + "\n"
	if key == "" {
		content = "# Encryption key for the email-agent database. 'email-agent init' prints it once.\n" +
			"# This file is read by the systemd unit; keep it 0600.\n" + content
	}
	if _, err := f.WriteString(content); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// renderUserUnit is the user-level unit. It carries only the sandboxing that
// works without root: the mount-namespace options (ProtectSystem, PrivateTmp,
// ReadWritePaths) need user namespaces the host may not allow, and a unit that
// fails to start on some hosts is worse than one with fewer restrictions. The
// full set lives in contrib/email-agent.service for system-wide installs.
func renderUserUnit(exe string, p servicePaths) string {
	return fmt.Sprintf(`[Unit]
Description=Email Agent (IMAP -> Telegram)
Documentation=https://github.com/paperspell/email-assistant
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# The database key comes from this 0600 file, not from the unit: unit files are
# readable by anyone who can list the directory.
EnvironmentFile=%s
ExecStart=%s run
Restart=always
RestartSec=15
# Some hosts have no working IPv6 route to api.telegram.org. The cgo resolver
# honours /etc/gai.conf; the pure-Go one does not. Harmless where cgo is absent.
Environment=GODEBUG=netdns=cgo

# MemoryDenyWriteExecute is deliberately absent: SQLite here runs on wasm,
# compiled to machine code at start, and denying W^X memory breaks every query.
NoNewPrivileges=yes
LockPersonality=yes
RestrictSUIDSGID=yes

[Install]
WantedBy=default.target
`, p.env, exe)
}

func firstLine(b []byte) string {
	for i, c := range b {
		if c == '\n' {
			return string(b[:i])
		}
	}
	return string(b)
}

package keychain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

const (
	service     = "email-agent"
	keyringUser = "db-key"

	// EnvKey is the environment variable name used as a fallback when the OS
	// keychain is not available (e.g. headless Linux servers).
	EnvKey = "EMAIL_AGENT_KEY"
)

// Generate creates a new random 32-byte encryption key and returns it as a
// 64-character hex string.
func Generate() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return hex.EncodeToString(key), nil
}

// Save stores hexKey in the OS keychain.
// Returns false (without error) when the keychain is not available — the
// caller should then instruct the user to set the EMAIL_AGENT_KEY env var.
func Save(hexKey string) (bool, error) {
	if err := keyring.Set(service, keyringUser, hexKey); err != nil {
		return false, nil
	}
	return true, nil
}

// Load returns the encryption key from the OS keychain, falling back to the
// EMAIL_AGENT_KEY environment variable. Returns an error if neither is set.
func Load() (string, error) {
	key, err := keyring.Get(service, keyringUser)
	if err == nil && key != "" {
		return key, nil
	}
	if v := os.Getenv(EnvKey); v != "" {
		return v, nil
	}
	return "", fmt.Errorf(
		"encryption key not found: run 'email-agent init' or set the %s env var", EnvKey,
	)
}

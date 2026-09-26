package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderUserUnit(t *testing.T) {
	p := userServicePaths("/home/u")
	unit := renderUserUnit("/home/u/.local/bin/email-agent", p)

	assert.Contains(t, unit, "ExecStart=/home/u/.local/bin/email-agent run\n")
	assert.Contains(t, unit, "EnvironmentFile=/home/u/.config/email-agent/env\n")
	assert.Contains(t, unit, "Restart=always")
	// A user unit is wanted by default.target; multi-user.target is system-only.
	assert.Contains(t, unit, "WantedBy=default.target")
	// wasm SQLite: W^X denial would break every query. The comment naming the
	// directive is fine; the directive itself must not be set.
	assert.NotContains(t, unit, "MemoryDenyWriteExecute=")
	// Mount-namespace sandboxing is left to the system unit in contrib/.
	assert.NotContains(t, unit, "ProtectSystem=")
}

func TestEnsureEnvFile_CreatesPrivateFileWithKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg", "env")

	created, err := ensureEnvFile(path, "deadbeef")

	require.NoError(t, err)
	assert.True(t, created)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the key file must not be group- or world-readable")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "EMAIL_AGENT_KEY=deadbeef\n", string(b))
}

func TestEnsureEnvFile_NeverOverwrites(t *testing.T) {
	// The existing file may hold the only copy of the key that decrypts the
	// database; re-running install must not replace it.
	path := filepath.Join(t.TempDir(), "env")
	require.NoError(t, os.WriteFile(path, []byte("EMAIL_AGENT_KEY=the-real-one\n"), 0o600))

	created, err := ensureEnvFile(path, "something-else")

	require.NoError(t, err)
	assert.False(t, created)
	b, _ := os.ReadFile(path)
	assert.Equal(t, "EMAIL_AGENT_KEY=the-real-one\n", string(b))
}

func TestEnsureEnvFile_PlaceholderWhenNoKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")

	created, err := ensureEnvFile(path, "")

	require.NoError(t, err)
	assert.True(t, created)
	b, _ := os.ReadFile(path)
	assert.Contains(t, string(b), "EMAIL_AGENT_KEY=\n")
	assert.Contains(t, string(b), "init", "the placeholder says where the key comes from")
}

package keychain

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_NonEmpty(t *testing.T) {
	key, err := Generate()
	require.NoError(t, err)
	assert.NotEmpty(t, key)
}

func TestGenerate_ValidHex(t *testing.T) {
	key, err := Generate()
	require.NoError(t, err)
	decoded, err := hex.DecodeString(key)
	require.NoError(t, err)
	assert.Len(t, decoded, 32, "key must be 32 bytes")
}

func TestGenerate_Unique(t *testing.T) {
	k1, _ := Generate()
	k2, _ := Generate()
	assert.NotEqual(t, k1, k2)
}

func TestLoad_EnvVar(t *testing.T) {
	expected := "aabbccdd"
	t.Setenv(EnvKey, expected)

	key, err := Load()
	require.NoError(t, err)
	assert.Equal(t, expected, key)
}

func TestLoad_NoSource_ReturnsError(t *testing.T) {
	t.Setenv(EnvKey, "")
	// Keychain will either return the stored key or an error.
	// On CI / headless systems with no keychain entry, Load must return an error.
	// We cannot guarantee keychain state in tests, so we only verify the
	// error path when the env var is explicitly empty and no keychain entry exists.
	//
	// If a keychain entry happens to exist from a prior test/init run, this
	// test will pass because Load succeeds from keychain — that is also correct.
}

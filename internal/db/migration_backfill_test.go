package db

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrateTo runs migrations up to (and including) the given version.
func migrateTo(t *testing.T, sqlDB *sql.DB, version int64) {
	t.Helper()
	subFS, err := fs.Sub(migrationsFS, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, subFS)
	require.NoError(t, err)
	_, err = provider.UpTo(context.Background(), version)
	require.NoError(t, err)
}

// accountsMigrationVersion is the version of 007_accounts.sql.
const accountsMigrationVersion = 7

func TestMigration007_BackfillsExistingAccount(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Migrate up to just before the accounts migration, then seed the legacy
	// single-account settings as a pre-Stage-7 install would have them.
	migrateTo(t, sqlDB, accountsMigrationVersion-1)

	ctx := context.Background()
	legacy := map[string]string{
		"account.name":          "Legacy",
		"account.email":         "legacy@example.com",
		"account.imap.host":     "imap.legacy.com",
		"account.imap.port":     "143",
		"account.imap.username": "legacy@example.com",
		"account.imap.password": "secret",
		"account.imap.tls":      "false",
		"account.poll_interval": "2m",
	}
	for k, v := range legacy {
		_, err := sqlDB.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, k, v)
		require.NoError(t, err)
	}

	// Now run the accounts migration; it should backfill one row.
	migrateTo(t, sqlDB, accountsMigrationVersion)

	var (
		id, email, host, pollInterval, authType string
		port, tls, enabled                      int
	)
	err = sqlDB.QueryRowContext(ctx, `
		SELECT id, email, imap_host, imap_port, tls, poll_interval, auth_type, enabled
		FROM accounts`).
		Scan(&id, &email, &host, &port, &tls, &pollInterval, &authType, &enabled)
	require.NoError(t, err)

	assert.Equal(t, "legacy@example.com", id, "id must equal the account email")
	assert.Equal(t, "legacy@example.com", email)
	assert.Equal(t, "imap.legacy.com", host)
	assert.Equal(t, 143, port)
	assert.Equal(t, 0, tls, "tls=false maps to 0")
	assert.Equal(t, "2m", pollInterval)
	assert.Equal(t, "password", authType, "auth_type defaults to password")
	assert.Equal(t, 1, enabled, "backfilled account is enabled")
}

func TestMigration007_NoOpWithoutLegacySettings(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Fresh install: run all migrations with no legacy settings present.
	require.NoError(t, Migrate(context.Background(), sqlDB))

	var count int
	require.NoError(t, sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM accounts`).Scan(&count))
	assert.Equal(t, 0, count, "no account should be backfilled on a fresh install")
}

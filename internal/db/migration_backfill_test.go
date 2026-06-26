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

// pollDefaultMigrationVersion is the version of 009_poll_interval_default.sql.
const pollDefaultMigrationVersion = 9

func TestMigration009_BackfillsOldPollDefault(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Migrate up to just before the poll-default migration, then seed accounts:
	// one on the old 1m default and one with a deliberate non-default value.
	migrateTo(t, sqlDB, pollDefaultMigrationVersion-1)

	ctx := context.Background()
	for _, a := range []struct{ id, poll string }{
		{"old@example.com", "1m"},
		{"custom@example.com", "5m"},
	} {
		_, err := sqlDB.ExecContext(ctx, `
			INSERT INTO accounts (id, email, imap_host, poll_interval)
			VALUES (?, ?, ?, ?)`, a.id, a.id, "imap.example.com", a.poll)
		require.NoError(t, err)
	}

	migrateTo(t, sqlDB, pollDefaultMigrationVersion)

	poll := func(id string) string {
		var v string
		require.NoError(t, sqlDB.QueryRowContext(ctx,
			`SELECT poll_interval FROM accounts WHERE id = ?`, id).Scan(&v))
		return v
	}
	assert.Equal(t, "10m", poll("old@example.com"), "old 1m default is bumped to 10m")
	assert.Equal(t, "5m", poll("custom@example.com"), "deliberate value is untouched")
}

// filterRulesMigrationVersion is the version of 010_filter_rules.sql.
const filterRulesMigrationVersion = 10

// scoresPerAccountMigrationVersion is the version of 011_scores_per_account.sql.
const scoresPerAccountMigrationVersion = 11

func TestMigration010_SeedsDefaultClausesWhenLLMConfigured(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	migrateTo(t, sqlDB, filterRulesMigrationVersion-1)
	ctx := context.Background()

	_, err = sqlDB.ExecContext(ctx,
		`INSERT INTO accounts (id, email, imap_host) VALUES ('acc-1', 'a@x.com', 'imap.x.com')`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ('llm.provider', 'anthropic', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	migrateTo(t, sqlDB, filterRulesMigrationVersion)

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM llm_clauses WHERE account_id = 'acc-1' AND source = 'default'`).Scan(&count))
	assert.Equal(t, 3, count, "the three default ignore clauses are seeded")
}

func TestMigration010_NoClausesWithoutLLM(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	migrateTo(t, sqlDB, filterRulesMigrationVersion-1)
	ctx := context.Background()
	_, err = sqlDB.ExecContext(ctx,
		`INSERT INTO accounts (id, email, imap_host) VALUES ('acc-1', 'a@x.com', 'imap.x.com')`)
	require.NoError(t, err)

	migrateTo(t, sqlDB, filterRulesMigrationVersion)

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_clauses`).Scan(&count))
	assert.Equal(t, 0, count, "no clauses seeded when no LLM provider configured")
}

func TestMigration011_RecreatesScoresPerAccount(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	migrateTo(t, sqlDB, filterRulesMigrationVersion)
	ctx := context.Background()
	// Seed a row in the old global senders table.
	_, err = sqlDB.ExecContext(ctx,
		`INSERT INTO senders (id, email, importance_score, seen_count, updated_at)
		 VALUES ('s1', 'a@x.com', 50, 1, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	migrateTo(t, sqlDB, scoresPerAccountMigrationVersion)

	// Old data is dropped (from-scratch decision).
	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM senders`).Scan(&count))
	assert.Equal(t, 0, count)

	// The new schema is per-account: the same address can coexist across accounts.
	const ins = `INSERT INTO senders (id, account_id, email, importance_score, seen_count, updated_at)
		VALUES (?, ?, 'a@x.com', ?, 1, CURRENT_TIMESTAMP)`
	for _, row := range [][]any{{"n1", "acc-a", 10}, {"n2", "acc-b", 90}} {
		_, err = sqlDB.ExecContext(ctx, ins, row...)
		require.NoError(t, err)
	}
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM senders`).Scan(&count))
	assert.Equal(t, 2, count)
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

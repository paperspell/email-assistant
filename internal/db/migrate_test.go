package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrate_FreshSchema runs all migrations on a clean database and asserts the
// consolidated baseline produces the full schema. The migration history was
// squashed into a single 001_initial.sql, so version-by-version backfill tests no
// longer apply; the repo round-trip suites exercise the schema's behaviour.
func TestMigrate_FreshSchema(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, Migrate(context.Background(), sqlDB))

	wantTables := []string{
		"settings", "accounts", "emails", "sync_state", "classifications",
		"senders", "domains", "llm_audit_log", "filter_rules", "llm_clauses",
		"digests", "digest_items", "pending_actions",
	}
	for _, tbl := range wantTables {
		var name string
		err := sqlDB.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, tbl).Scan(&name)
		require.NoErrorf(t, err, "table %q should exist", tbl)
		assert.Equal(t, tbl, name)
	}

	// A couple of key indices/columns the repos depend on.
	var idx string
	require.NoError(t, sqlDB.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='index' AND name = 'idx_emails_account_uid'`).Scan(&idx))

	var col int
	require.NoError(t, sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM pragma_table_info('emails') WHERE name IN ('decided_by','list_id')`).Scan(&col))
	assert.Equal(t, 2, col, "emails has decided_by and list_id")
}

// TestMigrate_Idempotent verifies a second Migrate is a no-op (no pending steps).
func TestMigrate_Idempotent(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, Migrate(context.Background(), sqlDB))
	require.NoError(t, Migrate(context.Background(), sqlDB))
}

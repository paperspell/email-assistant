//go:build migration

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

func TestMigrations_Up(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	err = Migrate(context.Background(), sqlDB)
	require.NoError(t, err)

	assertTableExists(t, sqlDB, "emails")
	assertTableExists(t, sqlDB, "sync_state")
}

func TestMigrations_Idempotent(t *testing.T) {
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, Migrate(context.Background(), sqlDB))
	require.NoError(t, Migrate(context.Background(), sqlDB), "second run must be idempotent")
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, table, name)
}

// A digest sent before 003 was a single message. The migration must register
// it as part 1 of itself, or a `/important` reply to an older digest would stop
// resolving the moment the daemon restarts on the new schema.
func TestMigration003_RegistersExistingDigestsAsPartOne(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	subFS, err := fs.Sub(migrationsFS, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, subFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 2)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO digests (id, account_id, digest_date, tg_message_id, sent_at)
		VALUES ('old', 'a@b.com', '2026-09-01', 4242, '2026-09-01T20:00:00Z')`)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var digestID string
	var partNo int
	err = sqlDB.QueryRowContext(ctx,
		`SELECT digest_id, part_no FROM digest_messages WHERE tg_message_id = 4242`,
	).Scan(&digestID, &partNo)
	require.NoError(t, err, "the pre-existing digest must be reachable by its message id")
	assert.Equal(t, "old", digestID)
	assert.Equal(t, 1, partNo)
}

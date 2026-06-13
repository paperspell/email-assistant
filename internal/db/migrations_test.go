//go:build migration

package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations_Up(t *testing.T) {
	sqlDB, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	err = Migrate(context.Background(), sqlDB)
	require.NoError(t, err)

	assertTableExists(t, sqlDB, "emails")
	assertTableExists(t, sqlDB, "sync_state")
}

func TestMigrations_Idempotent(t *testing.T) {
	sqlDB, err := Open(":memory:")
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

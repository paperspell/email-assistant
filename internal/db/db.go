package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/pressly/goose/v3"

	_ "github.com/ncruces/go-sqlite3/driver"       // registers the "sqlite3" database/sql driver
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum" // registers the adiantum VFS for encryption
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (or creates) the SQLite database at path.
// If hexKey is non-empty the database is opened with Adiantum encryption.
// Pass an empty hexKey for unencrypted databases (tests, :memory:).
func Open(path, hexKey string) (*sql.DB, error) {
	dsn := buildDSN(path, hexKey)

	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1) // SQLite does not support concurrent writes
	return sqlDB, nil
}

// Migrate runs all pending migrations against db using embedded SQL files.
func Migrate(ctx context.Context, sqlDB *sql.DB) error {
	subFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("resolve migrations fs: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, subFS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

func buildDSN(path, hexKey string) string {
	if path == ":memory:" {
		return "file::memory:?mode=memory"
	}
	if hexKey == "" {
		return path
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	return fmt.Sprintf("file:%s?vfs=adiantum&hexkey=%s",
		filepath.ToSlash(absPath), hexKey)
}

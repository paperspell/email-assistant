package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SettingsRepo provides key-value persistence for application configuration.
type SettingsRepo struct {
	db *sql.DB
}

// NewSettingsRepo creates a SettingsRepo backed by db.
func NewSettingsRepo(db *sql.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

// Get retrieves the value for key. Returns "", nil if the key does not exist.
func (r *SettingsRepo) Get(ctx context.Context, key string) (string, error) {
	const q = `SELECT value FROM settings WHERE key = ?`
	var value string
	err := r.db.QueryRowContext(ctx, q, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

// Set inserts or updates key with value.
func (r *SettingsRepo) Set(ctx context.Context, key, value string) error {
	const q = `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET
			value      = excluded.value,
			updated_at = excluded.updated_at
	`
	_, err := r.db.ExecContext(ctx, q, key, value,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// GetAll returns all settings as a key→value map.
func (r *SettingsRepo) GetAll(ctx context.Context) (map[string]string, error) {
	const q = `SELECT key, value FROM settings`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get all settings: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return result, nil
}

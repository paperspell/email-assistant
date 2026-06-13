package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// SyncStateRepo provides persistence for per-account sync state.
type SyncStateRepo struct {
	db *sql.DB
}

// NewSyncStateRepo creates a SyncStateRepo backed by db.
func NewSyncStateRepo(db *sql.DB) *SyncStateRepo {
	return &SyncStateRepo{db: db}
}

// Get retrieves the sync state for accountID.
// Returns nil, nil if no state exists yet.
func (r *SyncStateRepo) Get(ctx context.Context, accountID string) (*domain.SyncState, error) {
	const q = `SELECT account_id, last_uid, synced_at FROM sync_state WHERE account_id = ?`
	row := r.db.QueryRowContext(ctx, q, accountID)

	var s domain.SyncState
	var syncedAtStr string
	err := row.Scan(&s.AccountID, &s.LastUID, &syncedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan sync state: %w", err)
	}

	s.SyncedAt, err = time.Parse(time.RFC3339, syncedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse synced_at: %w", err)
	}
	return &s, nil
}

// Upsert inserts or updates the sync state for an account.
func (r *SyncStateRepo) Upsert(ctx context.Context, s domain.SyncState) error {
	const q = `
		INSERT INTO sync_state (account_id, last_uid, synced_at)
		VALUES (?, ?, ?)
		ON CONFLICT (account_id) DO UPDATE SET
			last_uid  = excluded.last_uid,
			synced_at = excluded.synced_at
	`
	_, err := r.db.ExecContext(ctx, q,
		s.AccountID,
		s.LastUID,
		s.SyncedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert sync state: %w", err)
	}
	return nil
}

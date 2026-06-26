package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// SenderRepo provides persistence for per-sender statistics.
type SenderRepo struct {
	db *sql.DB
}

// NewSenderRepo creates a SenderRepo backed by db.
func NewSenderRepo(db *sql.DB) *SenderRepo {
	return &SenderRepo{db: db}
}

// Get retrieves the sender record for emailAddr within an account.
// Returns nil, nil if not found.
func (r *SenderRepo) Get(ctx context.Context, accountID, emailAddr string) (*domain.Sender, error) {
	const q = `
		SELECT id, account_id, email, importance_score, seen_count, updated_at
		FROM senders WHERE account_id = ? AND email = ?
	`
	row := r.db.QueryRowContext(ctx, q, accountID, emailAddr)

	var s domain.Sender
	var updatedAtStr string
	err := row.Scan(&s.ID, &s.AccountID, &s.Email, &s.ImportanceScore, &s.SeenCount, &updatedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan sender: %w", err)
	}
	s.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse sender updated_at: %w", err)
	}
	return &s, nil
}

// Upsert inserts or updates a sender record, keyed by (account_id, email).
func (r *SenderRepo) Upsert(ctx context.Context, s domain.Sender) error {
	const q = `
		INSERT INTO senders (id, account_id, email, importance_score, seen_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, email) DO UPDATE SET
			importance_score = excluded.importance_score,
			seen_count       = excluded.seen_count,
			updated_at       = excluded.updated_at
	`
	_, err := r.db.ExecContext(ctx, q,
		s.ID, s.AccountID, s.Email, s.ImportanceScore, s.SeenCount,
		s.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert sender: %w", err)
	}
	return nil
}

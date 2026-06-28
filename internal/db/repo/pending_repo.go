package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// PendingRepo persists the in-progress Telegram interaction for each chat.
type PendingRepo struct {
	db *sql.DB
}

// NewPendingRepo creates a PendingRepo backed by db.
func NewPendingRepo(db *sql.DB) *PendingRepo {
	return &PendingRepo{db: db}
}

// Set stores (overwriting) the pending action for a chat.
func (r *PendingRepo) Set(ctx context.Context, p domain.PendingAction) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO pending_actions (chat_id, kind, email_id, account_id, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat_id) DO UPDATE SET
			kind       = excluded.kind,
			email_id   = excluded.email_id,
			account_id = excluded.account_id,
			payload    = excluded.payload,
			created_at = excluded.created_at
	`
	_, err := r.db.ExecContext(ctx, q,
		p.ChatID, p.Kind, p.EmailID, p.AccountID, p.Payload, p.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("set pending action: %w", err)
	}
	return nil
}

// Get returns the pending action for a chat, or nil if none.
func (r *PendingRepo) Get(ctx context.Context, chatID int64) (*domain.PendingAction, error) {
	const q = `SELECT chat_id, kind, email_id, account_id, payload, created_at
		FROM pending_actions WHERE chat_id = ?`
	var (
		p          domain.PendingAction
		createdStr string
	)
	err := r.db.QueryRowContext(ctx, q, chatID).Scan(
		&p.ChatID, &p.Kind, &p.EmailID, &p.AccountID, &p.Payload, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pending action: %w", err)
	}
	if t, perr := time.Parse(time.RFC3339, createdStr); perr == nil {
		p.CreatedAt = t
	}
	return &p, nil
}

// Delete removes the pending action for a chat.
func (r *PendingRepo) Delete(ctx context.Context, chatID int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM pending_actions WHERE chat_id = ?`, chatID); err != nil {
		return fmt.Errorf("delete pending action: %w", err)
	}
	return nil
}

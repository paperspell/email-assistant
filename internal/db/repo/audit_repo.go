package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AuditEntry records metadata about a single outbound LLM call.
type AuditEntry struct {
	ID          string
	EmailID     string
	Provider    string
	Model       string
	ContentMode string
	BytesSent   int
	CreatedAt   time.Time
}

// AuditRepo persists LLM audit log entries.
type AuditRepo struct {
	db *sql.DB
}

// NewAuditRepo creates an AuditRepo backed by db.
func NewAuditRepo(db *sql.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

// List returns the most recent audit entries, newest first.
func (r *AuditRepo) List(ctx context.Context, limit int) ([]AuditEntry, error) {
	const q = `
		SELECT id, email_id, provider, model, content_mode, bytes_sent, created_at
		FROM llm_audit_log
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := r.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var createdAtStr string
		if err := rows.Scan(&e.ID, &e.EmailID, &e.Provider, &e.Model,
			&e.ContentMode, &e.BytesSent, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		e.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return entries, nil
}

// Save inserts an audit entry into llm_audit_log.
func (r *AuditRepo) Save(ctx context.Context, e AuditEntry) error {
	const q = `
		INSERT INTO llm_audit_log (id, email_id, provider, model, content_mode, bytes_sent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(ctx, q,
		e.ID, e.EmailID, e.Provider, e.Model, e.ContentMode,
		e.BytesSent,
		e.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save audit entry: %w", err)
	}
	return nil
}

package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// DomainRepo provides persistence for per-domain statistics.
type DomainRepo struct {
	db *sql.DB
}

// NewDomainRepo creates a DomainRepo backed by db.
func NewDomainRepo(db *sql.DB) *DomainRepo {
	return &DomainRepo{db: db}
}

// Get retrieves the domain record for domainName within an account.
// Returns nil, nil if not found.
func (r *DomainRepo) Get(ctx context.Context, accountID, domainName string) (*domain.Record, error) {
	const q = `
		SELECT id, account_id, domain, importance_score, updated_at
		FROM domains WHERE account_id = ? AND domain = ?
	`
	row := r.db.QueryRowContext(ctx, q, accountID, domainName)

	var d domain.Record
	var updatedAtStr string
	err := row.Scan(&d.ID, &d.AccountID, &d.Domain, &d.ImportanceScore, &updatedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan domain: %w", err)
	}
	d.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse domain updated_at: %w", err)
	}
	return &d, nil
}

// Upsert inserts or updates a domain record, keyed by (account_id, domain).
func (r *DomainRepo) Upsert(ctx context.Context, d domain.Record) error {
	const q = `
		INSERT INTO domains (id, account_id, domain, importance_score, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (account_id, domain) DO UPDATE SET
			importance_score = excluded.importance_score,
			updated_at       = excluded.updated_at
	`
	_, err := r.db.ExecContext(ctx, q,
		d.ID, d.AccountID, d.Domain, d.ImportanceScore,
		d.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert domain: %w", err)
	}
	return nil
}

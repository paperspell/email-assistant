package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// ClauseRepo persists per-account LLM ignore clauses.
type ClauseRepo struct {
	db *sql.DB
}

// NewClauseRepo creates a ClauseRepo backed by db.
func NewClauseRepo(db *sql.DB) *ClauseRepo {
	return &ClauseRepo{db: db}
}

const clauseColumns = `id, account_id, text, enabled, source, created_at`

// List returns all clauses for an account ordered by creation time (stable 1-based
// index for the CLI).
func (r *ClauseRepo) List(ctx context.Context, accountID string) ([]domain.LLMClause, error) {
	return r.query(ctx,
		`SELECT `+clauseColumns+` FROM llm_clauses WHERE account_id = ? ORDER BY created_at, id`,
		accountID)
}

// ActiveTexts returns the text of enabled clauses for injection into the prompt.
func (r *ClauseRepo) ActiveTexts(ctx context.Context, accountID string) ([]string, error) {
	clauses, err := r.query(ctx,
		`SELECT `+clauseColumns+` FROM llm_clauses WHERE account_id = ? AND enabled = 1 ORDER BY created_at, id`,
		accountID)
	if err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(clauses))
	for _, c := range clauses {
		texts = append(texts, c.Text)
	}
	return texts, nil
}

// GetByIndex returns the clause at the 1-based position n within the account list.
func (r *ClauseRepo) GetByIndex(ctx context.Context, accountID string, n int) (*domain.LLMClause, error) {
	clauses, err := r.List(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > len(clauses) {
		return nil, fmt.Errorf("no clause #%d for account %q (have %d)", n, accountID, len(clauses))
	}
	return &clauses[n-1], nil
}

// Count returns how many clauses an account has (enabled and disabled).
func (r *ClauseRepo) Count(ctx context.Context, accountID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM llm_clauses WHERE account_id = ?`, accountID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count clauses: %w", err)
	}
	return n, nil
}

// Add inserts a new clause.
func (r *ClauseRepo) Add(ctx context.Context, c domain.LLMClause) error {
	if c.Source == "" {
		c.Source = domain.RuleSourceUser
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	const q = `INSERT INTO llm_clauses (` + clauseColumns + `) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		c.ID, c.AccountID, c.Text, boolToInt(c.Enabled), c.Source,
		c.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("add clause: %w", err)
	}
	return nil
}

// SetEnabled toggles a clause by id.
func (r *ClauseRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE llm_clauses SET enabled = ? WHERE id = ?`, boolToInt(enabled), id); err != nil {
		return fmt.Errorf("set clause enabled: %w", err)
	}
	return nil
}

// Delete removes a clause by id.
func (r *ClauseRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM llm_clauses WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete clause: %w", err)
	}
	return nil
}

func (r *ClauseRepo) query(ctx context.Context, q string, args ...any) ([]domain.LLMClause, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query clauses: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var clauses []domain.LLMClause
	for rows.Next() {
		var (
			c          domain.LLMClause
			enabled    int
			createdStr string
		)
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Text, &enabled, &c.Source, &createdStr); err != nil {
			return nil, fmt.Errorf("scan clause: %w", err)
		}
		c.Enabled = enabled != 0
		c.CreatedAt = parseDBTime(createdStr)
		clauses = append(clauses, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clauses: %w", err)
	}
	return clauses, nil
}

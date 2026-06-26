package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// RuleRepo persists per-account filter rules.
type RuleRepo struct {
	db *sql.DB
}

// NewRuleRepo creates a RuleRepo backed by db.
func NewRuleRepo(db *sql.DB) *RuleRepo {
	return &RuleRepo{db: db}
}

const ruleColumns = `id, account_id, action, type, matcher, value,
	scope_kind, scope_value, enabled, source, created_at`

// List returns all rules for an account ordered by creation time, giving a stable
// 1-based index for the CLI.
func (r *RuleRepo) List(ctx context.Context, accountID string) ([]domain.FilterRule, error) {
	return r.query(ctx,
		`SELECT `+ruleColumns+` FROM filter_rules WHERE account_id = ? ORDER BY created_at, id`,
		accountID)
}

// ListEnabled returns only enabled rules for an account (for the engine).
func (r *RuleRepo) ListEnabled(ctx context.Context, accountID string) ([]domain.FilterRule, error) {
	return r.query(ctx,
		`SELECT `+ruleColumns+` FROM filter_rules WHERE account_id = ? AND enabled = 1 ORDER BY created_at, id`,
		accountID)
}

// GetByIndex returns the rule at the 1-based position n within the account list.
func (r *RuleRepo) GetByIndex(ctx context.Context, accountID string, n int) (*domain.FilterRule, error) {
	rules, err := r.List(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > len(rules) {
		return nil, fmt.Errorf("no rule #%d for account %q (have %d)", n, accountID, len(rules))
	}
	return &rules[n-1], nil
}

// Add inserts a new rule.
func (r *RuleRepo) Add(ctx context.Context, rule domain.FilterRule) error {
	if rule.Matcher == "" {
		rule.Matcher = domain.MatcherExact
	}
	if rule.Source == "" {
		rule.Source = domain.RuleSourceUser
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now().UTC()
	}
	const q = `INSERT INTO filter_rules (` + ruleColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		rule.ID, rule.AccountID, rule.Action, rule.Type, rule.Matcher, rule.Value,
		rule.ScopeKind, rule.ScopeValue, boolToInt(rule.Enabled), rule.Source,
		rule.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("add rule: %w", err)
	}
	return nil
}

// SetEnabled toggles a rule by id.
func (r *RuleRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE filter_rules SET enabled = ? WHERE id = ?`, boolToInt(enabled), id); err != nil {
		return fmt.Errorf("set rule enabled: %w", err)
	}
	return nil
}

// UpdateValue changes the matched value and subject scope of a rule by id.
func (r *RuleRepo) UpdateValue(ctx context.Context, id, value, scopeKind, scopeValue string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE filter_rules SET value = ?, scope_kind = ?, scope_value = ? WHERE id = ?`,
		value, scopeKind, scopeValue, id); err != nil {
		return fmt.Errorf("update rule: %w", err)
	}
	return nil
}

// Delete removes a rule by id.
func (r *RuleRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM filter_rules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	return nil
}

func (r *RuleRepo) query(ctx context.Context, q string, args ...any) ([]domain.FilterRule, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var rules []domain.FilterRule
	for rows.Next() {
		var (
			rule       domain.FilterRule
			enabled    int
			createdStr string
		)
		if err := rows.Scan(
			&rule.ID, &rule.AccountID, &rule.Action, &rule.Type, &rule.Matcher, &rule.Value,
			&rule.ScopeKind, &rule.ScopeValue, &enabled, &rule.Source, &createdStr,
		); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		rule.Enabled = enabled != 0
		rule.CreatedAt = parseDBTime(createdStr)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rules: %w", err)
	}
	return rules, nil
}

// parseDBTime parses a timestamp written either as RFC3339 (app inserts) or the
// SQLite CURRENT_TIMESTAMP form. Returns zero time on failure (display-only field).
func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}

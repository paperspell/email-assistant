package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// ClassificationRepo provides persistence for email classification results.
type ClassificationRepo struct {
	db *sql.DB
}

// NewClassificationRepo creates a ClassificationRepo backed by db.
func NewClassificationRepo(db *sql.DB) *ClassificationRepo {
	return &ClassificationRepo{db: db}
}

// Save stores a classification record.
func (r *ClassificationRepo) Save(ctx context.Context, c domain.Classification) error {
	reasonJSON, err := json.Marshal(c.Reason)
	if err != nil {
		return fmt.Errorf("marshal reason: %w", err)
	}
	const q = `
		INSERT INTO classifications (id, email_id, level, category, score, reason, classified_at, source, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = r.db.ExecContext(ctx, q,
		c.ID, c.EmailID, string(c.Level), string(c.Category),
		c.Score, string(reasonJSON),
		c.ClassifiedAt.UTC().Format(time.RFC3339),
		c.Source,
		c.Summary,
	)
	if err != nil {
		return fmt.Errorf("save classification: %w", err)
	}
	return nil
}

// GetByEmailID retrieves the rule_based classification for a given email ID.
// When both rule_based and llm classifications exist, rule_based is preferred.
// Returns nil, nil if not found.
func (r *ClassificationRepo) GetByEmailID(ctx context.Context, emailID string) (*domain.Classification, error) {
	const q = `
		SELECT id, email_id, level, category, score, reason, classified_at, source, summary
		FROM classifications
		WHERE email_id = ?
		ORDER BY (source = 'rule_based') DESC
		LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, q, emailID)
	return scanClassification(row)
}

// GetByEmailIDAndSource retrieves the classification for a given email and source.
// Returns nil, nil if not found.
func (r *ClassificationRepo) GetByEmailIDAndSource(
	ctx context.Context, emailID, source string,
) (*domain.Classification, error) {
	const q = `
		SELECT id, email_id, level, category, score, reason, classified_at, source, summary
		FROM classifications
		WHERE email_id = ? AND source = ?
	`
	row := r.db.QueryRowContext(ctx, q, emailID, source)
	return scanClassification(row)
}

// GetAllByEmailID retrieves all classifications for a given email.
func (r *ClassificationRepo) GetAllByEmailID(ctx context.Context, emailID string) ([]domain.Classification, error) {
	const q = `
		SELECT id, email_id, level, category, score, reason, classified_at, source, summary
		FROM classifications
		WHERE email_id = ?
		ORDER BY (source = 'rule_based') DESC
	`
	rows, err := r.db.QueryContext(ctx, q, emailID)
	if err != nil {
		return nil, fmt.Errorf("query classifications: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var result []domain.Classification
	for rows.Next() {
		c, err := scanClassificationRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return result, nil
}

func scanClassification(row *sql.Row) (*domain.Classification, error) {
	var c domain.Classification
	var level, category, reasonJSON, classifiedAtStr string

	err := row.Scan(
		&c.ID, &c.EmailID, &level, &category, &c.Score,
		&reasonJSON, &classifiedAtStr, &c.Source, &c.Summary,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan classification: %w", err)
	}
	return finishClassification(&c, level, category, reasonJSON, classifiedAtStr)
}

func scanClassificationRow(rows *sql.Rows) (*domain.Classification, error) {
	var c domain.Classification
	var level, category, reasonJSON, classifiedAtStr string

	if err := rows.Scan(
		&c.ID, &c.EmailID, &level, &category, &c.Score,
		&reasonJSON, &classifiedAtStr, &c.Source, &c.Summary,
	); err != nil {
		return nil, fmt.Errorf("scan classification: %w", err)
	}
	return finishClassification(&c, level, category, reasonJSON, classifiedAtStr)
}

func finishClassification(
	c *domain.Classification, level, category, reasonJSON, classifiedAtStr string,
) (*domain.Classification, error) {
	c.Level = domain.ImportanceLevel(level)
	c.Category = domain.Category(category)

	if err := json.Unmarshal([]byte(reasonJSON), &c.Reason); err != nil {
		return nil, fmt.Errorf("unmarshal reason: %w", err)
	}

	var err error
	c.ClassifiedAt, err = time.Parse(time.RFC3339, classifiedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse classified_at: %w", err)
	}
	return c, nil
}

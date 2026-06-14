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
		INSERT INTO classifications (id, email_id, level, category, score, reason, classified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err = r.db.ExecContext(ctx, q,
		c.ID, c.EmailID, string(c.Level), string(c.Category),
		c.Score, string(reasonJSON),
		c.ClassifiedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save classification: %w", err)
	}
	return nil
}

// GetByEmailID retrieves the classification for a given email ID.
// Returns nil, nil if not found.
func (r *ClassificationRepo) GetByEmailID(ctx context.Context, emailID string) (*domain.Classification, error) {
	const q = `
		SELECT id, email_id, level, category, score, reason, classified_at
		FROM classifications WHERE email_id = ?
	`
	row := r.db.QueryRowContext(ctx, q, emailID)

	var c domain.Classification
	var level, category, reasonJSON, classifiedAtStr string

	err := row.Scan(&c.ID, &c.EmailID, &level, &category, &c.Score, &reasonJSON, &classifiedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan classification: %w", err)
	}

	c.Level = domain.ImportanceLevel(level)
	c.Category = domain.Category(category)

	if err := json.Unmarshal([]byte(reasonJSON), &c.Reason); err != nil {
		return nil, fmt.Errorf("unmarshal reason: %w", err)
	}

	c.ClassifiedAt, err = time.Parse(time.RFC3339, classifiedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse classified_at: %w", err)
	}
	return &c, nil
}

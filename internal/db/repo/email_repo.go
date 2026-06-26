package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// EmailRepo provides persistence for email records.
type EmailRepo struct {
	db *sql.DB
}

// NewEmailRepo creates an EmailRepo backed by db.
func NewEmailRepo(db *sql.DB) *EmailRepo {
	return &EmailRepo{db: db}
}

// Upsert inserts or replaces an email record.
func (r *EmailRepo) Upsert(ctx context.Context, e domain.Email) error {
	const q = `
		INSERT INTO emails (id, account_id, message_uid, subject, from_email, from_name, date, status, received_at, language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, message_uid) DO UPDATE SET
			subject    = excluded.subject,
			from_email = excluded.from_email,
			from_name  = excluded.from_name,
			date       = excluded.date,
			language   = excluded.language
	`
	_, err := r.db.ExecContext(ctx, q,
		e.ID, e.AccountID, e.MessageUID,
		e.Subject, e.FromEmail, e.FromName,
		e.Date.UTC().Format(time.RFC3339),
		string(e.Status),
		e.ReceivedAt.UTC().Format(time.RFC3339),
		e.Language,
	)
	if err != nil {
		return fmt.Errorf("upsert email: %w", err)
	}
	return nil
}

// GetByID retrieves an email by its primary key.
// Returns nil, nil if not found.
func (r *EmailRepo) GetByID(ctx context.Context, id string) (*domain.Email, error) {
	const q = `
		SELECT id, account_id, message_uid, subject, from_email, from_name,
		       date, status, received_at, language, telegram_message_id, decided_by
		FROM emails WHERE id = ?
	`
	row := r.db.QueryRowContext(ctx, q, id)
	return scanEmail(row)
}

// GetByAccountAndUID retrieves an email by account ID and IMAP UID.
// Returns nil, nil if not found.
func (r *EmailRepo) GetByAccountAndUID(ctx context.Context, accountID string, uid uint32) (*domain.Email, error) {
	const q = `
		SELECT id, account_id, message_uid, subject, from_email, from_name,
		       date, status, received_at, language, telegram_message_id, decided_by
		FROM emails
		WHERE account_id = ? AND message_uid = ?
	`
	row := r.db.QueryRowContext(ctx, q, accountID, uid)
	return scanEmail(row)
}

// UpdateStatus updates the status of an email by its ID.
func (r *EmailRepo) UpdateStatus(ctx context.Context, id string, status domain.EmailStatus) error {
	const q = `UPDATE emails SET status = ? WHERE id = ?`
	res, err := r.db.ExecContext(ctx, q, string(status), id)
	if err != nil {
		return fmt.Errorf("update email status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("update email status: email %q not found", id)
	}
	return nil
}

// UpdateStatusDecidedBy updates an email's status and records what filtered it
// (provenance). Used by the scheduler when an email is ignored.
func (r *EmailRepo) UpdateStatusDecidedBy(
	ctx context.Context, id string, status domain.EmailStatus, decidedBy string,
) error {
	const q = `UPDATE emails SET status = ?, decided_by = ? WHERE id = ?`
	res, err := r.db.ExecContext(ctx, q, string(status), decidedBy, id)
	if err != nil {
		return fmt.Errorf("update email status/decided_by: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("update email status/decided_by: email %q not found", id)
	}
	return nil
}

// SetTelegramMessageID stores the Telegram message ID of the notification sent for this email.
func (r *EmailRepo) SetTelegramMessageID(ctx context.Context, emailID string, msgID int64) error {
	const q = `UPDATE emails SET telegram_message_id = ? WHERE id = ?`
	res, err := r.db.ExecContext(ctx, q, msgID, emailID)
	if err != nil {
		return fmt.Errorf("set telegram_message_id: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("set telegram_message_id: email %q not found", emailID)
	}
	return nil
}

func scanEmail(row *sql.Row) (*domain.Email, error) {
	var e domain.Email
	var dateStr, receivedAtStr, status string

	err := row.Scan(
		&e.ID, &e.AccountID, &e.MessageUID,
		&e.Subject, &e.FromEmail, &e.FromName,
		&dateStr, &status, &receivedAtStr, &e.Language,
		&e.TelegramMessageID, &e.DecidedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan email: %w", err)
	}

	e.Status = domain.EmailStatus(status)
	e.Date, err = time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return nil, fmt.Errorf("parse email date: %w", err)
	}
	e.ReceivedAt, err = time.Parse(time.RFC3339, receivedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse email received_at: %w", err)
	}
	return &e, nil
}

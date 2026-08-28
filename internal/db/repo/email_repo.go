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

// Upsert inserts an email, or updates the mutable fields of the row that already
// holds this (account_id, message_uid).
// On conflict the stored row keeps the id it was inserted with, so e.ID is
// rewritten to it. Callers address the email by id afterwards — classifications
// reference emails(id), and status updates match on it — and a generated id
// that never reached the table would leave those writes pointing at nothing.
func (r *EmailRepo) Upsert(ctx context.Context, e *domain.Email) error {
	if e == nil {
		// The daemon runs unattended; a mistaken nil should surface as an error
		// rather than take the process down.
		return errors.New("upsert email: nil email")
	}
	const q = `
		INSERT INTO emails
			(id, account_id, message_uid, subject, from_email, from_name, date, status, received_at, language, list_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, message_uid) DO UPDATE SET
			subject    = excluded.subject,
			from_email = excluded.from_email,
			from_name  = excluded.from_name,
			date       = excluded.date,
			language   = excluded.language,
			list_id    = excluded.list_id
		RETURNING id
	`
	var storedID string
	err := r.db.QueryRowContext(ctx, q,
		e.ID, e.AccountID, e.MessageUID,
		e.Subject, e.FromEmail, e.FromName,
		e.Date.UTC().Format(time.RFC3339),
		string(e.Status),
		e.ReceivedAt.UTC().Format(time.RFC3339),
		e.Language, e.ListID,
	).Scan(&storedID)
	if err != nil {
		return fmt.Errorf("upsert email: %w", err)
	}
	e.ID = storedID
	return nil
}

// GetByID retrieves an email by its primary key.
// Returns nil, nil if not found.
func (r *EmailRepo) GetByID(ctx context.Context, id string) (*domain.Email, error) {
	const q = `
		SELECT id, account_id, message_uid, subject, from_email, from_name,
		       date, status, received_at, language, telegram_message_id, decided_by, list_id
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
		       date, status, received_at, language, telegram_message_id, decided_by, list_id
		FROM emails
		WHERE account_id = ? AND message_uid = ?
	`
	row := r.db.QueryRowContext(ctx, q, accountID, uid)
	return scanEmail(row)
}

// ListIgnoredByAccountInRange returns ignored emails for an account whose
// received_at falls in [from, to), ordered by received_at. Used to build the
// daily digest.
func (r *EmailRepo) ListIgnoredByAccountInRange(
	ctx context.Context, accountID string, from, to time.Time,
) ([]domain.Email, error) {
	const q = `
		SELECT id, account_id, message_uid, subject, from_email, from_name,
		       date, status, received_at, language, telegram_message_id, decided_by, list_id
		FROM emails
		WHERE account_id = ? AND status = ? AND received_at >= ? AND received_at < ?
		ORDER BY received_at`
	rows, err := r.db.QueryContext(ctx, q, accountID, string(domain.StatusIgnored),
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("query ignored emails: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var emails []domain.Email
	for rows.Next() {
		e, err := scanEmailRow(rows)
		if err != nil {
			return nil, err
		}
		emails = append(emails, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ignored emails: %w", err)
	}
	return emails, nil
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
	return scanEmailRow(row)
}

func scanEmailRow(row rowScanner) (*domain.Email, error) {
	var e domain.Email
	var dateStr, receivedAtStr, status string

	err := row.Scan(
		&e.ID, &e.AccountID, &e.MessageUID,
		&e.Subject, &e.FromEmail, &e.FromName,
		&dateStr, &status, &receivedAtStr, &e.Language,
		&e.TelegramMessageID, &e.DecidedBy, &e.ListID,
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

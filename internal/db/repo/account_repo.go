package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// AccountRepo provides persistence for configured email accounts.
type AccountRepo struct {
	db *sql.DB
}

// NewAccountRepo creates an AccountRepo backed by db.
func NewAccountRepo(db *sql.DB) *AccountRepo {
	return &AccountRepo{db: db}
}

const accountColumns = `id, name, email, imap_host, imap_port, imap_username,
	imap_password, tls, poll_interval, auth_type, enabled,
	oauth_refresh_token, oauth_access_token, oauth_token_expiry`

// List returns all accounts ordered by creation time.
func (r *AccountRepo) List(ctx context.Context) ([]domain.Account, error) {
	return r.query(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY created_at`)
}

// ListEnabled returns only accounts that are enabled.
func (r *AccountRepo) ListEnabled(ctx context.Context) ([]domain.Account, error) {
	return r.query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE enabled = 1 ORDER BY created_at`)
}

// Get retrieves a single account by id, email, or name.
// Returns nil, nil if no account matches.
func (r *AccountRepo) Get(ctx context.Context, ref string) (*domain.Account, error) {
	const q = `SELECT ` + accountColumns + `
		FROM accounts WHERE id = ? OR email = ? OR name = ? LIMIT 1`
	a, err := r.scan(r.db.QueryRowContext(ctx, q, ref, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Upsert inserts or updates an account, keyed by id.
func (r *AccountRepo) Upsert(ctx context.Context, a domain.Account) error {
	if a.AuthType == "" {
		a.AuthType = domain.AuthPassword
	}
	const q = `
		INSERT INTO accounts
			(id, name, email, imap_host, imap_port, imap_username,
			 imap_password, tls, poll_interval, auth_type, enabled,
			 oauth_refresh_token, oauth_access_token, oauth_token_expiry)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name          = excluded.name,
			email         = excluded.email,
			imap_host     = excluded.imap_host,
			imap_port     = excluded.imap_port,
			imap_username = excluded.imap_username,
			imap_password = excluded.imap_password,
			tls           = excluded.tls,
			poll_interval = excluded.poll_interval,
			auth_type     = excluded.auth_type,
			enabled       = excluded.enabled,
			oauth_refresh_token = excluded.oauth_refresh_token,
			oauth_access_token  = excluded.oauth_access_token,
			oauth_token_expiry  = excluded.oauth_token_expiry
	`
	_, err := r.db.ExecContext(ctx, q,
		a.ID, a.Name, a.Email, a.Host, a.Port, a.Username,
		a.Password, boolToInt(a.TLS), a.PollInterval.String(), a.AuthType, boolToInt(a.Enabled),
		a.OAuthRefreshToken, a.OAuthAccessToken, nullableTime(a.OAuthTokenExpiry),
	)
	if err != nil {
		return fmt.Errorf("upsert account: %w", err)
	}
	return nil
}

// UpdateTokens writes only the OAuth token columns for an account. Used by the
// refreshing token source so a token refresh does not rewrite the whole row.
func (r *AccountRepo) UpdateTokens(
	ctx context.Context, id, accessToken, refreshToken string, expiry time.Time,
) error {
	const q = `UPDATE accounts SET
		oauth_access_token  = ?,
		oauth_refresh_token = ?,
		oauth_token_expiry  = ?
		WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, q, accessToken, refreshToken, nullableTime(expiry), id); err != nil {
		return fmt.Errorf("update account tokens: %w", err)
	}
	return nil
}

// Delete removes the account with the given id.
func (r *AccountRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

// PurgeData removes an account's emails and sync_state rows. It does not remove
// the account itself — call Delete for that.
func (r *AccountRepo) PurgeData(ctx context.Context, accountID string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM emails WHERE account_id = ?`, accountID); err != nil {
		return fmt.Errorf("purge account emails: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sync_state WHERE account_id = ?`, accountID); err != nil {
		return fmt.Errorf("purge account sync_state: %w", err)
	}
	return nil
}

// SetEnabled toggles whether an account is polled.
func (r *AccountRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE accounts SET enabled = ? WHERE id = ?`, boolToInt(enabled), id); err != nil {
		return fmt.Errorf("set account enabled: %w", err)
	}
	return nil
}

func (r *AccountRepo) query(ctx context.Context, q string, args ...any) ([]domain.Account, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var accounts []domain.Account
	for rows.Next() {
		a, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accounts: %w", err)
	}
	return accounts, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *AccountRepo) scan(s rowScanner) (*domain.Account, error) {
	var (
		a            domain.Account
		tls, enabled int
		pollStr      string
		expiry       sql.NullString
	)
	if err := s.Scan(
		&a.ID, &a.Name, &a.Email, &a.Host, &a.Port, &a.Username,
		&a.Password, &tls, &pollStr, &a.AuthType, &enabled,
		&a.OAuthRefreshToken, &a.OAuthAccessToken, &expiry,
	); err != nil {
		return nil, err
	}

	d, err := time.ParseDuration(pollStr)
	if err != nil {
		return nil, fmt.Errorf("parse poll_interval %q for account %q: %w", pollStr, a.ID, err)
	}
	a.PollInterval = d
	a.TLS = tls != 0
	a.Enabled = enabled != 0
	if expiry.Valid && expiry.String != "" {
		if a.OAuthTokenExpiry, err = time.Parse(time.RFC3339, expiry.String); err != nil {
			return nil, fmt.Errorf("parse oauth_token_expiry %q for account %q: %w", expiry.String, a.ID, err)
		}
	}
	if a.AuthType == "" {
		a.AuthType = domain.AuthPassword
	}
	return &a, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullableTime returns nil for the zero time (stored as NULL) or an RFC3339
// string otherwise, matching how other timestamps are persisted.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

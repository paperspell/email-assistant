package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
)

// DigestRepo persists sent digests and their numbered items.
type DigestRepo struct {
	db *sql.DB
}

// NewDigestRepo creates a DigestRepo backed by db.
func NewDigestRepo(db *sql.DB) *DigestRepo {
	return &DigestRepo{db: db}
}

// Save inserts a digest, the Telegram messages it was sent as, and its items in
// one transaction. d.TGMessageIDs must list every part in order; when it is
// empty, d.TGMessageID is registered as the single part.
func (r *DigestRepo) Save(ctx context.Context, d domain.Digest, items []domain.DigestItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin digest tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO digests (id, account_id, digest_date, tg_message_id, tg_chat_id, sent_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.AccountID, d.Date, d.TGMessageID, d.TGChatID, d.SentAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert digest: %w", err)
	}
	parts := d.TGMessageIDs
	if len(parts) == 0 && d.TGMessageID != 0 {
		parts = []int64{d.TGMessageID}
	}
	for i, msgID := range parts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO digest_messages (digest_id, part_no, tg_message_id, tg_chat_id)
			VALUES (?, ?, ?, ?)`,
			d.ID, i+1, msgID, d.TGChatID,
		); err != nil {
			return fmt.Errorf("insert digest message: %w", err)
		}
	}
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO digest_items (digest_id, seq_no, email_id, promoted)
			VALUES (?, ?, ?, ?)`,
			d.ID, it.SeqNo, it.EmailID, boolToInt(it.Promoted),
		); err != nil {
			return fmt.Errorf("insert digest item: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit digest: %w", err)
	}
	return nil
}

// GetByTGMessage returns the digest any of whose parts was sent as the given
// message in the given chat, or nil. A reply to the first part of a split
// digest must resolve as well as a reply to the part with the buttons.
//
// Message ids are unique only within a chat, so the chat is part of the key.
// Digests recorded before accounts had their own chats carry chat 0 and match
// any chat; the caller's ownership check keeps such a match from acting on an
// account the replying chat does not own.
func (r *DigestRepo) GetByTGMessage(ctx context.Context, chatID, msgID int64) (*domain.Digest, error) {
	return r.get(ctx, `
		SELECT d.id, d.account_id, d.digest_date, d.tg_message_id, d.tg_chat_id, d.sent_at
		FROM digests d
		JOIN digest_messages m ON m.digest_id = d.id
		WHERE m.tg_message_id = ? AND (m.tg_chat_id = ? OR m.tg_chat_id = 0)`, msgID, chatID)
}

// GetByTGMessageID resolves a message id without a chat: only digests recorded
// before accounts had their own chats. Kept for callers that predate the
// chat-keyed lookup.
func (r *DigestRepo) GetByTGMessageID(ctx context.Context, msgID int64) (*domain.Digest, error) {
	return r.GetByTGMessage(ctx, 0, msgID)
}

// GetByID returns a digest by its id, or nil. Bulk actions carry the digest id
// in their callback data and must be checked against the account it belongs to.
func (r *DigestRepo) GetByID(ctx context.Context, id string) (*domain.Digest, error) {
	return r.get(ctx,
		`SELECT id, account_id, digest_date, tg_message_id, tg_chat_id, sent_at FROM digests WHERE id = ?`, id)
}

// GetByAccountAndDate returns the digest for an account on a date, or nil.
func (r *DigestRepo) GetByAccountAndDate(ctx context.Context, accountID, date string) (*domain.Digest, error) {
	return r.get(ctx,
		`SELECT id, account_id, digest_date, tg_message_id, tg_chat_id, sent_at
		 FROM digests WHERE account_id = ? AND digest_date = ?`, accountID, date)
}

func (r *DigestRepo) get(ctx context.Context, q string, args ...any) (*domain.Digest, error) {
	var (
		d      domain.Digest
		sentAt string
	)
	err := r.db.QueryRowContext(ctx, q, args...).Scan(&d.ID, &d.AccountID, &d.Date, &d.TGMessageID, &d.TGChatID, &sentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get digest: %w", err)
	}
	if t, perr := time.Parse(time.RFC3339, sentAt); perr == nil {
		d.SentAt = t
	}
	return &d, nil
}

// Items returns all items for a digest ordered by seq_no.
func (r *DigestRepo) Items(ctx context.Context, digestID string) ([]domain.DigestItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT digest_id, seq_no, email_id, promoted FROM digest_items
		 WHERE digest_id = ? ORDER BY seq_no`, digestID)
	if err != nil {
		return nil, fmt.Errorf("query digest items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []domain.DigestItem
	for rows.Next() {
		var (
			it       domain.DigestItem
			promoted int
		)
		if err := rows.Scan(&it.DigestID, &it.SeqNo, &it.EmailID, &promoted); err != nil {
			return nil, fmt.Errorf("scan digest item: %w", err)
		}
		it.Promoted = promoted != 0
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest items: %w", err)
	}
	return items, nil
}

// MarkPromoted flags a digest item (by seq_no) as promoted to important.
func (r *DigestRepo) MarkPromoted(ctx context.Context, digestID string, seqNo int) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE digest_items SET promoted = 1 WHERE digest_id = ? AND seq_no = ?`,
		digestID, seqNo); err != nil {
		return fmt.Errorf("mark digest item promoted: %w", err)
	}
	return nil
}

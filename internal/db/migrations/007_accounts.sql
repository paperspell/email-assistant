-- +goose Up

CREATE TABLE IF NOT EXISTS accounts (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL,
    imap_host     TEXT NOT NULL,
    imap_port     INTEGER NOT NULL DEFAULT 993,
    imap_username TEXT NOT NULL DEFAULT '',
    imap_password TEXT NOT NULL DEFAULT '',
    tls           INTEGER NOT NULL DEFAULT 1,
    poll_interval TEXT NOT NULL DEFAULT '1m',
    auth_type     TEXT NOT NULL DEFAULT 'password',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Backfill from the Stage 1-6 single-account settings, if present.
-- account_id elsewhere already equals the account email, so using the email as
-- the new accounts.id keeps existing emails/sync_state rows matching with no
-- data rewrite. auth_type/enabled fall back to their column defaults.
INSERT OR IGNORE INTO accounts
    (id, name, email, imap_host, imap_port, imap_username, imap_password, tls, poll_interval)
SELECT
    (SELECT value FROM settings WHERE key = 'account.email'),
    COALESCE((SELECT value FROM settings WHERE key = 'account.name'), ''),
    (SELECT value FROM settings WHERE key = 'account.email'),
    (SELECT value FROM settings WHERE key = 'account.imap.host'),
    COALESCE((SELECT value FROM settings WHERE key = 'account.imap.port'), '993'),
    COALESCE((SELECT value FROM settings WHERE key = 'account.imap.username'), ''),
    COALESCE((SELECT value FROM settings WHERE key = 'account.imap.password'), ''),
    CASE WHEN (SELECT value FROM settings WHERE key = 'account.imap.tls') = 'false' THEN 0 ELSE 1 END,
    COALESCE((SELECT value FROM settings WHERE key = 'account.poll_interval'), '1m')
WHERE EXISTS (SELECT 1 FROM settings WHERE key = 'account.email');

-- +goose Down

DROP TABLE IF EXISTS accounts;

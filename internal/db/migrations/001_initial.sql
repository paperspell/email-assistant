-- +goose Up

CREATE TABLE IF NOT EXISTS emails (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    message_uid INTEGER NOT NULL,
    subject     TEXT NOT NULL DEFAULT '',
    from_email  TEXT NOT NULL DEFAULT '',
    from_name   TEXT NOT NULL DEFAULT '',
    date        DATETIME NOT NULL,
    status      TEXT NOT NULL DEFAULT 'new',
    received_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_emails_account_uid
    ON emails (account_id, message_uid);

CREATE TABLE IF NOT EXISTS sync_state (
    account_id TEXT PRIMARY KEY,
    last_uid   INTEGER NOT NULL DEFAULT 0,
    synced_at  DATETIME NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS sync_state;
DROP INDEX IF EXISTS idx_emails_account_uid;
DROP TABLE IF EXISTS emails;

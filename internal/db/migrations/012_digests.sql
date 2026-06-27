-- +goose Up

CREATE TABLE digests (
    id            TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL,
    digest_date   TEXT NOT NULL,            -- YYYY-MM-DD in the account's timezone
    tg_message_id INTEGER NOT NULL DEFAULT 0,
    sent_at       TEXT NOT NULL,
    UNIQUE (account_id, digest_date)
);

CREATE TABLE digest_items (
    digest_id TEXT NOT NULL,
    seq_no    INTEGER NOT NULL,             -- 1-based number shown to the user
    email_id  TEXT NOT NULL,
    promoted  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (digest_id, seq_no)
);
CREATE INDEX idx_digest_items_email ON digest_items(email_id);

ALTER TABLE accounts ADD COLUMN digest_time TEXT NOT NULL DEFAULT '';  -- '' = use global digest.time

-- +goose Down
DROP TABLE IF EXISTS digest_items;
DROP TABLE IF EXISTS digests;
-- accounts.digest_time left in place (SQLite cannot drop columns pre-3.35).

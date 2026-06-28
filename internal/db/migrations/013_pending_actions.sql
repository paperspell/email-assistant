-- +goose Up

-- Free-text input the bot is waiting for, keyed by chat. One pending action per
-- chat; a new menu choice overwrites it.
CREATE TABLE pending_actions (
    chat_id    INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL,           -- clause | subject_edit | subject_confirm
    email_id   TEXT NOT NULL,
    account_id TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '', -- suggested subject pattern (subject_confirm)
    created_at TEXT NOT NULL
);

-- Persist the List-Id header so the ignore menu can offer a list_id rule and the
-- created rule has a value to match on.
ALTER TABLE emails ADD COLUMN list_id TEXT NOT NULL DEFAULT '';

-- +goose Down
DROP TABLE IF EXISTS pending_actions;
-- emails.list_id left in place (SQLite cannot drop columns pre-3.35).

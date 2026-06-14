-- +goose Up

ALTER TABLE emails ADD COLUMN telegram_message_id INTEGER NOT NULL DEFAULT 0;

-- +goose Down

-- SQLite does not support DROP COLUMN; this migration is irreversible.

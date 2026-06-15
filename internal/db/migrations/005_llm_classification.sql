-- +goose Up

ALTER TABLE classifications ADD COLUMN source  TEXT NOT NULL DEFAULT 'rule_based';
ALTER TABLE classifications ADD COLUMN summary TEXT NOT NULL DEFAULT '';

-- +goose Down

-- SQLite does not support DROP COLUMN; this migration is irreversible.

-- +goose Up

ALTER TABLE emails ADD COLUMN language TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS classifications (
    id            TEXT PRIMARY KEY,
    email_id      TEXT NOT NULL REFERENCES emails(id),
    level         TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'other',
    score         INTEGER NOT NULL DEFAULT 0,
    reason        TEXT NOT NULL DEFAULT '[]',
    classified_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS senders (
    id               TEXT PRIMARY KEY,
    email            TEXT NOT NULL UNIQUE,
    importance_score INTEGER NOT NULL DEFAULT 0,
    seen_count       INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
    id               TEXT PRIMARY KEY,
    domain           TEXT NOT NULL UNIQUE,
    importance_score INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS classifications;
DROP TABLE IF EXISTS senders;
DROP TABLE IF EXISTS domains;

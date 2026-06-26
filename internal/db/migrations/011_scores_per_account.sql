-- +goose Up

-- Sender/domain learning becomes per-account. Per the Stage 9 decision we start
-- from scratch: the old global scores are coarse and re-learn quickly from the
-- next feedback click, so the previous rows are dropped rather than replicated.
DROP TABLE IF EXISTS senders;
DROP TABLE IF EXISTS domains;

CREATE TABLE senders (
    id               TEXT PRIMARY KEY,
    account_id       TEXT NOT NULL,
    email            TEXT NOT NULL,
    importance_score INTEGER NOT NULL DEFAULT 0,
    seen_count       INTEGER NOT NULL DEFAULT 0,
    updated_at       TEXT NOT NULL,
    UNIQUE (account_id, email)
);

CREATE TABLE domains (
    id               TEXT PRIMARY KEY,
    account_id       TEXT NOT NULL,
    domain           TEXT NOT NULL,
    importance_score INTEGER NOT NULL DEFAULT 0,
    updated_at       TEXT NOT NULL,
    UNIQUE (account_id, domain)
);

-- +goose Down

-- Forward-only: revert to the global single-table shape from migration 003.
DROP TABLE IF EXISTS senders;
DROP TABLE IF EXISTS domains;
CREATE TABLE senders (
    id               TEXT PRIMARY KEY,
    email            TEXT NOT NULL UNIQUE,
    importance_score INTEGER NOT NULL DEFAULT 0,
    seen_count       INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL
);
CREATE TABLE domains (
    id               TEXT PRIMARY KEY,
    domain           TEXT NOT NULL UNIQUE,
    importance_score INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL
);

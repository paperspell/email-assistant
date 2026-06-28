-- +goose Up
-- Consolidated baseline schema (squash of the original 001–013 migrations).
-- Representation only: same tables, columns, defaults, indexes, and constraints
-- the repositories work against. Timestamp-affinity columns the repos write as
-- RFC3339 text are kept as-is; default-data seeding lives in the Go account path.

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE accounts (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL DEFAULT '',
    email               TEXT NOT NULL,
    imap_host           TEXT NOT NULL,
    imap_port           INTEGER NOT NULL DEFAULT 993,
    imap_username       TEXT NOT NULL DEFAULT '',
    imap_password       TEXT NOT NULL DEFAULT '',
    tls                 INTEGER NOT NULL DEFAULT 1,
    poll_interval       TEXT NOT NULL DEFAULT '10m',
    auth_type           TEXT NOT NULL DEFAULT 'password',
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    oauth_refresh_token TEXT NOT NULL DEFAULT '',
    oauth_access_token  TEXT NOT NULL DEFAULT '',
    oauth_token_expiry  DATETIME,
    digest_time         TEXT NOT NULL DEFAULT ''   -- '' = use global digest.time
);

CREATE TABLE emails (
    id                  TEXT PRIMARY KEY,
    account_id          TEXT NOT NULL,
    message_uid         INTEGER NOT NULL,
    subject             TEXT NOT NULL DEFAULT '',
    from_email          TEXT NOT NULL DEFAULT '',
    from_name           TEXT NOT NULL DEFAULT '',
    date                DATETIME NOT NULL,
    status              TEXT NOT NULL DEFAULT 'new',
    received_at         DATETIME NOT NULL,
    language            TEXT NOT NULL DEFAULT '',
    telegram_message_id INTEGER NOT NULL DEFAULT 0,
    decided_by          TEXT NOT NULL DEFAULT '',   -- rule:<id> | baseline | llm:low
    list_id             TEXT NOT NULL DEFAULT ''    -- List-Id header
);
CREATE UNIQUE INDEX idx_emails_account_uid ON emails (account_id, message_uid);

CREATE TABLE sync_state (
    account_id TEXT PRIMARY KEY,
    last_uid   INTEGER NOT NULL DEFAULT 0,
    synced_at  DATETIME NOT NULL
);

CREATE TABLE classifications (
    id            TEXT PRIMARY KEY,
    email_id      TEXT NOT NULL REFERENCES emails(id),
    level         TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'other',
    score         INTEGER NOT NULL DEFAULT 0,
    reason        TEXT NOT NULL DEFAULT '[]',
    classified_at DATETIME NOT NULL,
    source        TEXT NOT NULL DEFAULT 'rule_based',
    summary       TEXT NOT NULL DEFAULT ''
);

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

CREATE TABLE llm_audit_log (
    id           TEXT PRIMARY KEY,
    email_id     TEXT NOT NULL REFERENCES emails(id),
    provider     TEXT NOT NULL,
    model        TEXT NOT NULL,
    content_mode TEXT NOT NULL,
    bytes_sent   INTEGER NOT NULL,
    created_at   DATETIME NOT NULL
);

CREATE TABLE filter_rules (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    action      TEXT NOT NULL DEFAULT 'ignore',   -- ignore | allow
    type        TEXT NOT NULL,                     -- sender | domain | list_id | subject
    matcher     TEXT NOT NULL DEFAULT 'exact',     -- exact | substring
    value       TEXT NOT NULL,
    scope_kind  TEXT NOT NULL DEFAULT '',          -- subject: 'sender'
    scope_value TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    source      TEXT NOT NULL DEFAULT 'user',      -- user | default
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_filter_rules_account ON filter_rules(account_id, enabled);

CREATE TABLE llm_clauses (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    text        TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    source      TEXT NOT NULL DEFAULT 'user',
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_llm_clauses_account ON llm_clauses(account_id, enabled);

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

CREATE TABLE pending_actions (
    chat_id    INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL,            -- clause | subject_edit | subject_confirm
    email_id   TEXT NOT NULL,
    account_id TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '', -- suggested subject pattern (subject_confirm)
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS pending_actions;
DROP TABLE IF EXISTS digest_items;
DROP TABLE IF EXISTS digests;
DROP TABLE IF EXISTS llm_clauses;
DROP TABLE IF EXISTS filter_rules;
DROP TABLE IF EXISTS llm_audit_log;
DROP TABLE IF EXISTS domains;
DROP TABLE IF EXISTS senders;
DROP TABLE IF EXISTS classifications;
DROP TABLE IF EXISTS sync_state;
DROP TABLE IF EXISTS emails;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS settings;

-- +goose Up

CREATE TABLE IF NOT EXISTS llm_audit_log (
    id           TEXT PRIMARY KEY,
    email_id     TEXT NOT NULL REFERENCES emails(id),
    provider     TEXT NOT NULL,
    model        TEXT NOT NULL,
    content_mode TEXT NOT NULL,
    bytes_sent   INTEGER NOT NULL,
    created_at   DATETIME NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS llm_audit_log;

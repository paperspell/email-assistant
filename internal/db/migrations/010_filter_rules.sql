-- +goose Up

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

ALTER TABLE emails ADD COLUMN decided_by TEXT NOT NULL DEFAULT '';

-- Seed the default ignore clauses (Set A) for every existing account, but only
-- when an LLM provider is configured (clauses do nothing without the LLM layer).
-- New accounts are seeded in Go at `account add`.
INSERT INTO llm_clauses (id, account_id, text, enabled, source, created_at)
SELECT lower(hex(randomblob(16))), a.id, c.text, 1, 'default',
       strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM accounts a
CROSS JOIN (
    SELECT 'Ignore promotional and marketing emails (sales, discounts, product announcements, webinars) unless tied to an active order, payment, shipping, or account security.' AS text
    UNION ALL
    SELECT 'Ignore automated social-media notifications: likes, reactions, new followers, "people you may know", comment digests.'
    UNION ALL
    SELECT 'Ignore periodic newsletter digests unless from a sender previously marked important.'
) c
WHERE EXISTS (SELECT 1 FROM settings WHERE key = 'llm.provider' AND value <> '');

-- +goose Down

DROP TABLE IF EXISTS filter_rules;
DROP TABLE IF EXISTS llm_clauses;
-- emails.decided_by left in place (SQLite cannot drop columns pre-3.35).

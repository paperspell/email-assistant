# db-schema.md

> **Keep this file up to date.** Update the ERD whenever a new migration is added.

Current schema reflects migrations up to: `012_digests.sql`

```mermaid
erDiagram
    emails {
        TEXT id PK "ULID"
        TEXT account_id "email address"
        INTEGER message_uid "IMAP UID"
        TEXT subject
        TEXT from_email
        TEXT from_name
        DATETIME date
        TEXT status "new | notified | ignored | handled | reply_needed"
        DATETIME received_at
        TEXT language "ISO 639-1 or empty"
        INTEGER telegram_message_id "0 until notification sent"
        TEXT decided_by "rule:<id> | baseline | llm:low; empty when notified/allowed"
    }

    sync_state {
        TEXT account_id PK "email address"
        INTEGER last_uid "last processed IMAP UID"
        DATETIME synced_at
    }

    settings {
        TEXT key PK "dot-notation key e.g. telegram.bot_token"
        TEXT value
        DATETIME updated_at
    }

    accounts {
        TEXT id PK "email address (= account_id elsewhere)"
        TEXT name "display label"
        TEXT email
        TEXT imap_host
        INTEGER imap_port
        TEXT imap_username
        TEXT imap_password "DB encrypted at rest"
        INTEGER tls "1 = use TLS"
        TEXT poll_interval "Go duration string e.g. 10m; default seeded from poll.default_interval"
        TEXT auth_type "password | oauth"
        INTEGER enabled "1 = polled"
        DATETIME created_at
        TEXT oauth_refresh_token "oauth accounts only"
        TEXT oauth_access_token "refreshable cache"
        DATETIME oauth_token_expiry "nullable"
        TEXT digest_time "HH:MM override; empty = global digest.time"
    }

    classifications {
        TEXT id PK "ULID"
        TEXT email_id FK "references emails.id"
        TEXT level "critical | important | maybe | ignore"
        TEXT category "work | finance | legal | ..."
        INTEGER score "0–100"
        TEXT reason "JSON array of reason strings"
        DATETIME classified_at
        TEXT source "rule_based | llm:anthropic | llm:openai"
        TEXT summary "LLM-generated summary; empty for rule_based"
    }

    senders {
        TEXT id PK "ULID"
        TEXT account_id "per-account; UNIQUE(account_id, email)"
        TEXT email
        INTEGER importance_score
        INTEGER seen_count
        DATETIME updated_at
    }

    domains {
        TEXT id PK "ULID"
        TEXT account_id "per-account; UNIQUE(account_id, domain)"
        TEXT domain
        INTEGER importance_score
        DATETIME updated_at
    }

    filter_rules {
        TEXT id PK "ULID"
        TEXT account_id "per-account"
        TEXT action "ignore | allow"
        TEXT type "sender | domain | list_id | subject"
        TEXT matcher "exact | substring"
        TEXT value
        TEXT scope_kind "subject: 'sender'"
        TEXT scope_value
        INTEGER enabled "1 = active"
        TEXT source "user | default"
        TEXT created_at
    }

    llm_clauses {
        TEXT id PK "ULID"
        TEXT account_id "per-account"
        TEXT text "natural-language ignore instruction"
        INTEGER enabled "1 = injected into prompt"
        TEXT source "user | default"
        TEXT created_at
    }

    digests {
        TEXT id PK "ULID"
        TEXT account_id "per-account; UNIQUE(account_id, digest_date)"
        TEXT digest_date "YYYY-MM-DD (account tz)"
        INTEGER tg_message_id "maps replies/buttons back"
        TEXT sent_at
    }

    digest_items {
        TEXT digest_id PK "FK digests.id"
        INTEGER seq_no PK "1-based number shown"
        TEXT email_id "FK emails.id"
        INTEGER promoted "1 once kept via /important"
    }

    goose_db_version {
        INTEGER id PK
        INTEGER version_id
        INTEGER is_applied
        DATETIME tstamp
    }

    llm_audit_log {
        TEXT id PK "ULID"
        TEXT email_id FK "references emails.id"
        TEXT provider "anthropic | openai"
        TEXT model "model name used"
        TEXT content_mode "headers_only | redacted_body | full_body"
        INTEGER bytes_sent "size of user message sent to LLM"
        DATETIME created_at
    }

    emails }o--|| sync_state : "account_id"
    emails }o--|| accounts : "account_id"
    sync_state ||--|| accounts : "account_id"
    classifications ||--|| emails : "email_id"
    llm_audit_log }o--|| emails : "email_id"
    filter_rules }o--|| accounts : "account_id"
    llm_clauses }o--|| accounts : "account_id"
    senders }o--|| accounts : "account_id"
    domains }o--|| accounts : "account_id"
    digests }o--|| accounts : "account_id"
    digest_items }o--|| digests : "digest_id"
    digest_items }o--|| emails : "email_id"
```

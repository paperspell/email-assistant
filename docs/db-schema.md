# db-schema.md

> **Keep this file up to date.** Update the ERD whenever a new migration is added.

Current schema reflects migrations up to: `003_classifications.sql`

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
        TEXT status "new | notified | ignored"
        DATETIME received_at
        TEXT language "ISO 639-1 or empty"
    }

    sync_state {
        TEXT account_id PK "email address"
        INTEGER last_uid "last processed IMAP UID"
        DATETIME synced_at
    }

    settings {
        TEXT key PK "dot-notation key e.g. account.imap.host"
        TEXT value
        DATETIME updated_at
    }

    classifications {
        TEXT id PK "ULID"
        TEXT email_id FK "references emails.id"
        TEXT level "critical | important | maybe | ignore"
        TEXT category "work | finance | legal | ..."
        INTEGER score "0–100"
        TEXT reason "JSON array of reason strings"
        DATETIME classified_at
    }

    senders {
        TEXT id PK "ULID"
        TEXT email "UNIQUE"
        INTEGER importance_score
        INTEGER seen_count
        DATETIME updated_at
    }

    domains {
        TEXT id PK "ULID"
        TEXT domain "UNIQUE"
        INTEGER importance_score
        DATETIME updated_at
    }

    goose_db_version {
        INTEGER id PK
        INTEGER version_id
        INTEGER is_applied
        DATETIME tstamp
    }

    emails }o--|| sync_state : "account_id"
    classifications ||--|| emails : "email_id"
```

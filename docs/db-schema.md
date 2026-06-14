# db-schema.md

> **Keep this file up to date.** Update the ERD whenever a new migration is added.

Current schema reflects migrations up to: `001_initial.sql`

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
    }

    sync_state {
        TEXT account_id PK "email address"
        INTEGER last_uid "last processed IMAP UID"
        DATETIME synced_at
    }

    goose_db_version {
        INTEGER id PK
        INTEGER version_id
        INTEGER is_applied
        DATETIME tstamp
    }

    emails }o--|| sync_state : "account_id"
```

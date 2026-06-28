# 009-10-squash-migrations.md

Status: Implemented
Version: 0.1

> Implementation notes:
> - `001_initial.sql` was authored from a live `.schema` dump of a freshly-migrated
>   DB, then folded inline (no trailing-`ALTER` columns) — fidelity preserved.
> - `accounts.poll_interval` default is set to `10m` in the baseline (the old
>   column default was a cosmetic `1m`; the app always supplies the value).
> - The init-ordering gap is closed by `reconcileDefaultClauses`, run at the end of
>   every `init <section>` (notably `init llm`): it seeds Set A clauses for any
>   account that has none, once an LLM provider is configured. This replaces the
>   old migration-010 backfill.
> - The version-based `migration_backfill_test.go` was replaced by
>   `migrate_test.go` (`TestMigrate_FreshSchema` / `TestMigrate_Idempotent`).

# Stage 009-10 — Squash Migrations to a Single Baseline

## Goal

Collapse migrations `001`–`013` into one consolidated `001_initial.sql` that *is*
the current schema, and move all default-data seeding out of SQL into the Go
account-creation path. Keep goose; future stages continue from `002`.

There is no production data — only a disposable dev database — so the transitional
backfills (legacy single-account import, per-account score recreation, the
default-clause `CROSS JOIN`) carry nothing and become pure noise once squashed.

---

## What Changes

| Before | After |
|--------|-------|
| 13 incremental migrations (`001`–`013`) with ALTERs + data backfills | One `001_initial.sql` describing the final schema |
| Default ignore clauses seeded partly in SQL (migration 010) and partly in Go | All seeding in the Go `account add` / `init` path |
| `migration_backfill_test.go` asserts intermediate states by version number | Replaced by a single fresh-migrate smoke test + existing repo tests |
| Dev DB carries goose history `1..13` | Dev DB wiped and re-initialised at version `1` |

No schema *content* changes — same tables, columns, indexes, constraints, and
defaults. This is a representation/cleanup change only.

---

## Directory Structure Changes

```
internal/db/migrations/
  001_initial.sql          # REWRITTEN: full final schema (consolidated)
  002_settings.sql         # DELETE
  003_classifications.sql  # DELETE
  …                        # DELETE 004–013
internal/db/
  migration_backfill_test.go  # REWRITTEN/trimmed (version-specific tests removed)
cmd/email-agent/
  cmd_account.go / cmd_init.go  # ensure all seeding lives here (no SQL backfill)
docs/
  db-schema.md             # pointer → 001_initial.sql; ERD already matches
  general-implementation-plan.md  # note the squash under Stage 9
```

---

## Tasks

### T1 — Author the consolidated `001_initial.sql`

Derive it faithfully rather than by hand, so nothing is lost:

1. On a scratch DB, run the *current* migrations (`db.Migrate`) to materialise the
   final schema.
2. Dump it: `sqlite3 scratch.db '.schema'`.
3. Strip the `goose_db_version` table (goose manages it) and any
   migration-bookkeeping; reformat into a single `-- +goose Up` block with a
   matching `-- +goose Down` that drops every table.

The consolidated schema must contain (cross-check against `docs/db-schema.md`):

- **settings**(key PK, value, updated_at)
- **emails**(id PK, account_id, message_uid, subject, from_email, from_name, date,
  status, received_at, language, telegram_message_id, **decided_by**, **list_id**;
  `UNIQUE(account_id, message_uid)`)
- **sync_state**(account_id PK, last_uid, synced_at)
- **classifications**(id PK, email_id, level, category, score, reason, classified_at,
  source, summary)
- **senders**(id PK, account_id, email, importance_score, seen_count, updated_at;
  `UNIQUE(account_id, email)`)
- **domains**(id PK, account_id, domain, importance_score, updated_at;
  `UNIQUE(account_id, domain)`)
- **accounts**(id PK, name, email, imap_host, imap_port, imap_username,
  imap_password, tls, **poll_interval DEFAULT '10m'**, auth_type, enabled,
  created_at, oauth_refresh_token, oauth_access_token, oauth_token_expiry,
  **digest_time**)
- **llm_audit_log**(id PK, email_id, provider, model, content_mode, bytes_sent,
  created_at)
- **filter_rules**(id PK, account_id, action, type, matcher, value, scope_kind,
  scope_value, enabled, source, created_at) + `idx_filter_rules_account`
- **llm_clauses**(id PK, account_id, text, enabled, source, created_at) +
  `idx_llm_clauses_account`
- **digests**(id PK, account_id, digest_date, tg_message_id, sent_at;
  `UNIQUE(account_id, digest_date)`)
- **digest_items**(digest_id, seq_no, email_id, promoted; `PK(digest_id, seq_no)`)
  + `idx_digest_items_email`
- **pending_actions**(chat_id PK, kind, email_id, account_id, payload, created_at)

**Watch the timestamp affinities** the repos rely on: `poll_interval`,
`created_at`/`sent_at` on the Stage-9 tables, and the score `updated_at` columns
are written as RFC3339 *text* by the repos. Keep whatever the `.schema` dump
produces (it reflects what the repos already work against) — do not "tidy" column
types into `DATETIME` where a repo writes/reads RFC3339 strings.

Crucially, the consolidated file contains **no `INSERT`/backfill statements** —
no legacy `account.*` import (old 007), no `DROP`+recreate of scores (old 011),
no default-clause `CROSS JOIN` (old 010).

---

### T2 — Delete migrations `002`–`013`

Remove the files. `go:embed migrations/*.sql` picks up whatever remains, so no
code change is needed for embedding.

---

### T3 — Seeding lives only in Go

The default ignore clauses (Set A) and example rules (Set B) are already seeded in
`addOrEditAccount` (`cmd_account.go`). Removing the 010 SQL backfill drops the
*existing-account* path, which no longer exists on a fresh DB. Two adjustments:

- Verify Set A seeding still gates on a configured LLM provider and stays
  idempotent (`ClauseRepo.Count == 0`).
- **Close the init-ordering gap.** `init` creates the first account *before* the
  LLM provider is set, so Set A would never seed for it (previously the 010
  backfill covered this). Fix by seeding clauses at the **end of `init`**, after
  the LLM section, for the account(s) just created — or by reordering init so LLM
  is configured first. (Decision: end-of-init seeding pass; least disruptive.)

---

### T4 — Rewrite the migration tests

`migration_backfill_test.go` is built on version numbers and intermediate states
that disappear:

- **Delete** `TestMigration007_*` (legacy backfill), `TestMigration009_*`
  (poll-default backfill), `TestMigration010_*` (clause backfill),
  `TestMigration011_*` (score drop/recreate), and the `migrateTo`/version
  constants.
- **Keep/replace** with a single `TestMigrate_FreshSchema` that runs `db.Migrate`
  on `:memory:` and asserts the expected tables exist (and a couple of key
  columns/indices), proving the consolidated file is valid and complete.
- The repo round-trip tests and the `digest`/`filter`/`telegram` suites are
  unaffected — they all run the full `db.Migrate` and exercise the final schema.

---

### T5 — Reset the dev database

Not code — a runbook step (and optional `make` target):

```
rm -f ~/.email-agent/email-agent.db   # or the EMAIL_AGENT_DB path
email-agent init                      # re-creates at version 1
```

Document this in the stage doc / README note so the squash is reproducible.

---

### T6 — Docs

- `docs/db-schema.md`: change "reflects migrations up to" → `001_initial.sql`; the
  ERD already matches the final schema (sanity-check it against T1's output).
- `docs/general-implementation-plan.md`: add a one-line bullet under Stage 9 that
  the migration history was squashed to a single baseline.

---

### T7 — Verify

`make check` (lint + unit + migration tests + race). Then a manual
`rm db && email-agent init && email-agent account add` smoke test to confirm a
fresh install seeds clauses/examples correctly with no migration errors.

---

## Recommended Task Order

```
T1 author 001_initial → T2 delete 002–013 → T3 Go-only seeding (+init gap)
→ T4 rewrite migration tests → T5 reset dev DB → T6 docs → T7 make check
```

---

## Definition of Done

1. `internal/db/migrations/` contains exactly one file, `001_initial.sql`.
2. `make check` passes; `db.Migrate` on a fresh DB yields the full schema.
3. No `INSERT`/backfill SQL in migrations; all seeding happens in Go, including the
   first account created during `init` (when an LLM provider is configured).
4. A wiped dev DB re-initialises cleanly via `email-agent init`.
5. `docs/db-schema.md` points at `001_initial.sql` and matches the schema.

---

## Out of Scope

- Any schema content change (new/renamed columns, constraints) — representation only.
- Removing goose or changing the migration tooling.
- Re-squashing after Stages 10–13 (a future, separate cleanup if desired).

---

## Notes / Risks

- **goose bookkeeping:** a fresh DB applies the single migration as version 1;
  there is no in-place reconciliation for an already-migrated DB (acceptable — the
  only such DB is the disposable dev one, which T5 wipes).
- **Fidelity:** authoring T1 from a live `.schema` dump (not by hand) is the
  safeguard against silently dropping a column default, index, or `UNIQUE`.
- **Re-accumulation:** Stages 10–13 will add `002`, `003`, … again. That's fine;
  this squash is about clearing the pre-release transitional cruft now.

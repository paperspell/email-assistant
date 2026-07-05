# 008-06-first-run-backfill.md

Status: Implemented
Version: 0.1

# Stage 008-06 — First-Run Backfill of Recent Unread Mail

## Goal

On the first poll of a new account, in addition to setting the baseline
(008-05), optionally process **unread mail received within a recent window** so
the user isn't blind to what's already sitting unread. Important messages notify
immediately; unimportant ones fall through to the digest.

Opt-in and per-account: default is **off** (`0`), so first run stays silent
unless the user enables a window.

## Behaviour

On the first run (`state == nil`), after computing the cheap baseline:

1. If the account's `BackfillWindow` is `0`, do nothing (current silent behaviour).
2. Otherwise `UID SEARCH UNSEEN SINCE <now - window>` (window clamped to **7
   days**), take the newest **200** UIDs (cap), fetch them (envelope + body) and
   run each through the normal `processMessage` pipeline.
3. Then record the baseline.

Routing is the existing pipeline's, so no new decision logic:

- **important → notify** immediately (Telegram),
- **unimportant → ignored/recorded → digest** (picked up by the digest scheduler).

Read state is **not** changed — `BODY.PEEK` means the mail stays unread in Gmail.

### Bounds & safety

| Concern | Handling |
|---------|----------|
| Window | Per-account; clamped to `maxBackfillWindow = 7d` in both CLI and scheduler |
| Volume | `maxBackfillMessages = 200` newest unread (cap on LLM cost / notification burst) |
| Restart mid-backfill | Baseline is written **after** the backfill; each message is skipped if already ingested (`EmailRepo.GetByAccountAndUID`), so a restart resumes without re-notifying |
| Ordering vs baseline | Backfilled UIDs are ≤ baseline, so later polls never reprocess them |

## Changes

| Piece | Location |
|-------|----------|
| Per-account column | `internal/db/migrations/002_backfill_window.sql` — `backfill_window TEXT NOT NULL DEFAULT '0s'` |
| Domain field | `internal/domain/account.go` — `Account.BackfillWindow time.Duration` |
| Persist / scan | `internal/db/repo/account_repo.go` — column, Upsert, scan (parsed like `poll_interval`) |
| Provider method | `internal/email/provider.go` — `FetchUnseenSince(ctx, since, limit)` |
| IMAP impl | `internal/email/imap/client.go` — `FetchUnseenSince` via `UID SEARCH UNSEEN SINCE`; shared `fetchMessages` helper extracted from `FetchSince` |
| Scheduler | `internal/scheduler/scheduler.go` — `Config.BackfillWindow`, `maxBackfillWindow`/`maxBackfillMessages`, `backfillFirstRun` invoked in the first-run branch |
| CLI prompt | `cmd/email-agent/cmd_account.go` — "First-run backfill of unread mail (0 = off, max 168h)" as the last account field; clamps to a week; also now preserves `DigestTime` on edit |
| Daemon wiring | `cmd/email-agent/main.go` — `BackfillWindow: acc.BackfillWindow` |

## Configuration

Set per account with `account add` / `account edit`:

```
First-run backfill of unread mail (0 = off, max 168h) [0s]: 168h
```

Only affects an account's **first** run (no sync state). Changing it later has no
effect unless the account's sync state is reset.

## Tests

`internal/scheduler/scheduler_backfill_test.go`:

- disabled by default → `FetchUnseenSince` not called, baseline still set;
- enabled → unread ingested & (allow-rule) notified, baseline from `LatestUID`,
  cap = 200, `since ≈ now - window`;
- window over a week is clamped to 7 days;
- already-ingested unread is skipped (no re-notify) — restart safety.

Migration test (`-tags=migration`) covers `002_backfill_window.sql`.

## Definition of Done

1. `make check` passes (incl. migration tests, `-race`).
2. With `backfill_window = 0` (default), first run is silent (008-05 behaviour).
3. With a window set, first run notifies important unread from the last N days
   (≤ 7, ≤ 200) and routes the rest to the digest, without marking anything read.
4. A restart during backfill does not duplicate notifications.

## Out of Scope

- Re-running backfill after the first run (it is first-run only).
- Global (non-per-account) default window.
- Backfilling **read** mail or mail older than a week.

# 008-05-fast-first-run.md

Status: Implemented
Version: 0.1

# Stage 008-05 — Fast First Run (baseline without downloading existing mail)

## Goal

On the first poll of a new account, establish the "starting point" UID **without
downloading every existing message** (and, when body access is enabled, every
existing body).

## Problem

On a fresh install there is no sync state, so `poll` treated the first cycle as a
"first run". Its only job is to record the current highest UID as a baseline and
skip processing (so pre-existing mail does not flood notifications). But it got
that UID by calling `FetchSince(0)`, which:

1. runs `UID SEARCH` for all messages,
2. `FETCH`es the envelope of every message, and
3. with `content.mode = redacted_body|full_body` (`fetch_body=true`), downloads
   the **full body of every existing message**,

then computes `max(UID)` and discards everything. On a 12 844-message mailbox
this meant tens of seconds of envelope fetching followed by a full-body download
of all 12 844 messages — pure waste. Worse, the baseline is only persisted
**after** that completes, so killing the daemon mid-first-run repeats it.

## Change

Add a cheap `LatestUID` to the provider and use it on the first run instead of
`FetchSince`.

| Piece | Location |
|-------|----------|
| Interface method | `internal/email/provider.go` — `LatestUID(ctx) (uint32, error)` |
| IMAP impl | `internal/email/imap/client.go` — `Client.LatestUID` via a single `STATUS INBOX (UIDNEXT)`, returns `UIDNEXT-1` (0 for an empty mailbox), wrapped in `exec` for auto-reconnect |
| Scheduler | `internal/scheduler/scheduler.go` — the `state == nil` branch calls `LatestUID` and upserts the baseline; `FetchSince` moved after the first-run check so it never runs on the first cycle |

Semantics preserved:

- An **empty mailbox** (`UIDNEXT <= 1` → baseline 0) still does **not** create a
  sync state, so the account stays in first-run state (matches prior behaviour).
- A non-empty mailbox records `baseline = UIDNEXT-1`; the next poll fetches only
  `UID > baseline` (genuinely new mail).
- First run still sends **no notifications** and makes **no LLM calls** for
  existing mail.

## Tests

`internal/scheduler/scheduler_test.go`:

- `TestScheduler_FirstRun_DoesNotFetchExistingBodies` — `FetchSince` is wired to
  error; the first run still succeeds (proving it is not called) and sets the
  baseline from `LatestUID`.
- `TestScheduler_FirstRun_LatestUIDErrorPropagates` — a `LatestUID` error fails
  the poll.
- Existing `TestScheduler_NoNewMessages` (empty mailbox → no sync state) and
  `TestScheduler_FirstRun_SkipsExistingMessages` (baseline = highest UID) still
  pass. The test `mockProvider` gained a `LatestUID` returning the max UID of its
  messages.

## Definition of Done

1. `make check` passes.
2. Starting the daemon against a large fresh mailbox sets the baseline within one
   `STATUS` round-trip — no envelope or body downloads, visible as an immediate
   `first run: baseline set, skipping existing emails` log with no preceding
   `imap fetch`/`imap body fetch` lines.
3. Subsequent polls fetch only new mail (`UID > baseline`).

## Out of Scope / follow-up

- Processing a bounded, relevant subset on first run (e.g. **unread mail from the
  last 7 days**) instead of skipping everything — a deliberate opt-in that layers
  on top of this baseline logic. Tracked separately (see chat / a future
  `008-06`). This stage keeps first run silent; the follow-up would add a
  targeted `UID SEARCH UNSEEN SINCE <date>` pass before setting the baseline.

# 009-02-daily-digest.md

Status: Planned
Version: 0.1

# Stage 009-02 — Daily Digest

## Goal

Collapse the day's unimportant mail into one **per-account digest** the user can
act on: a numbered list of LLM-judged-unimportant emails (with summaries) plus a
collapsed counter of rule/baseline-dropped junk. The user promotes items back to
"important" by **replying** `/important 3,7`, and clears the remainder with **Mark
read** / **Remove (→Trash)** buttons.

Depends on the engine and provenance from **009-01**. No rule-editing UX here —
that is **009-03**; this stage delivers the digest, the promote mechanics, and the
two bulk buttons.

---

## What Changes

| Before | After |
|--------|-------|
| Ignored mail just sits as `StatusIgnored` | A scheduled per-account digest summarises the day's ignored mail |
| Poller listens only for `callback_query` | Poller also listens for `message` (the `/important` reply command) |
| Telegram actions are per-message buttons | The digest carries bulk **Mark read** / **Remove** buttons over the remainder |
| Provider can MarkRead / FetchBody | Provider gains **MoveToTrash** |
| No way to recover an ignored email | `/important` reclassifies it and re-sends as important |

---

## Directory Structure Changes

```
internal/
  email/
    email.go                # Provider: add MoveToTrash(ctx, uid)
    imap/client.go          # MoveToTrash impl (Trash folder, \Deleted fallback)
  digest/
    digest.go               # NEW: builder — gather day's ignored, split listed/counted
    format.go               # NEW: render the Telegram digest text + counter
    scheduler.go            # NEW: per-account daily job at digest.time
    digest_test.go          # NEW
  db/
    migrations/
      012_digests.sql        # NEW: digests, digest_items, accounts.digest_time
    repo/
      digest_repo.go         # NEW: persist digest + items, lookup by tg message id
      email_repo.go          # query ignored-by-account-by-day; promote helper
  telegram/
    poller.go               # AllowedUpdates += "message"
    handler.go              # handle /important reply; digest_read / digest_remove callbacks
    handler_msg.go          # NEW: message/command dispatch (reply → digest)
    bot.go                  # SendDigest (returns message id), edit/remove keyboard
  config/
    keys.go, config.go       # digest.time, digest.timezone
cmd/email-agent/
  cmd_digest.go             # NEW: `digest show <date> [account]`
  main.go                   # start digest scheduler per account; wire DigestRepo
```

---

## Tasks

### T1 — Migration 012: digests

```sql
-- +goose Up
CREATE TABLE digests (
    id                 TEXT PRIMARY KEY,
    account_id         TEXT NOT NULL,
    digest_date        TEXT NOT NULL,            -- YYYY-MM-DD (account timezone)
    tg_message_id      INTEGER NOT NULL DEFAULT 0,
    sent_at            DATETIME NOT NULL,
    UNIQUE (account_id, digest_date)
);
CREATE TABLE digest_items (
    digest_id  TEXT NOT NULL,
    seq_no     INTEGER NOT NULL,                 -- 1-based number shown to the user
    email_id   TEXT NOT NULL,
    promoted   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (digest_id, seq_no)
);
CREATE INDEX idx_digest_items_email ON digest_items(email_id);

ALTER TABLE accounts ADD COLUMN digest_time TEXT NOT NULL DEFAULT '';  -- '' = use global
-- +goose Down
DROP TABLE IF EXISTS digest_items;
DROP TABLE IF EXISTS digests;
```

Only **listed** items (LLM-judged) get a `seq_no`; rule/baseline-dropped mail is
counted, not numbered (variant (б)). `tg_message_id` ties a `/important` reply
back to the right digest/account.

---

### T2 — Config: schedule

`config/keys.go`: `KeyDigestTime = "digest.time"`, `KeyDigestTimezone =
"digest.timezone"`. `config.go`: `Digest{Time string; Location *time.Location}`,
defaults `20:00` and system local (`digest.timezone` parsed via
`time.LoadLocation`, fallback Local). Per-account override = `accounts.digest_time`
when non-empty. Validate `HH:MM`.

---

### T3 — Provider: MoveToTrash

`internal/email/email.go` — add to `Provider`:

```go
// MoveToTrash moves the message to the mailbox Trash (recoverable). Falls back to
// \Deleted + expunge when no Trash folder is available. Best-effort re: connection.
MoveToTrash(ctx context.Context, uid uint32) error
```

`imap/client.go` — implement with `MOVE` to the Trash folder when the server
advertises one (`[Gmail]/Trash`, `Trash`); otherwise set `\Deleted` and expunge.
Reuse the existing client mutex. Extend the handler's `Mailbox` interface
(009-01/Stage 7) with `MoveToTrash`.

---

### T4 — Digest builder

`internal/digest/digest.go`:

```go
type Item struct { SeqNo int; Email domain.Email; Summary string; Provenance string }
type Counter struct { ByRule map[string]int; Baseline int; Total int }
type Digest struct { AccountID string; Date string; Items []Item; Counter Counter }

// Build gathers emails with StatusIgnored for the account within the date window,
// listing those with decided_by="llm:low" (with their LLM summary) and counting
// the rest grouped by decided_by.
func Build(ctx, emailRepo, classRepo, accountID, date, loc) (Digest, error)
```

`EmailRepo` gains `ListIgnoredByAccountInRange(accountID, from, to)`.

---

### T5 — Digest scheduler

`internal/digest/scheduler.go` — per account, compute the next occurrence of the
account's `digest_time` (or global) in its timezone, sleep, then build + send +
persist. Mirrors the poll scheduler's `Start(ctx)`/`Stop()` shape and runs as one
more goroutine in the daemon errgroup. **Empty day → skip sending** (no digest
row). After sending, persist the `digests` row (with `tg_message_id`) and one
`digest_items` row per listed item.

---

### T6 — Telegram: send + format

`internal/digest/format.go` — render:

```
🗂 Daily digest — work@x.com — 2026-06-24
1. LinkedIn — "5 new jobs for you"        ⓘ llm:low (promo)
2. Medium — "Your weekly digest"          ⓘ llm:low
3. billing@shop — "Receipt #4471"         ⓘ llm:low

+34 filtered by rules/baseline · /important <n,…> to keep · digest show 2026-06-24
[ Mark read ]  [ Remove ]
```

`bot.go` — `SendDigest(ctx, text, kb) (msgID int64, err error)`. Buttons carry
`digest_read:<digestID>` / `digest_remove:<digestID>`.

---

### T7 — Promote via reply

`telegram/poller.go` — add `"message"` to `AllowedUpdates`.
`telegram/handler_msg.go` — on a message that is a **reply** to a known digest
(`ReplyToMessage.MessageId` → `digest_repo` by `tg_message_id`) and text matches
`/important <nums>`:

- parse comma list → `seq_no`s → `digest_items` → emails;
- for each: **reclassify** and **re-send as important** (reuse the notify path
  from the scheduler — extract a `Notify(email, classification)` helper),
  set status `StatusNotified`/important, mark `digest_items.promoted=1`;
- apply the per-account sender-score bump (handled/positive delta);
- the rule-side follow-up (reverse-lookup, allow-rule offer) is **009-03**.

A bare `/important` with no reply → bot replies "reply to a digest message".

---

### T8 — Bulk buttons (remainder)

`handler.go` — new callbacks:
- `digest_read:<id>` → for every `digest_items` with `promoted=0`: `MarkRead`.
- `digest_remove:<id>` → for every `promoted=0` item: `MoveToTrash` (→Trash).

"Remainder" = listed items minus those already promoted (at tap time). After the
action, edit the digest message to drop the keyboard and append a result line
("✓ marked read N" / "🗑 moved to Trash N"). Idempotent if tapped twice.

---

### T9 — CLI: digest show

`cmd/email-agent/cmd_digest.go` — `email-agent digest show <date> [account]`
reprints a past digest **with the counter expanded** (the grouped provenance from
009-01 §provenance):

```
Filtered by rules/baseline: 34
   rule #4  (domain facebookmail.com)   12
   rule #7  (list_id <deals.shop>)       9
   baseline (score < 30)                13
```

This is the agreed CLI form of the counter expansion (no Telegram button).

---

## Tests

| Package | What to test |
|---------|--------------|
| `internal/digest` | Build lists only `llm:low` items, counts the rest grouped; empty day → empty digest |
| `internal/digest` | next-run time honours per-account override and timezone |
| `internal/telegram` | `/important 1,3` reply promotes those items, marks `promoted`, ignores non-reply |
| `internal/telegram` | `digest_remove` calls `MoveToTrash` only on non-promoted items |
| `internal/db/repo` | digest + items round-trip; lookup by `tg_message_id` |
| `internal/email/imap` | MoveToTrash uses MOVE when Trash exists, \Deleted fallback otherwise (fake server/seam) |

---

## Recommended Task Order

```
T1 mig → T2 config → T3 MoveToTrash → T4 builder → T5 scheduler
→ T6 send/format → T7 promote-reply → T8 bulk buttons → T9 CLI → tests
```

---

## Definition of Done

1. `make check` passes.
2. A daily per-account digest is sent at `digest.time`; empty days are skipped.
3. Replying `/important 3,7` reclassifies and re-sends those as important and marks
   them promoted; a non-reply `/important` is rejected with guidance.
4. **Mark read** / **Remove** act on the remainder; Remove moves to Trash.
5. `digest show <date>` reprints the digest with the expanded provenance counter.
6. Numbering is stable per digest message; multi-account replies resolve to the
   correct account via the replied-to message.

---

## Out of Scope (→ 009-03 / future)

- Reverse-lookup of the filtering rule and the remove/exception offer on promote.
- Allow-rule creation from promote.
- Per-message digest expansion as a Telegram button (CLI only here).

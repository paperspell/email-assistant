# 008-02-configurable-poll-interval.md

Status: Implemented
Version: 0.1

# Stage 008-02 — Configurable Per-Box Scan Interval (default 10m)

## Goal

Let each mailbox define how often it is scanned over IMAP, and make the
*default* for new boxes a global, configurable value of **10 minutes**.

This is a small, additive quick fix. The per-account scan interval already
exists end-to-end (`accounts.poll_interval` → `domain.Account.PollInterval` →
`scheduler.Config.PollInterval` → the polling ticker). The only gaps are that
the default is hard-coded to `1m` in two places and is not itself configurable.

---

## What Already Exists (do not rebuild)

| Piece | Location |
|-------|----------|
| Per-account column | `internal/db/migrations/007_accounts.sql` — `poll_interval TEXT NOT NULL DEFAULT '1m'` |
| Domain field | `internal/domain/account.go` — `Account.PollInterval time.Duration` |
| Persist / scan | `internal/db/repo/account_repo.go` — Upsert writes `PollInterval.String()`, scan parses it |
| CLI prompt | `cmd/email-agent/cmd_account.go:222` — `promptDuration("  Poll interval", cur.PollInterval)` |
| Daemon wiring | `cmd/email-agent/main.go:158` — `PollInterval: acc.PollInterval` |
| Ticker | `internal/scheduler/scheduler.go:66` — `time.NewTicker(s.cfg.PollInterval)` |
| Validation | `internal/config/config.go:206` — rejects non-positive `poll_interval` |

So per-box configuration is **done**. This stage only changes the default and
makes it configurable.

---

## What Changes

| Before | After |
|--------|-------|
| New-account default scan interval is hard-coded `1m` (`cmd_account.go:191`) | Default comes from a global setting, defaulting to `10m` |
| The "10 minute" default is not configurable | New global setting `poll.default_interval` (default `10m`) |
| Migration column/backfill default is `1m` | New migration sets the column default and backfills untouched rows to `10m` |
| Per-box override | Unchanged — still set/edited via `account add` / `account edit` |

---

## Tasks

### T1 — New global setting key

`internal/config/keys.go` — add:

```go
KeyPollDefaultInterval = "poll.default_interval"
```

Add it to `KnownKeys` so `config list` / `config set` accept it. It is not
sensitive (no masking).

---

### T2 — Default poll interval in config

`internal/config/config.go`:

- Add a package constant `DefaultPollInterval = 10 * time.Minute`.
- Add a `Poll struct{ DefaultInterval time.Duration }` field on `Config`
  (mirroring the existing `OAuth`/`Content` grouping).
- In `applySettings`, parse `KeyPollDefaultInterval` with `time.ParseDuration`;
  on empty or parse error fall back to `DefaultPollInterval`.
- In `validate`, reject a non-positive `Poll.DefaultInterval` with a clear
  message (parallels the existing per-account `poll_interval` check at
  `config.go:206`).

The daemon still reads each account's own `PollInterval`; this global value only
seeds the CLI prompt and new-account inserts. No change to `main.go` wiring.

---

### T3 — CLI uses the global default for new boxes

`cmd/email-agent/cmd_account.go`:

- In `addOrEditAccount`, replace the hard-coded `PollInterval: time.Minute`
  (line 191) with the global default. Load it via the existing `SettingsRepo`
  (`sr` is already threaded in) and parse with the same fallback to `10m`.
- The prompt at line 222 already pre-fills with `cur.PollInterval`, so for a new
  box it now shows `10m`; for an edit it shows the box's current value. No other
  prompt change needed.
- Factor the "settings value or 10m" lookup into a tiny helper so config and CLI
  share one fallback rule.

---

### T4 — Migration 009: default + backfill to 10m

`internal/db/migrations/009_poll_interval_default.sql` (NEW):

```sql
-- +goose Up
-- Backfill boxes still on the old 1m default to the new 10m default. Rows a user
-- explicitly set to some other value are left untouched.
UPDATE accounts SET poll_interval = '10m' WHERE poll_interval = '1m';

-- The column DEFAULT in 007 is cosmetic (Upsert always supplies poll_interval),
-- but align it for direct SQL inserts. SQLite cannot ALTER a column default in
-- place; document this as a no-op note rather than recreating the table.
-- (Forward-only: new inserts go through the app, which supplies the value.)

-- +goose Down
-- No-op: forward-only.
```

> Decision needed only if you object: backfilling `1m → 10m` assumes existing
> `1m` rows were the unmodified default, not a deliberate choice. If any box was
> intentionally set to `1m`, change it back with `account edit` after migrating.

---

### T5 — Tests

| Package | What to test |
|---------|--------------|
| `internal/config` | `poll.default_interval` parses; empty/invalid falls back to `10m`; non-positive fails validation |
| `internal/db` | Migration 009 backfills `1m` rows to `10m`, leaves other values (e.g. `5m`) untouched |
| `cmd/email-agent` (or repo seam) | A newly added account with no override gets the global default |

---

### T6 — Docs

- `docs/settings.md` — document `poll.default_interval` (default `10m`) and that
  each box can override it via `account add` / `account edit`.
- `docs/db-schema.md` — note the `10m` default for `poll_interval` and migration `009`.

---

## Recommended Task Order

```
T1 → keys.go: poll.default_interval
T2 → config: Poll.DefaultInterval + parse + validate
T3 → cmd_account: seed new-box default from the setting
T4 → migration 009: backfill 1m → 10m
T5 → tests
T6 → docs
```

---

## Definition of Done

1. `make check` passes.
2. `email-agent config set poll.default_interval 15m` changes the default offered
   to new boxes; unset, the default is `10m`.
3. `email-agent account add` pre-fills the poll-interval prompt with the global
   default; the entered value is stored per box.
4. `email-agent account edit` shows and can change an individual box's interval
   without affecting others.
5. Existing boxes left on the old `1m` default are migrated to `10m`; boxes set
   to any other value are unchanged.
6. The daemon polls each box on its own interval (verifiable in the
   `scheduler starting … poll_interval=…` log line).

---

## Out of Scope

- Changing the polling mechanism (still ticker-based; no IMAP IDLE).
- Per-box backoff/jitter tuning (`pollWithBackoff` unchanged).
- Any UI beyond the existing CLI prompts.

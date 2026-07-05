# 008-04-token-hot-reload.md

Status: Phase 1 Implemented; Phase 2 Draft
Version: 0.2

# Stage 008-04 — Hot Reload (OAuth tokens first, config reconciliation next)

## Goal

Let the running daemon pick up configuration changes made by the separate CLI
process **without a restart**. The immediate, high-value case: after
`email-agent account edit <email>` re-authorizes a Gmail box, the daemon should
resume polling it on its own — no `Ctrl-C` + `run` required.

This is split into two phases. **Phase 1 (token hot-reload)** solves the
re-authorization case narrowly and is the recommended first slice. **Phase 2
(config reconciliation)** generalises to accounts/settings and is a larger,
structural change.

---

## Why a restart is needed today

`runDaemon` (`cmd/email-agent/main.go`) builds everything **once** at startup and
holds it for the process lifetime:

| Built once | Consequence for live edits |
|-----------|----------------------------|
| `cfg, _ := config.Load(...)` | Global settings (min importance, LLM provider/model, content mode) are snapshotted |
| `newProvider(...)` per account → `oauth.TokenSource(...)` | The `persistingSource` wraps `oauth2.ReuseTokenSource`, which **caches the refresh token in memory** from startup; a re-auth written to the DB is never seen |
| `scheduler.New(cfg)` per account, `g.Go(sched.Start)` | Schedulers exist for the accounts present at startup; ticker interval is fixed at construction |
| `digest.New(...)` per account | Same |
| `handler.Mailboxes` / `handler.Accounts` maps | Built once, read by the Telegram poller goroutine |

Already hot (do not rebuild): **rules and clauses** are reloaded every poll
(`scheduler.go` — "Load the account's enabled rules and active ignore clauses
once per poll, so CLI/Telegram edits take effect on the next cycle").

The CLI edits the DB from a **different process**, so there is no in-process
event; the daemon must either be signalled or observe the DB.

---

## Phase 1 — OAuth token hot-reload (recommended first)

### Idea

Replace the memory-caching token source with one that can **re-read the account's
tokens from the DB** when its cached refresh token is rejected. Combined with the
008-03 auto-reconnect, the daemon then self-heals after `account edit`:

1. Refresh token expires → reconnect → `invalid_grant`.
2. Telegram alert fires (008-03). User runs `account edit`, re-authorizes.
3. On the next poll, the token source sees `invalid_grant` from its cached token,
   reloads tokens from `AccountRepo`, and if they changed, rebuilds the base
   source and retries — succeeding with the freshly stored refresh token.

No signal, no restart, no new IPC.

### Sketch

A new token source in `internal/auth/oauth` (or a small wrapper in the daemon)
that owns a `reload func() (Tokens, error)`:

```go
// reloadingSource wraps persistingSource. On invalid_grant it reloads tokens
// from storage; if the stored refresh token differs from the cached one, it
// rebuilds the base source and retries once.
type reloadingSource struct {
    mu      sync.Mutex
    cfg     *oauth2.Config
    ctx     context.Context
    inner   oauth2.TokenSource      // persistingSource over the current tokens
    current Tokens
    reload  func() (Tokens, error)  // reads AccountRepo for this account
    persist func(Tokens) error
}
```

`Token()` calls `inner.Token()`; on `IsReauthRequired`, it calls `reload()`, and
if `reload().RefreshToken != current.RefreshToken`, rebuilds `inner` and retries
once. The daemon wires `reload` to `accountRepo` + the account id.

### Phase 1 tasks — implemented

- **T1 ✅** — `ReloadingTokenSource` / `reloadingSource` in
  `internal/auth/oauth/oauth.go`. On `invalid_grant` it calls the `ReloadFunc`,
  and if the stored refresh token differs from the cached one, rebuilds the inner
  `TokenSource` and retries once. Inner-source construction is injected via a
  `build` seam (`newReloadingSource`) so the logic is testable without a network.
  Tests: `internal/auth/oauth/reload_test.go`.
- **T2 ✅** — `newProvider` (`cmd/email-agent/main.go`) now builds
  `oauth.ReloadingTokenSource(...)` with a `reload` callback that re-reads the
  account via `accountRepo.Get(accID)` and returns its token fields. The existing
  `persist` write-back (`accountRepo.UpdateTokens`) is unchanged.
- **T3 ✅** — Recovery note in `008-03` updated: the daemon restart is no longer
  required; re-authorization is picked up on the next poll.

### Phase 1 caveats

- Recovery is still **poll-latency bound**: it heals on the next poll after the
  user re-authorizes, not instantly.
- The re-auth still happens via the CLI's browser consent; Phase 1 only removes
  the daemon restart.
- SQLite concurrency: the daemon and the `account edit` process both open the
  encrypted DB. Confirm WAL + busy-timeout handle the brief concurrent write, or
  document that `account edit` may need a moment.

---

## Phase 2 — Config reconciliation (accounts + settings)

Generalises hot reload to the rest of the config. This is the structural part.

### Trigger options

| Option | Pros | Cons |
|--------|------|------|
| **SIGHUP** (`kill -HUP <pid>`) | Classic, no IPC, scriptable | User/CLI must know the pid; `account edit` would need to send it |
| DB **version/dirty flag** the daemon polls | Fully automatic; CLI just bumps a counter | Adds a poll loop; small latency |
| Telegram `/reload` command | User-driven from anywhere | Manual; not automatic |
| Local IPC socket | Precise | New surface area; against the single-binary/no-server grain |

Recommended: **SIGHUP** for an explicit reload, optionally paired with a DB
version bump written by mutating CLI commands so `account add/edit/remove` can
signal automatically when they can find the pid.

### Structural change — a supervisor

Introduce a supervisor that owns the set of per-account workers and can start,
stop, and replace them:

- Give each account a **child context** (`context.WithCancel` off the daemon
  context) so an individual scheduler/digest can be cancelled without touching
  the others.
- On reload: `config.Load` again, then **diff** the account set:
  - added/enabled → build provider + scheduler + digest, start them;
  - removed/disabled → cancel the child context, close the provider;
  - changed (poll interval, host/port, auth) → cancel + rebuild.
- Swap the handler's `Mailboxes`/`Accounts` behind a mutex (or an atomically
  swapped immutable snapshot), since the Telegram poller reads them concurrently.
- Global settings captured by value in `scheduler.Config` (min importance,
  content mode, LLM provider) → either rebuild affected schedulers on change, or
  refactor those to be read live per poll (as rules/clauses already are).

### Phase 2 nuances / risks

- **Concurrency.** `handler.Mailboxes`/`Accounts` are plain maps read by the
  poller goroutine; mutating them on reload without synchronization is a data
  race. Needs a lock or copy-on-write swap.
- **In-flight actions.** A shared `imap.Client` is used by both the poll loop and
  Telegram callbacks. Replacing/closing a provider must not close a connection
  mid-command — close the old client only after the new one is swapped in and
  under the client's `mu`.
- **errgroup semantics.** Today one worker returning an error cancels the whole
  group. A supervisor must isolate per-account failures (a restarting scheduler
  must not bring down the daemon) — this partly overlaps with the 008-03 change
  that made a startup re-auth failure non-fatal.
- **Partial reload failure.** If the new config fails validation, keep running
  the old one and report the error (log + optional Telegram alert) rather than
  half-applying.
- **Digest schedulers** hold their own timers; reload must cancel/rebuild them
  alongside their account.

---

## Recommended path

1. **Ship Phase 1** — it directly removes the restart after `account edit`, is
   self-contained (one new token source + wiring + tests), and needs no new
   trigger mechanism or supervisor.
2. Evaluate whether Phase 2 is needed. Adding/removing accounts without a restart
   is convenient but rarer than re-auth; it can wait until the supervisor
   refactor is worth its complexity.

---

## Definition of Done (Phase 1)

1. `make check` passes.
2. With the daemon running and a Gmail box whose refresh token has expired:
   running `account edit <email>` + re-authorizing causes the daemon to resume
   polling that box **on the next poll**, with no restart.
3. Other accounts are unaffected throughout.
4. Unit tests cover: reload-and-retry on `invalid_grant`; no reload when the
   stored refresh token is unchanged; a still-invalid stored token surfaces the
   error (and the 008-03 alert path still fires).

---

## Out of Scope (this stage)

- IMAP IDLE / push polling.
- A settings UI (see `docs/settings-ui.md`), which would build on Phase 2.
- Instant (sub-poll-interval) recovery; Phase 1 heals on the next poll.

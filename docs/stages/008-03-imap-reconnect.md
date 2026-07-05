# 008-03-imap-reconnect.md

Status: Implemented
Version: 0.1

# Stage 008-03 — IMAP Auto-Reconnect + OAuth Re-auth Telegram Alert

## Goal

Make a long-running daemon survive dropped IMAP connections, and — crucially —
detect an **expired/revoked OAuth refresh token during a live session**, notify
the user over Telegram with recovery instructions instead of failing silently in
the logs.

This closes two gaps that existed before this stage:

1. `imap.Client` connected once at startup and never reconnected. A dropped
   connection (Gmail idle timeout, network blip) left every subsequent poll
   failing against a stale client, with no recovery short of a daemon restart.
2. A rejected refresh token (the ~7-day Gmail "Testing" mode expiry) surfaced
   only as an `Error` log line. There was no Telegram alert, so the user learned
   about it only by noticing the mailbox had gone quiet.

---

## Background — why the two are linked

The refresh-token rejection (`invalid_grant`) is only observable when
`authenticate` runs, i.e. inside `Connect`. Since the client never reconnected,
after the initial startup connection the token source was never consulted again.
So mid-session token expiry could not surface at all until the process
restarted. Auto-reconnect is therefore the mechanism that makes the re-auth
alert fire while the daemon is running: a dropped connection triggers a
reconnect, the reconnect re-authenticates, and a dead refresh token is exposed.

---

## What Changed

### 1. Telegram re-auth alert (scheduler)

| Piece | Location |
|-------|----------|
| Alert interface | `internal/scheduler/scheduler.go` — `Alerter interface { SendAlert(ctx, text) error }` |
| Config field | `scheduler.Config.Alerter` (nil disables alerts) |
| Dedup state | `Scheduler.reauthAlerted bool` (reset after a successful poll) |
| Trigger helper | `Scheduler.alertReauthIfNeeded(ctx, err)` — fires on `oauth.IsReauthRequired(err)` |
| Message body | `Scheduler.reauthAlertText()` — HTML with `account edit` instructions |
| Wire points | `Start` (Connect error) and `pollWithBackoff` (after retries) |
| Bot method | `internal/telegram/bot.go` — `Bot.SendAlert(ctx, text)` (HTML parse mode) |
| Daemon wiring | `cmd/email-agent/main.go` — `Alerter: bot` in the scheduler config |

Behaviour:

- The alert is sent **once per outage** (deduped via `reauthAlerted`), not on
  every poll. A successful poll clears the flag so a later expiry alerts again.
- If sending the alert fails (Telegram down), the flag is not set, so the next
  attempt retries.
- A mailbox whose token has expired **at daemon startup** is now skipped (with
  the alert) instead of returning an error from `Start` that would tear down the
  whole `errgroup` (and, under a restart-looping service, spam the alert). Other
  accounts keep polling.

### 2. IMAP auto-reconnect (client)

| Piece | Location |
|-------|----------|
| Split connect | `Connect` (locks `mu`) → `dial()` (establishes + stores the client) |
| Retry wrapper | `exec(op func() error) error` — reconnects once and retries on a dropped connection |
| Reconnect | `reconnect()` — closes the dead client and re-`dial`s (re-authenticates) |
| Error classifier | `isConnErr(err)` — connection-loss errors vs server command errors |
| Wrapped ops | `FetchSince`, `MarkRead`, `FetchBody`, `MoveToTrash` run inside `exec` |

Behaviour:

- `exec` reconnects and retries **at most once**. A reconnect failure (including
  a rejected refresh token) is returned immediately — the op is not retried — so
  the re-auth error propagates to the scheduler.
- Only errors classified by `isConnErr` (`net.ErrClosed`, `io.EOF`,
  `io.ErrUnexpectedEOF`, and the substrings `use of closed network connection`,
  `connection reset`, `broken pipe`, `connection closed`, `EOF`) trigger a
  reconnect. Server-side command errors are returned as-is.
- The per-method `if c.client == nil { "not connected" }` guards were removed;
  `exec` now establishes the connection on demand (self-healing).
- `Connect` now holds `mu` while assigning `c.client`, removing a data race with
  concurrent Telegram-triggered mailbox actions.

---

## Recovery flow (user-facing)

When the alert fires:

1. On the machine running the daemon: `email-agent account edit <email>`, answer
   **y** to "Re-authorize with Google now?" and complete Google consent in the
   browser.
2. That's it — the running daemon picks up the new token on the next poll; no
   restart is required.

> Restart-free recovery is provided by the reloading token source added in
> **[stage 008-04](008-04-token-hot-reload.md)** (phase 1). Before that stage a
> restart was needed because the token source cached the refresh token in memory.

---

## Tests

| File | Coverage |
|------|----------|
| `internal/scheduler/scheduler_alert_test.go` | Alert sent once + dedup; reset after success; non-reauth errors ignored; nil `Alerter` no panic; send-failure is retryable; message uses email when name empty |
| `internal/email/imap/reconnect_test.go` | `isConnErr` table; `exec` succeeds first try; no reconnect when connected; reconnect+retry on conn error; no retry on command error; re-auth error from reconnect propagates |

`reconnect_test.go` uses a `reconnectFn` test seam on `Client` to exercise
`exec`'s reconnect/retry path without a real IMAP server.

---

## Definition of Done

1. `make check` passes (build, lint, unit + migration tests; `-race` clean).
2. A dropped IMAP connection is transparently re-established on the next
   operation without a daemon restart.
3. An expired/revoked refresh token produces a single Telegram alert naming the
   account with re-authorization instructions, detected both at startup and
   mid-session (via reconnect).
4. One account's expired token does not take down the daemon; other accounts
   keep polling.

---

## Out of Scope

- Picking up a re-authorized token without restarting the daemon → **008-04**.
- IMAP IDLE / push (still ticker-based polling).
- Reconnect backoff/jitter tuning (the scheduler's `pollWithBackoff` already
  bounds retries; `exec` itself retries only once).

# 007-02-mailbox-actions.md

Status: Draft
Version: 0.1

# Stage 007-02 — Mailbox Actions from Telegram

## Goal

Let the Telegram action buttons act on the mailbox itself:

- **Handled** / **Ignore** mark the email as read (`\Seen`) in the mailbox.
- **Details** shows the email body (fetched on demand), not just the
  classification breakdown.

Both go **through the `email.Provider` abstraction** — no direct IMAP access
outside `internal/email/imap`, and **no second connection**: the existing
per-account provider is reused, with concurrent access serialized by a mutex.

No database schema change. The body is never persisted (Privacy First) — it is
re-fetched live when Details is pressed.

---

## What Changes

| Before | After |
|--------|-------|
| Polling never marks mail read (`PEEK`); buttons don't touch the mailbox | Handled/Ignore set `\Seen` on the message; polling still uses `PEEK` |
| `Details` sends the classification reasons only | `Details` fetches and shows the email body + a compact classification line |
| `Provider` = `Connect` / `FetchSince` / `Close` | `Provider` also exposes `MarkRead` and `FetchBody` |
| Telegram `Handler` has no mailbox access | `Handler` resolves the per-account provider from a registry |

Read state stays driven by **explicit user triage**, never by background
polling — preserving the fix from Plan A (`PEEK`).

---

## Design

The capability the `Handler` lacks today is mailbox access. Rather than open a
second IMAP connection, we **extend the `Provider` interface** and reuse the
same per-account provider instance that the scheduler already owns.

- The two new operations are expressed abstractly, so they port to a future
  Gmail-API transport (`messages.modify` to drop `UNREAD`; `messages.get` for
  the body).
- The scheduler's polling goroutine and the Telegram poller's button goroutine
  share one connection, so the IMAP client serializes all commands with a
  `sync.Mutex`. A button action waits at most one poll's worth of time.
- All actions are **best-effort**: a connection error is logged, the button
  still completes (status + sender feedback are written regardless).

### Provider interface change

`internal/email/provider.go`:

```go
type Provider interface {
	Connect(ctx context.Context) error
	FetchSince(ctx context.Context, lastUID uint32) ([]Message, error)
	MarkRead(ctx context.Context, uid uint32) error          // NEW
	FetchBody(ctx context.Context, uid uint32) (string, error) // NEW
	Close() error
}
```

The Telegram `Handler` depends on a **narrow interface it defines itself**
(satisfied structurally by `email.Provider`), so the telegram package stays
decoupled from the email package while still only ever calling the abstraction:

```go
// internal/telegram
type Mailbox interface {
	MarkRead(ctx context.Context, uid uint32) error
	FetchBody(ctx context.Context, uid uint32) (string, error)
}
```

---

## Directory Structure Changes

```
internal/
  email/
    provider.go        # add MarkRead, FetchBody to Provider
    imap/
      client.go        # sync.Mutex; implement MarkRead + FetchBody; guard existing commands
  telegram/
    handler.go         # Mailbox lookup; mark read on Handled/Ignore; body in Details
    bot.go             # (only if a new follow-up formatter is added)
cmd/email-agent/
  main.go              # keep per-account providers in a registry; pass to Handler
```

No migration.

---

## Tasks

### T1 — Extend the Provider interface

`internal/email/provider.go` — add `MarkRead` and `FetchBody` as above. Update
the doc comments to state they operate on a single message UID and are
best-effort with respect to connection state.

Any existing fakes/mocks implementing `Provider` (scheduler tests) gain no-op
implementations.

---

### T2 — IMAP client: mutex + the two operations

`internal/email/imap/client.go`:

- Add `mu sync.Mutex` to `Client`. Lock it around every block that issues IMAP
  commands — `FetchSince`, `fetchBodies`, and the two new methods — so the
  shared connection is never used by two goroutines at once.

- `MarkRead`:

```go
func (c *Client) MarkRead(_ context.Context, uid uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return fmt.Errorf("imap: not connected")
	}
	set := imaplib.UIDSetNum(imaplib.UID(uid))
	cmd := c.client.Store(set, &imaplib.StoreFlags{
		Op:    imaplib.StoreFlagsAdd,
		Flags: []imaplib.Flag{imaplib.FlagSeen},
	}, nil) // UIDSet ⇒ UID STORE
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("imap mark read uid %d: %w", uid, err)
	}
	return nil
}
```

- `FetchBody` — single-UID variant of the existing `fetchBodies`, reusing
  `peekBodySection`, `stripHTML`, and `truncate`:

```go
func (c *Client) FetchBody(_ context.Context, uid uint32) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return "", fmt.Errorf("imap: not connected")
	}
	set := imaplib.UIDSetNum(imaplib.UID(uid))
	msgs, err := c.client.Fetch(set, &imaplib.FetchOptions{
		UID:         true,
		BodySection: []*imaplib.FetchItemBodySection{peekBodySection},
	}).Collect()
	if err != nil {
		return "", fmt.Errorf("imap fetch body uid %d: %w", uid, err)
	}
	for _, m := range msgs {
		if b := m.FindBodySection(peekBodySection); len(b) > 0 {
			return truncate(stripHTML(string(b)), maxBodyLen), nil
		}
	}
	return "", nil
}
```

`FetchBody` uses `PEEK`, so viewing the body does **not** mark it read — only the
explicit Handled/Ignore buttons do.

---

### T3 — Provider registry in main.go

`cmd/email-agent/main.go` — today the per-account providers built in the loop are
handed to the scheduler and the reference dropped. Instead, collect them:

```go
mailboxes := make(map[string]email.Provider, len(cfg.Accounts))
for _, acc := range cfg.Accounts {
	provider, err := newProvider(acc, fetchBody, logger)
	if err != nil {
		return err
	}
	mailboxes[acc.ID] = provider
	sched := scheduler.New(scheduler.Config{ /* ... Provider: provider ... */ })
	g.Go(func() error { return sched.Start(gCtx) })
}
```

Pass `mailboxes` to the `Handler` (typed as `map[string]telegram.Mailbox`, which
the providers satisfy structurally).

---

### T4 — Handler: act on the mailbox

`internal/telegram/handler.go`:

- Add `Mailboxes map[string]Mailbox` to `Handler`.
- Helper to resolve and run an action best-effort:

```go
func (h *Handler) markRead(ctx context.Context, e *domain.Email) {
	mb, ok := h.Mailboxes[e.AccountID]
	if !ok {
		return
	}
	if err := mb.MarkRead(ctx, e.MessageUID); err != nil {
		h.Logger.Error(err, "email_id", e.ID, "account_id", e.AccountID)
	}
}
```

- Call `h.markRead(ctx, e)` from `handleHandled` and `handleIgnore` (after the
  DB status update; failure must not abort the button).
- `handleDetails` fetches the body and includes it:

```go
mb, ok := h.Mailboxes[e.AccountID]
var body string
if ok {
	if b, err := mb.FetchBody(ctx, e.MessageUID); err != nil {
		h.Logger.Error(err, "email_id", e.ID)
	} else {
		body = b
	}
}
text := formatDetails(e, all, body)
return h.Bot.SendFollowUp(ctx, text)
```

---

### T5 — Details formatting

`internal/telegram/handler.go` — extend `formatDetails` to render, in order:

```
ℹ Email details

From: …
Subject: …
Date: …

<body, or "(body unavailable)" when fetch failed / empty>

Importance: <llm level> (score N) · <category>     ← compact one-liner
```

- Body trimmed to fit Telegram's 4096-char message limit (already capped at
  `maxBodyLen` = 3000 by `FetchBody`; leave headroom for headers).
- `SendFollowUp` sends plain text today; keep it plain (no HTML escaping needed)
  or switch to the same HTML mode as notifications if formatting is wanted —
  decide in implementation, default plain.
- The full rule/LLM breakdown can move to the compact line or stay below the
  body; keep the message readable.

---

### T6 — Tests

| Package | What to test |
|---------|--------------|
| `internal/email/imap` | `MarkRead` issues a UID STORE; `FetchBody` returns stripped/truncated text (against a fake IMAP server or the existing test seam) |
| `internal/telegram` | Handled/Ignore call `MarkRead` with the email's UID; a `MarkRead` error does not fail the callback |
| `internal/telegram` | Details calls `FetchBody` and includes the body; missing provider / fetch error degrades to "(body unavailable)" without erroring |
| `internal/telegram` | `formatDetails` includes body + compact classification, stays within length bounds |

A fake `Mailbox` in the telegram tests records calls and can be set to error.

---

### T7 — Docs

- `docs/stages/003_004-01-telegram-interaction.md` — note the new button
  side-effects (mark read; details shows body).
- `docs/architecture.md` — `Provider` now also exposes `MarkRead` / `FetchBody`.
- `README.md` — update the button descriptions if they are listed there.

---

## Dependencies

None. Reuses `emersion/go-imap/v2` (STORE + FETCH already available) and the
existing `stripHTML` / `truncate` helpers.

---

## Recommended Task Order

```
T1  → Provider interface: MarkRead, FetchBody
T2  → IMAP client: mutex + implementations
T3  → main.go: provider registry → Handler
T4  → Handler: mark read on Handled/Ignore; body in Details
T5  → Details formatting
T6  → tests
T7  → docs
```

---

## Definition of Done

1. `make check` passes.
2. Pressing **Handled** or **Ignore** marks the email read in the mailbox; the
   change is visible in another mail client.
3. Background polling still does **not** mark mail read.
4. Pressing **Details** shows the email body (re-fetched live), with a compact
   classification line; the body is not stored in the database.
5. A connection/fetch failure on any button is logged and degrades gracefully —
   the button still completes and the notification is not broken.
6. No IMAP access exists outside `internal/email/imap`; the `Handler` uses only
   the `Provider`/`Mailbox` abstraction.

---

## Out of Scope

- Automatic reconnect of a dropped IMAP connection (Plan A — IMAP Connection
  Resilience); until then mailbox actions are best-effort while disconnected.
- Marking read on **Details** (read-only view stays non-mutating).
- Per-button configuration (e.g. mark read on Handled but not Ignore) — both
  mark read for now; can become a setting later.
- Showing HTML-rendered bodies or attachments (plain-text, stripped, truncated).

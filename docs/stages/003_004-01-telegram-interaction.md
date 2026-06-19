# 003_004-01-telegram-interaction.md

Status: Draft
Version: 0.2

# Stage 003-04 — Telegram Interaction & State Management

## Goal

Close the feedback loop: the user taps a button on a Telegram notification to act on the email.
Each action updates the email's lifecycle status and adjusts the sender's importance score,
so the classifier improves automatically over time.

No LLM, no body content, no multi-account. All interaction is button-based.

---

## What Changes

| Before | After |
|--------|-------|
| Notification is fire-and-forget | Notification has action buttons attached |
| Email has 3 statuses: new / notified / ignored | 5 statuses: + handled / reply_needed |
| Sender score never changes after first classification | Adjusted by user feedback |
| Notification message: from + subject + date | + classification level, score, reasons |

---

## Interaction Model

Notification messages are sent with an **inline keyboard** — three buttons attached to the message:

```
📧 New email

From: John Doe <john@work.com>
Subject: Urgent meeting scheduled
Date: Mon, 15 Jun 2026 10:30 UTC

Importance: important (score 75)
Why: baseline: +40; urgent keyword: +25; meeting keyword: +20; unknown sender: −10

[✓ Handled]  [✗ Ignore]  [ℹ Details]
```

When the user taps a button:
1. Bot receives a `callback_query` containing the button's data.
2. Bot processes the action.
3. Bot answers the callback (dismisses the loading spinner).
4. Bot edits the original message: replaces the keyboard with a status line, e.g. `✓ Handled`.

This works in **private chats and channels** alike — no need to reply to messages.

---

## Button Actions

| Button | Callback data | Action |
|--------|--------------|--------|
| ✓ Handled | `handled:{emailID}` | Mark email handled; sender score +25; mark message read in the mailbox |
| ✗ Ignore  | `ignore:{emailID}`  | Mark email ignored; sender score −25; mark message read in the mailbox |
| ℹ Details | `details:{emailID}` | Bot sends a follow-up with the email body (fetched on demand) + classification breakdown |

The email ID is embedded directly in the callback data — no message ID lookup needed.

> Mailbox side-effects (marking read, fetching the body) were added in Stage
> 007-02 (`docs/stages/007-02-mailbox-actions.md`). They go through the
> `email.Provider` abstraction and are best-effort: a connection failure is
> logged and never breaks the button. Background polling still does **not** mark
> mail read. The body is fetched live and not stored.

After `handled` or `ignore`, the keyboard is replaced with a single confirmation line:

```
✓ Handled — sender score updated
```

or

```
✗ Ignored — sender will score lower in future
```

After `details`, the keyboard remains so the user can still act after reading the breakdown.

---

## Details Message Format

Sent as a follow-up message (does not replace the original notification):

```
ℹ Email details

From: John Doe <john@work.com>
Subject: Urgent meeting scheduled
Date: Mon, 15 Jun 2026 10:30 UTC

Classification: important (score 75)
Category: work
Reasons:
  • baseline: +40
  • urgent keyword in subject: +25
  • meeting keyword in subject: +20
  • unknown sender: −10
```

---

## Sender Score Adjustment

Scores are stored on `senders.importance_score` and already feed into the classifier
(`bonus = senderScore / 5`, clamped to [0, 100]).

| Button   | Delta | Effect on future emails from same sender |
|----------|-------|------------------------------------------|
| ✓ Handled | +25  | Scores roughly +5 higher |
| ✗ Ignore  | −25  | Scores roughly −5 lower  |

Score is clamped to [0, 100] after adjustment.
Repeated feedback accumulates: 4× handled → +100 cap → every future email from that sender is +20.

---

## Architecture

The daemon runs two concurrent components:

```
main.go
  ├── scheduler.Start(ctx)     — IMAP poll loop (existing)
  └── telegram.Poller.Run(ctx) — Telegram update poll loop (new)
```

Both share the same `*sql.DB` connection pool and are cancelled together on SIGINT/SIGTERM
via `errgroup` (`golang.org/x/sync` is already a dependency).

The poller runs a 30-second long poll against `getUpdates` listening for `callback_query`
updates and dispatches each to `Handler.Handle`.

---

## Directory Structure Changes

```
internal/
  domain/
    email.go               # add StatusHandled, StatusReplyNeeded; add TelegramMessageID field
  telegram/
    bot.go                 # SendNewEmail: attach inline keyboard, return message ID
    notifier.go            # update Notifier interface
    poller.go              # NEW: long-poll getUpdates, track offset in settings
    handler.go             # NEW: dispatch callback_query actions
    handler_test.go        # NEW
  db/
    migrations/
      004_telegram_feedback.sql   # NEW
    repo/
      email_repo.go        # add SetTelegramMessageID
```

---

## Tasks

### T1 — Domain model

`internal/domain/email.go`

Add statuses:
```go
const (
    StatusNew         EmailStatus = "new"
    StatusNotified    EmailStatus = "notified"
    StatusIgnored     EmailStatus = "ignored"
    StatusHandled     EmailStatus = "handled"      // new
    StatusReplyNeeded EmailStatus = "reply_needed"  // new
)
```

Add field to `Email`:
```go
type Email struct {
    // existing fields ...
    TelegramMessageID int64 // 0 until a notification is sent
}
```

---

### T2 — Migration: 004_telegram_feedback.sql

```sql
-- +goose Up

ALTER TABLE emails ADD COLUMN telegram_message_id INTEGER NOT NULL DEFAULT 0;

-- +goose Down

-- SQLite does not support DROP COLUMN on older versions; migration is irreversible.
```

The Telegram update offset is stored in the existing `settings` table under
the key `telegram.update_offset` (integer string). No new table needed.

Add `telegram.update_offset` to `config.KnownKeys`.

---

### T3 — Notifier interface and Bot

**`notifier.go`**

```go
type Notifier interface {
    SendNewEmail(ctx context.Context, e domain.Email, c domain.Classification) (int64, error)
}
```

Returns the Telegram message ID so the scheduler can persist it.
Takes `Classification` to include level, score, and reasons in the message body.

**`bot.go`**

- `SendNewEmail` calls `SendMessage` with an `InlineKeyboardMarkup`:
  ```go
  keyboard := gotgbot.InlineKeyboardMarkup{
      InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
          {Text: "✓ Handled", CallbackData: "handled:" + e.ID},
          {Text: "✗ Ignore",  CallbackData: "ignore:"  + e.ID},
          {Text: "ℹ Details", CallbackData: "details:" + e.ID},
      }},
  }
  ```
- Returns `(msg.MessageId, nil)` on success.
- `formatMessage(e domain.Email, c domain.Classification) string` produces the enriched
  format: from, subject, date, importance level + score, reasons joined by `; `.

Add helper to `Bot`:
```go
// AnswerCallback dismisses the spinner on a callback_query.
func (b *Bot) AnswerCallback(queryID string) error

// EditReplyMarkup replaces the inline keyboard on a sent message.
func (b *Bot) EditReplyMarkup(msgID int64, markup *gotgbot.InlineKeyboardMarkup) error

// SendReply sends a message in the same chat as a follow-up.
func (b *Bot) SendReply(ctx context.Context, text string) error
```

---

### T4 — EmailRepo: SetTelegramMessageID

```go
// SetTelegramMessageID persists the Telegram message ID after a notification is sent.
func (r *EmailRepo) SetTelegramMessageID(ctx context.Context, emailID string, msgID int64) error
```

No `GetByTelegramMessageID` needed — the email ID is carried in callback data directly.

---

### T5 — Scheduler: persist message ID and pass Classification

In `processMessage`, after a successful `SendNewEmail`:

```go
msgID, err := s.cfg.Notifier.SendNewEmail(ctx, e, classification)
if err != nil {
    return err
}
if err := s.cfg.EmailRepo.SetTelegramMessageID(ctx, e.ID, msgID); err != nil {
    return err
}
```

---

### T6 — Telegram poller

`internal/telegram/poller.go`

```go
type Poller struct {
    Bot          *Bot
    Handler      *Handler
    SettingsRepo *repo.SettingsRepo
    Logger       log.Logger
}

func (p *Poller) Run(ctx context.Context) error
```

Sequence:
1. Load current offset from `settings["telegram.update_offset"]` (0 if absent).
2. Call `bot.GetUpdates` with `offset+1`, `timeout=30`, `allowed_updates=["callback_query"]`.
3. For each update: call `Handler.Handle(ctx, update)`.
4. After processing all updates: persist `max(updateID) + 1` as new offset.
5. Loop until `ctx` is cancelled; return `nil` on cancellation.

---

### T7 — Telegram handler

`internal/telegram/handler.go`

```go
type Handler struct {
    Bot                *Bot
    EmailRepo          *repo.EmailRepo
    SenderRepo         *repo.SenderRepo
    ClassificationRepo *repo.ClassificationRepo
    Logger             log.Logger
}

func (h *Handler) Handle(ctx context.Context, update gotgbot.Update) error
```

Dispatch logic:
1. If `update.CallbackQuery == nil` → ignore.
2. Always call `AnswerCallback(update.CallbackQuery.Id)` first (dismisses spinner).
3. Parse `update.CallbackQuery.Data` as `"{action}:{emailID}"`.
4. Load email via `EmailRepo.GetByID(ctx, emailID)`.
5. If not found → edit message text to "⚠ Email not found (it may have been deleted)."; return.
6. Dispatch on action:
   - `"handled"` → `handleHandled`
   - `"ignore"`  → `handleIgnore`
   - `"details"` → `handleDetails`
   - unknown → log warning, no edit

```go
func (h *Handler) handleHandled(ctx context.Context, q *gotgbot.CallbackQuery, email *domain.Email) error
func (h *Handler) handleIgnore(ctx context.Context, q *gotgbot.CallbackQuery, email *domain.Email) error
func (h *Handler) handleDetails(ctx context.Context, q *gotgbot.CallbackQuery, email *domain.Email) error
```

---

### T8 — Feedback: sender score

Constants (in `handler.go`):
```go
const (
    feedbackPositiveDelta = 25
    feedbackNegativeDelta = 25
)
```

`handleHandled`:
1. Load sender via `SenderRepo.Get(ctx, email.FromEmail)`. Create if nil.
2. `sender.ImportanceScore = clamp(sender.ImportanceScore + feedbackPositiveDelta, 0, 100)`
3. `SenderRepo.Upsert(ctx, sender)`
4. `EmailRepo.UpdateStatus(ctx, email.ID, domain.StatusHandled)`
5. `EditReplyMarkup` — replace keyboard with `✓ Handled — sender score updated`

`handleIgnore`:
1. Same pattern, delta is negative.
2. `EmailRepo.UpdateStatus(ctx, email.ID, domain.StatusIgnored)`
3. `EditReplyMarkup` — replace keyboard with `✗ Ignored — sender will score lower in future`

`handleDetails`:
1. Load classification from `ClassificationRepo.GetByEmailID(ctx, email.ID)`.
2. `SendReply` with the details format shown above.
3. Keyboard remains on the original message.

---

### T9 — Wire up in main.go

```go
handler := &telegram.Handler{
    Bot:                bot,
    EmailRepo:          emailRepo,
    SenderRepo:         senderRepo,
    ClassificationRepo: classificationRepo,
    Logger:             logger.With("component", "telegram_handler"),
}

poller := &telegram.Poller{
    Bot:          bot,
    Handler:      handler,
    SettingsRepo: settingsRepo,
    Logger:       logger.With("component", "telegram_poller"),
}

g, gCtx := errgroup.WithContext(ctx)
g.Go(func() error { return sched.Start(gCtx) })
g.Go(func() error { return poller.Run(gCtx) })
return g.Wait()
```

---

### T10 — Tests

| Package | What to test |
|---------|--------------|
| `internal/telegram` | `handled` callback → status=handled, sender score +25, keyboard replaced |
| `internal/telegram` | `ignore` callback → status=ignored, sender score −25, keyboard replaced |
| `internal/telegram` | `details` callback → follow-up message contains level, score, reasons |
| `internal/telegram` | callback with unknown email ID → graceful error edit |
| `internal/telegram` | non-callback update → ignored |
| `internal/telegram` | `Poller` offset advances after processing updates |
| `internal/db/repo`  | `SetTelegramMessageID` round-trip |

---

## Dependencies

No new external dependencies.

- `golang.org/x/sync` — already present; used for `errgroup` in `main.go`.
- `gotgbot/v2` — already present; `InlineKeyboardMarkup`, `AnswerCallbackQuery`,
  `EditMessageReplyMarkup`, and `GetUpdates` are all part of the existing library.

---

## Definition of Done

1. `make check` passes.
2. Notification messages arrive with three buttons attached.
3. Tapping ✓ Handled marks the email `handled`, updates sender score, replaces keyboard.
4. Tapping ✗ Ignore marks the email `ignored`, updates sender score, replaces keyboard.
5. Tapping ℹ Details sends a follow-up with level, score, and per-signal breakdown.
6. After feedback, re-running the classifier for the same sender produces a different score.
7. Unknown or stale callback data does not crash the daemon.
8. Daemon survives a Telegram API timeout without restarting.

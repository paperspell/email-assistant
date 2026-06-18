# 007-01-multi-account.md

Status: Implemented
Version: 0.1

# Stage 007 — Multi-Account Support

## Goal

Monitor more than one IMAP account at the same time. Each account polls
independently, keeps its own sync cursor, and can be enabled or disabled
without affecting the others. Telegram notifications identify which account an
email arrived on.

No new external services. No change to the importance filter, LLM layer, or
privacy modes.

---

## What Already Works

The storage layer was designed for this from Stage 1 and needs no structural
change:

- `emails` is keyed by `(account_id, message_uid)` (unique index).
- `sync_state` is keyed by `account_id` (primary key).
- `Scheduler` already takes a single `AccountID` + `Provider` and threads
  `account_id` through every log line and DB write.
- `main.go` already runs components in an `errgroup`, so starting one scheduler
  goroutine per account is a natural extension.
- The DB is opened with `SetMaxOpenConns(1)`, so concurrent schedulers writing
  to SQLite are serialized automatically — no "database is locked" risk.

The gap is entirely in the **configuration layer** (a flat, single-account
key/value store) and the **wiring** (one hard-coded client + scheduler).

---

## What Changes

| Before | After |
|--------|-------|
| One account in flat `account.*` settings keys | N accounts as rows in a new `accounts` table |
| `config.Account` is a single `IMAPAccount` | `config.Accounts` is a slice |
| One IMAP client + one scheduler in `main.go` | One client + scheduler **per enabled account**, each in its own goroutine |
| `email-agent init account` configures the one account | `email-agent account add/list/edit/remove/enable/disable` manage many |
| Notification shows no account context | Notification labels the source account |
| `account_id` = the account's email (implicit) | `account_id` = the account's email (explicit, now the `accounts` table key) |

The importance/LLM/privacy behaviour and the Telegram feedback buttons are
unchanged.

---

## Design Decision — accounts table, not namespaced keys

Two ways to store multiple accounts in the existing model:

1. **Namespaced settings keys** — `account.<id>.imap.host`, etc. Reuses the
   `settings` table but breaks the static `KnownKeys` / `DefaultValues`
   machinery (keys become dynamic) and makes enumeration awkward.
2. **Dedicated `accounts` table** *(chosen)* — one row per account. The schema
   is already `account_id`-centric, `domain.Account` already exists, and
   whole-database Adiantum encryption already protects the stored password, so
   no per-field encryption is needed. Enumeration, add, and remove become plain
   row operations.

**Account identity stays the email address.** Today `AccountID =
cfg.Account.Email`, and `emails` / `sync_state` rows are already keyed by that
value. Using the email as `accounts.id` (a stable natural key for a local
single-user tool) means **existing rows keep matching with zero data
rewrite** — the upgrade only has to copy the current `account.*` settings into
one `accounts` row.

Global settings (`telegram.*`, `llm.*`, `content.*`, `notification.*`,
`log.*`) remain in the `settings` table — they are not per-account.

---

## Data Model

New `accounts` table:

| Column | Description |
|--------|-------------|
| `id` | Account identity = email address (also the `account_id` used elsewhere) |
| `name` | Display label shown in notifications and CLI |
| `email` | Account email (same as `id`) |
| `imap_host` | IMAP server host |
| `imap_port` | IMAP server port (default 993) |
| `imap_username` | IMAP login (defaults to email) |
| `imap_password` | IMAP password (DB is encrypted at rest) |
| `tls` | Use TLS (default true) |
| `poll_interval` | Poll cadence, Go duration string (default `1m`) |
| `auth_type` | Authentication method: `password` (default) — reserved for future `oauth` |
| `enabled` | When false, the account is not polled |
| `created_at` | UTC timestamp |

`sync_state.account_id` and `emails.account_id` continue to hold this `id`.

### OAuth forward-compatibility

This stage only implements `password` auth, but Gmail/Microsoft will later need
OAuth (client id/secret, a persisted refresh token, and rotating short-lived
access tokens). Two cheap choices here keep that path open without a second
migration or a `main.go` rewrite:

- The `auth_type` column distinguishes account credential models, so OAuth rows
  can be added later with no schema change. The Stage 1–6 backfill sets it to
  `password`.
- Provider construction goes through a factory keyed on the account (T8), so a
  Gmail provider becomes one new `case`, not a rewrite of the wiring.

The heavier OAuth pieces (token storage columns, the browser consent flow, and
refreshing the access token on reconnect) are out of scope here — see
[Out of Scope](#out-of-scope-future-stages).

---

## Directory Structure Changes

```
internal/
  domain/
    account.go             # EXPAND: add IMAP connection fields + Enabled, PollInterval
  db/
    migrations/
      007_accounts.sql      # NEW: accounts table + backfill from account.* settings
    repo/
      account_repo.go       # NEW: List / Get / Upsert / Delete / SetEnabled
  config/
    config.go               # Account → Accounts []domain.Account; load via AccountRepo
    keys.go                 # remove account.* keys (now in accounts table)
  scheduler/
    scheduler.go            # add AccountName; pass to notifier
  telegram/
    notifier.go             # SendNewEmail gains accountName
    bot.go                  # formatMessage prepends Account line
cmd/email-agent/
  cmd_account.go            # NEW: account add/list/edit/remove/enable/disable
  cmd_init.go               # first-account setup uses the new account-add flow
  main.go                   # loop accounts → one client + scheduler each
```

---

## Tasks

### T1 — Expand `domain.Account`

`internal/domain/account.go` — make `Account` the full account record:

```go
type Account struct {
    ID           string        // = Email
    Name         string
    Email        string
    Host         string
    Port         int
    Username     string
    Password     string
    TLS          bool
    PollInterval time.Duration
    AuthType     string        // "password" for now; "oauth" reserved for Gmail/Graph
    Enabled      bool
}
```

Default `AuthType` to `"password"` wherever an `Account` is constructed without
an explicit value (repo read of legacy rows, CLI add flow).

This becomes the single source of truth carried by config, the repo, and the
CLI (replacing `config.IMAPAccount`).

---

### T2 — Migration: 007_accounts.sql

Create the table and backfill the existing single account from the flat
settings so current installs upgrade transparently.

```sql
-- +goose Up

CREATE TABLE IF NOT EXISTS accounts (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL,
    imap_host     TEXT NOT NULL,
    imap_port     INTEGER NOT NULL DEFAULT 993,
    imap_username TEXT NOT NULL DEFAULT '',
    imap_password TEXT NOT NULL DEFAULT '',
    tls           INTEGER NOT NULL DEFAULT 1,
    poll_interval TEXT NOT NULL DEFAULT '1m',
    auth_type     TEXT NOT NULL DEFAULT 'password',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Backfill from the Stage 1–6 single-account settings, if present.
-- auth_type/enabled fall back to their column defaults ('password' / 1).
INSERT OR IGNORE INTO accounts
    (id, name, email, imap_host, imap_port, imap_username, imap_password, tls, poll_interval)
SELECT
    (SELECT value FROM settings WHERE key = 'account.email'),
    COALESCE((SELECT value FROM settings WHERE key = 'account.name'), ''),
    (SELECT value FROM settings WHERE key = 'account.email'),
    (SELECT value FROM settings WHERE key = 'account.imap.host'),
    COALESCE((SELECT value FROM settings WHERE key = 'account.imap.port'), '993'),
    COALESCE((SELECT value FROM settings WHERE key = 'account.imap.username'), ''),
    COALESCE((SELECT value FROM settings WHERE key = 'account.imap.password'), ''),
    CASE WHEN (SELECT value FROM settings WHERE key = 'account.imap.tls') = 'false' THEN 0 ELSE 1 END,
    COALESCE((SELECT value FROM settings WHERE key = 'account.poll_interval'), '1m')
WHERE EXISTS (SELECT 1 FROM settings WHERE key = 'account.email');

-- Old account.* settings rows are left in place but no longer read (harmless).

-- +goose Down

DROP TABLE IF EXISTS accounts;
```

Note: `imap_port` and `poll_interval` are stored verbatim from settings (strings)
and parsed in the repo; the defaults above only apply when a key is absent.

---

### T3 — Account repo

`internal/db/repo/account_repo.go`:

```go
type AccountRepo struct { db *sql.DB }

func NewAccountRepo(db *sql.DB) *AccountRepo
func (r *AccountRepo) List(ctx context.Context) ([]domain.Account, error)         // all, ordered by created_at
func (r *AccountRepo) ListEnabled(ctx context.Context) ([]domain.Account, error)  // enabled only
func (r *AccountRepo) Get(ctx context.Context, idOrEmail string) (*domain.Account, error)
func (r *AccountRepo) Upsert(ctx context.Context, a domain.Account) error          // ON CONFLICT(id)
func (r *AccountRepo) Delete(ctx context.Context, id string) error
func (r *AccountRepo) SetEnabled(ctx context.Context, id string, enabled bool) error
```

`poll_interval` parsed with `time.ParseDuration`; `port` with `strconv.Atoi`;
`tls`/`enabled` stored as `0`/`1`. `auth_type` round-trips as-is; reads default
an empty value to `"password"` so legacy rows are handled.

---

### T4 — Config: load accounts from the repo

`internal/config/config.go`:

- Replace `Account IMAPAccount` with `Accounts []domain.Account`.
- Delete the `IMAPAccount` struct and all `account.*` handling in
  `applySettings`, `DefaultValues`, and `KnownKeys`.
- Change `Load` to also take the account repo and populate `Accounts`:

```go
func Load(ctx context.Context, s *repo.SettingsRepo, a *repo.AccountRepo) (*Config, error)
```

- `validate()`:

```go
if len(c.Accounts) == 0 {
    return fmt.Errorf("config: no accounts configured — run 'email-agent account add'")
}
for _, acc := range c.Accounts {
    if acc.Host == "" || acc.Username == "" || acc.Password == "" {
        return fmt.Errorf("config: account %q missing host/username/password", acc.Email)
    }
    if acc.Port == 0 || acc.PollInterval <= 0 {
        return fmt.Errorf("config: account %q has invalid port/poll_interval", acc.Email)
    }
}
```

`account.*` constants are removed from `keys.go`.

---

### T5 — Scheduler: carry the account name

`internal/scheduler/scheduler.go`:

- Add `AccountName string` to `Config`.
- Pass it into the notifier call (see T6).

No other scheduler change — it is already per-account.

---

### T6 — Notification labels the account

`internal/telegram/notifier.go` — extend the interface:

```go
SendNewEmail(ctx context.Context, e domain.Email, c domain.Classification, accountName string) (int64, error)
```

`internal/telegram/bot.go` — `formatMessage` prepends an account line when a
label is present (HTML-escaped, like the other fields):

```
📧 New email
🟠 Importance: important (score 75)

Account: Work (acme@work.com)
From: …
Subject: …
Date: …
```

Update the `scheduler` call site to pass `s.cfg.AccountName`.

---

### T7 — Account management command

`cmd/email-agent/cmd_account.go` — new `account` command group:

```
email-agent account list                     # name, email, host, enabled
email-agent account add                       # interactive add
email-agent account edit   <email|name>       # reconfigure one account
email-agent account remove <email|name>       # delete account
email-agent account enable  <email|name>
email-agent account disable <email|name>
```

- `add` / `edit` reuse the existing `promptText` / `promptPassword` helpers from
  `cmd_init.go` and write through `AccountRepo.Upsert`.
- `remove` deletes the `accounts` row; prompt whether to also delete its
  `emails` / `sync_state` rows (default: keep, so re-adding resumes).
- All open the DB with the keychain hex key, exactly like `cmd_config.go`.

---

### T8 — init + main.go wiring

`cmd/email-agent/cmd_init.go`:

- `runFullInit` configures the **first** account via the new account-add flow
  (writing to `AccountRepo`), then prints
  `Add more accounts with: email-agent account add`.
- `init account` becomes an alias for `account add`.

**Provider factory.** Build the `email.Provider` for an account through a small
factory that switches on `acc.AuthType`, rather than calling
`imapmail.NewClient` inline. This stage only handles `"password"`, but the seam
means a Gmail/Graph OAuth provider becomes one new `case` later, not a `main.go`
change:

```go
func newProvider(acc domain.Account, fetchBody bool, logger log.Logger) (email.Provider, error) {
    switch acc.AuthType {
    case "", "password":
        return imapmail.NewClient(imapmail.Config{
            Host: acc.Host, Port: acc.Port, Username: acc.Username,
            Password: acc.Password, TLS: acc.TLS,
            FetchBody: fetchBody,
            Logger:    logger.With("component", "imap", "account", acc.Email),
        }), nil
    // case "oauth": return gmailoauth.NewClient(...)  // future stage
    default:
        return nil, fmt.Errorf("account %q: unsupported auth_type %q", acc.Email, acc.AuthType)
    }
}
```

`cmd/email-agent/main.go`:

```go
accountRepo := repo.NewAccountRepo(sqlDB)
cfg, err := config.Load(ctx, settingsRepo, accountRepo)
// ...
fetchBody := cfg.Content.Mode == "full_body" || cfg.Content.Mode == "redacted_body"
g, gCtx := errgroup.WithContext(ctx)
for _, acc := range cfg.Accounts {           // ListEnabled already applied in Load
    acc := acc
    provider, err := newProvider(acc, fetchBody, logger)
    if err != nil {
        return err
    }
    sched := scheduler.New(scheduler.Config{
        AccountID:    acc.ID,
        AccountName:  acc.Name,
        PollInterval: acc.PollInterval,
        Provider:     provider,
        // ...all shared repos / filter / llmProvider / bot as today...
    })
    g.Go(func() error { return sched.Start(gCtx) })
}
g.Go(func() error { return poller.Run(gCtx) })   // single shared Telegram poller
```

The Telegram `bot`, `Handler`, and `Poller` stay single instances — feedback
callbacks look up emails by global ULID, so they already work across accounts.

---

### T9 — Tests

| Package | What to test |
|---------|--------------|
| `internal/db/repo` | `AccountRepo` round-trip: Upsert/Get/List/ListEnabled/Delete/SetEnabled |
| `internal/db` | Migration 007 backfills one account from existing `account.*` settings; no-op when none present |
| `internal/config` | `Load` returns all enabled accounts; `validate` rejects zero accounts and accounts missing required fields |
| `internal/telegram` | `formatMessage` includes the `Account:` line when name set; omits/handles empty name; account name is HTML-escaped |
| `internal/scheduler` | Two schedulers with distinct `AccountID` maintain independent `sync_state` cursors (extend existing `mockProvider` test) |

---

### T10 — Docs

- `docs/db-schema.md` — add the `accounts` table; bump migration version to 007.
- `docs/settings.md` — note that account fields moved from `account.*` settings
  to `account` subcommands; keep global settings section.
- `docs/architecture.md` — update to "one scheduler goroutine per enabled
  account".
- `config.example.yaml` — replace the single `account:` block with guidance to
  use `email-agent account add`.
- `general-implementation-plan.md` — mark Stage 7 status.

---

## Dependencies

No new external dependencies.

---

## Recommended Task Order

```
T1  → domain.Account expanded
T2  → migration 007_accounts.sql (+ backfill)
T3  → account_repo.go
T4  → config: Accounts slice, load via AccountRepo, validate
T5  → scheduler: AccountName
T6  → notifier/bot: Account line in notification
T7  → cmd_account.go (add/list/edit/remove/enable/disable)
T8  → cmd_init + main.go per-account wiring
T9  → tests
T10 → docs
```

---

## Definition of Done

1. `make check` passes.
2. An existing single-account install upgrades automatically: after migration,
   `email-agent account list` shows the previously configured account and the
   daemon keeps polling it with its existing sync cursor (no re-notification of
   old mail).
3. `email-agent account add` adds a second account; the daemon polls **both**
   concurrently, each with independent `sync_state`.
4. `email-agent account disable <email>` stops polling that account on next
   daemon start without affecting the others; `enable` resumes it.
5. `email-agent account remove <email>` removes the account from the poll set.
6. Each Telegram notification shows which account the email arrived on.
7. Telegram feedback buttons (Handled / Ignore / Details) work for emails from
   any account.

---

## Out of Scope (future stages)

- Per-account notification routing (separate Telegram chats per account).
- Per-account importance thresholds or LLM/privacy settings (currently global).
- Gmail API / Microsoft Graph backends — these implement the same
  `email.Provider` interface and slot into the per-account loop (via the
  `newProvider` factory) unchanged when added.
- **OAuth authentication.** This stage stores the `auth_type` discriminator and
  routes provider construction through a factory so OAuth can be added without a
  schema change or wiring rewrite, but the OAuth mechanics themselves are
  deferred to Stage 8 — Gmail OAuth Backend (`docs/stages/008-01-gmail-oauth.md`):
  token storage columns (client id/secret, refresh token, access token + expiry),
  the browser consent flow in `account add`, and refreshing the access token on
  reconnect (which builds on Plan A — IMAP Connection Resilience).
- Hot reload of accounts without a daemon restart.
```
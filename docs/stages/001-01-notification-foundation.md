# 001-notification-foundation.md

Status: Draft  
Version: 0.1

# Stage 1 — Notification Foundation

## Goal

Produce a single Go binary that monitors one IMAP inbox, detects new emails, and sends a Telegram notification for every new email.

At the end of this stage the user can:
- Run `email-agent run` locally with a YAML config
- Point it at one IMAP account and a Telegram bot
- Receive a Telegram message for every new email detected in the inbox

No importance filtering. No classification. Every new email triggers a notification.

---

## Deliverables

- Single Go binary `email-agent`
- SQLite database for local state (sync state, email metadata, notification records)
- IMAP polling loop for one account
- Telegram send-only notifier
- CLI commands: `run`, `version`
- YAML config file with validation on startup
- Makefile with standard targets
- golangci-lint config
- GitHub Actions pipeline: lint + unit tests + migration tests
- `AGENTS.md` — general project description and doc index
- `README.md` — GitHub project description
- `config.example.yaml` — annotated reference config
- `.mockery.yaml` — mock generation config

---

## Directory Structure

```
email-assistant/
├── cmd/
│   └── email-agent/
│       └── main.go
├── internal/
│   ├── pkg/                        # copied and adapted from go-sdk
│   │   ├── errx/
│   │   │   ├── wrap.go
│   │   │   ├── trace.go
│   │   │   └── wrap_test.go
│   │   ├── log/
│   │   │   ├── log.go              # Logger interface, IntoContext, FromContext
│   │   │   ├── logger.go           # Slogger implementation (no Sentry)
│   │   │   └── noop.go
│   │   ├── idx/
│   │   │   ├── ids.go              # GenerateID() → ULID string
│   │   │   └── hash.go             # Hash(string) → base32 SHA-256
│   │   ├── workers/
│   │   │   ├── worker.go           # Worker interface, BlockingWorker, UnlimitedWorker
│   │   │   └── tryable.go
│   │   ├── slicex/
│   │   │   └── slice.go
│   │   ├── timex/
│   │   │   └── now_utc.go
│   │   ├── flow/
│   │   │   └── must.go
│   │   └── ioutil/
│   │       └── close.go
│   ├── domain/
│   │   ├── email.go
│   │   ├── account.go
│   │   └── sync_state.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── db/
│   │   ├── db.go                   # open SQLite, run migrations
│   │   └── repo/
│   │       ├── email_repo.go
│   │       ├── email_repo_test.go
│   │       └── sync_state_repo.go
│   ├── email/
│   │   ├── provider.go             # EmailProvider interface
│   │   └── imap/
│   │       ├── client.go
│   │       └── client_test.go
│   ├── telegram/
│   │   ├── notifier.go             # Notifier interface
│   │   └── bot.go                  # gotgbot implementation
│   └── scheduler/
│       └── scheduler.go
├── migrations/
│   └── 001_initial.sql
├── docs/
│   ├── architecture.md
│   ├── coding-guidelines.md
│   ├── dependencies.md
│   ├── general-implementation-plan.md
│   ├── importance-filter.md
│   └── stages/
│       └── 001-notification-foundation.md
├── .github/
│   └── workflows/
│       └── test.yml
├── .gitignore
├── .golangci.yaml
├── .mockery.yaml
├── AGENTS.md
├── Makefile
├── README.md
├── config.example.yaml
├── go.mod
└── go.sum
```

---

## Tasks

### T1 — Internal Utility Packages

Copy and adapt from go-sdk into `internal/pkg/`. Strip all references to `github.com/paperspell/go-sdk` and replace with the local module path. Remove Sentry from the logger.

**T1.1 — slicex, timex, flow, ioutil**

These are prerequisites for errx and idx. No external dependencies beyond the standard library.

- `slicex/slice.go` — generic slice helpers (`Any`, `Map`, etc.)
- `timex/now_utc.go` — `NowUTC() time.Time`
- `flow/must.go` — `Must(n int, err error)` panic helper
- `ioutil/close.go` — safe `io.Closer` wrapper

**T1.2 — errx**

Error wrapping with context, structured key-values, and stacktrace capture.

Public API:
```go
func New(ctx context.Context, message string, kvs ...any) error
func Wrap(ctx context.Context, err error, message string, kvs ...any) error
func Stacktrace(ignoreLevels int, ignorePrefixes ...string) []TraceEntry
```

External dependency: `github.com/pkg/errors`

Unit tests: wrap nil, wrap non-nil, key-values propagation, context propagation, stacktrace capture.

**T1.3 — log**

Logger interface and Slogger implementation. No Sentry. JSON handler in production, tint handler in dev mode.

Logger interface (unchanged from go-sdk):
```go
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(err error, args ...any)
    Error(err error, args ...any)
    With(args ...any) Logger
    InfoEnabled() bool
    ErrorEnabled() bool
    WarnEnabled() bool
    DebugEnabled() bool
}
```

Files: `log.go` (interface + context helpers), `logger.go` (Slogger), `noop.go` (Noop).

External dependency: `github.com/lmittmann/tint`

**T1.4 — idx**

ULID-based ID generation and deterministic hashing.

```go
func GenerateID() string          // ULID
func Hash(data string) string     // base32(sha256[:16])
```

External dependency: `github.com/oklog/ulid/v2`

Unit tests: GenerateID is non-empty and unique across calls, Hash is deterministic.

**T1.5 — workers**

Worker interface and implementations for managing goroutine lifecycles.

```go
type Worker interface {
    Do(ctx context.Context, fn func(context.Context))
    DoTry(ctx context.Context, fn func(context.Context)) bool
    Wait()
}
func NewSequentialWorker() Worker   // blocking, single goroutine
func NewUnlimitedWorker() Worker    // one goroutine per Do call
```

No external dependencies.

---

### T2 — Domain Models

`internal/domain/` — transport-agnostic types. No imports from IMAP, Telegram, SQLite, or any SDK.

**email.go**

```go
type EmailStatus string

const (
    StatusNew      EmailStatus = "new"
    StatusNotified EmailStatus = "notified"
    StatusIgnored  EmailStatus = "ignored"
)

type Email struct {
    ID          string
    AccountID   string
    MessageUID  uint32
    Subject     string
    FromEmail   string
    FromName    string
    Date        time.Time
    Status      EmailStatus
    ReceivedAt  time.Time
}
```

**account.go**

```go
type Account struct {
    ID       string
    Name     string
    Email    string
}
```

**sync_state.go**

```go
type SyncState struct {
    AccountID  string
    LastUID    uint32
    SyncedAt   time.Time
}
```

---

### T3 — Config

`internal/config/config.go` — load from YAML file, with env overrides via `caarlos0/env`.

```go
type Config struct {
    LogLevel string `yaml:"log_level" env:"LOG_LEVEL" envDefault:"info"`
    DevMode  bool   `yaml:"dev_mode"  env:"DEV_MODE"`

    DB struct {
        Path string `yaml:"path" env:"DB_PATH" envDefault:"email-agent.db"`
    } `yaml:"db"`

    Account IMAPAccount `yaml:"account"`

    Telegram TelegramConfig `yaml:"telegram"`
}

type IMAPAccount struct {
    Name     string `yaml:"name"`
    Email    string `yaml:"email"`
    Host     string `yaml:"host"`
    Port     int    `yaml:"port" envDefault:"993"`
    Username string `yaml:"username"`
    Password string `yaml:"password" env:"IMAP_PASSWORD"`
    TLS      bool   `yaml:"tls" envDefault:"true"`
    PollInterval time.Duration `yaml:"poll_interval" envDefault:"1m"`
}

type TelegramConfig struct {
    BotToken string `yaml:"bot_token" env:"TELEGRAM_BOT_TOKEN"`
    ChatID   int64  `yaml:"chat_id"`
}
```

Loading sequence:
1. Read YAML from config file path (CLI flag `--config`, default `config.yaml`)
2. Override with env vars
3. Validate: missing required fields → return error immediately, binary exits

Unit tests: valid config loads, missing required fields return error, env overrides YAML value.

---

### T4 — Database

**T4.1 — Migration: 001_initial.sql**

```sql
CREATE TABLE IF NOT EXISTS emails (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    message_uid INTEGER NOT NULL,
    subject     TEXT NOT NULL,
    from_email  TEXT NOT NULL,
    from_name   TEXT NOT NULL,
    date        DATETIME NOT NULL,
    status      TEXT NOT NULL DEFAULT 'new',
    received_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_emails_account_uid
    ON emails (account_id, message_uid);

CREATE TABLE IF NOT EXISTS sync_state (
    account_id  TEXT PRIMARY KEY,
    last_uid    INTEGER NOT NULL DEFAULT 0,
    synced_at   DATETIME NOT NULL
);
```

**T4.2 — db.go**

Open SQLite with `modernc.org/sqlite`. Run goose migrations from the `migrations/` directory on startup. Return `*sql.DB`.

```go
func Open(path string) (*sql.DB, error)
func Migrate(db *sql.DB, migrationsDir string) error
```

**T4.3 — EmailRepo**

```go
type EmailRepo struct { db *sql.DB }

func (r *EmailRepo) Upsert(ctx context.Context, e domain.Email) error
func (r *EmailRepo) GetByAccountAndUID(ctx context.Context, accountID string, uid uint32) (*domain.Email, error)
func (r *EmailRepo) UpdateStatus(ctx context.Context, id string, status domain.EmailStatus) error
```

**T4.4 — SyncStateRepo**

```go
type SyncStateRepo struct { db *sql.DB }

func (r *SyncStateRepo) Get(ctx context.Context, accountID string) (*domain.SyncState, error)
func (r *SyncStateRepo) Upsert(ctx context.Context, s domain.SyncState) error
```

Integration tests (build tag `integration`): run against a real in-memory SQLite instance (`:memory:`), test upsert, get, update, and constraint violations.

---

### T5 — Email Provider

**T5.1 — Interface**

`internal/email/provider.go`

```go
type Message struct {
    UID       uint32
    Subject   string
    FromEmail string
    FromName  string
    Date      time.Time
}

type Provider interface {
    // Connect establishes and authenticates the connection.
    Connect(ctx context.Context) error
    // FetchSince returns messages with UID greater than lastUID.
    FetchSince(ctx context.Context, lastUID uint32) ([]Message, error)
    // Close closes the connection.
    Close() error
}
```

**T5.2 — IMAP Implementation**

`internal/email/imap/client.go` using `github.com/emersion/go-imap/v2`.

```go
type Config struct {
    Host     string
    Port     int
    Username string
    Password string
    TLS      bool
}

type Client struct { /* ... */ }

func NewClient(cfg Config) *Client
func (c *Client) Connect(ctx context.Context) error
func (c *Client) FetchSince(ctx context.Context, lastUID uint32) ([]email.Message, error)
func (c *Client) Close() error
```

Behaviour:
- Select `INBOX` on connect
- Use `UID SEARCH UID lastUID+1:*` to find new messages
- Fetch `ENVELOPE` only (no body fetch in Stage 1)
- Return messages sorted by UID ascending

Unit tests (build tag `manual`, skip in CI): connect to a real IMAP server, fetch messages. Use build tag to exclude from automated pipeline.

---

### T6 — Telegram Notifier

**T6.1 — Interface**

`internal/telegram/notifier.go`

```go
type Notifier interface {
    SendNewEmail(ctx context.Context, e domain.Email) error
}
```

**T6.2 — Bot Implementation**

`internal/telegram/bot.go` using `github.com/PaulSonOfLars/gotgbot/v2` in polling-free send-only mode (direct HTTP calls via `gotgbot.Bot`).

Notification message format:
```
📬 New email

From: {from_name} <{from_email}>
Subject: {subject}
Date: {date}
```

No webhook. No command handling. Polling mode not started.

Unit tests: mock `Notifier` interface, verify `SendNewEmail` is called with correct email data.

---

### T7 — Scheduler

`internal/scheduler/scheduler.go`

Coordinates the polling loop for one account. Uses `workers.UnlimitedWorker` for the poll goroutine. Respects context cancellation for graceful shutdown.

```go
type Config struct {
    Account      config.IMAPAccount
    PollInterval time.Duration
    EmailRepo    *repo.EmailRepo
    SyncRepo     *repo.SyncStateRepo
    Provider     email.Provider
    Notifier     telegram.Notifier
    Logger       log.Logger
}

type Scheduler struct { /* ... */ }

func New(cfg Config) *Scheduler
func (s *Scheduler) Start(ctx context.Context) error   // blocks until ctx cancelled
func (s *Scheduler) Stop()
```

Poll loop behaviour:
1. Load `SyncState` for account from DB (last processed UID)
2. Call `Provider.FetchSince(ctx, lastUID)`
3. For each new message:
   a. Build `domain.Email`, assign `idx.GenerateID()`
   b. `EmailRepo.Upsert`
   c. `Notifier.SendNewEmail`
   d. `EmailRepo.UpdateStatus(id, StatusNotified)`
4. Update `SyncState` with the highest UID seen
5. Sleep for `PollInterval`, repeat

On error: log with `logger.Error`, apply exponential backoff (`cenkalti/backoff/v4`), do not crash.

Unit tests: mock Provider and Notifier, assert correct sequence of calls, assert SyncState is updated after a successful poll.

---

### T8 — CLI Entrypoint

`cmd/email-agent/main.go` using `github.com/spf13/cobra`.

**Commands:**

`run` — start the daemon:
1. Load config
2. Open and migrate DB
3. Build provider, notifier, repos, scheduler
4. Inject logger into context
5. Start scheduler
6. Block on `SIGINT`/`SIGTERM` → cancel context → call `scheduler.Stop()` → exit 0

`version` — print version and build info, exit 0.

CLI flags on `run`:
- `--config` / `-c` — path to config file (default: `config.yaml`)

---

### T9 — Supporting Files

**T9.1 — Makefile**

Targets:

```makefile
build          # go build -o bin/email-agent -trimpath ./cmd/email-agent
run            # load .env and run ./bin/email-agent run
test           # go test -race -cover ./...
test-migrations# run migration tests
lint           # golangci-lint run -v
lint-fix       # golangci-lint run -v --fix
fmt            # go fmt ./...
generate       # go generate ./...
mock           # regenerate mocks with mockery
tidy           # go mod tidy
check          # lint-fix + test + test-migrations
setup          # brew install prerequisites
help           # print available targets
```

**T9.2 — .golangci.yaml**

Same linters and formatters as document-service. Update the `wrapcheck.ignore-package-globs` to:

```yaml
ignore-package-globs:
  - github.com/paperspell/*
```

Add `gci` sections:
```yaml
sections:
  - standard
  - default
  - prefix(github.com/paperspell/email-assistant)
  - alias
  - blank
```

**T9.3 — .mockery.yaml**

```yaml
quiet: false
disable-version-string: true
with-expecter: true
packages:
  github.com/paperspell/email-assistant/internal/email:
    interfaces:
      Provider:
  github.com/paperspell/email-assistant/internal/telegram:
    interfaces:
      Notifier:
  github.com/paperspell/email-assistant/internal/pkg/log:
    interfaces:
      Logger:
```

**T9.4 — config.example.yaml**

Annotated reference configuration with all fields, their defaults, and descriptions.

**T9.5 — GitHub Actions: .github/workflows/test.yml**

Three jobs (no deploy, no integration tests with Docker):

- `golangci-lint` — install golangci-lint v2.10.1, run `golangci-lint run`
- `unit-test` — run `make test`
- `migration-test` — run `make test-migrations`

Trigger on: push and PR to `main`.

Private repo access via `GH_PAT` secret (needed if go-sdk is ever referenced as an external module).

**T9.6 — AGENTS.md**

General project description, architectural guardrails, and an index of all docs in `docs/` and `docs/stages/`.

**T9.7 — README.md**

GitHub-facing description: what the project is, how to install, how to configure, how to run. Includes a minimal `config.example.yaml` snippet and the `make run` command.

---

## Dependencies to Add

Run `go get` for:

```
github.com/spf13/cobra@v1.10.2
github.com/caarlos0/env/v10@v10.0.0
gopkg.in/yaml.v3@v3.0.1
modernc.org/sqlite@v1.52.0
github.com/pressly/goose/v3@v3.27.1
github.com/emersion/go-imap/v2@v2.0.0-beta.8
github.com/PaulSonOfLars/gotgbot/v2@v2.0.0-rc.34
github.com/lmittmann/tint@v1.1.3
github.com/pkg/errors@v0.9.1
github.com/oklog/ulid/v2@v2.1.1
github.com/cenkalti/backoff/v4@v4.3.0
github.com/stretchr/testify@v1.11.1
github.com/elliotchance/pie/v2@v2.9.1
github.com/google/uuid@v1.6.0
golang.org/x/exp@latest
golang.org/x/sync@latest
```

---

## Test Summary

| Package | Test type | Tag | Runs in CI |
|---------|-----------|-----|------------|
| `internal/pkg/errx` | Unit | — | Yes |
| `internal/pkg/idx` | Unit | — | Yes |
| `internal/config` | Unit | — | Yes |
| `internal/db/repo` | Integration | `integration` | Yes (in-memory SQLite) |
| `internal/scheduler` | Unit (mocks) | — | Yes |
| `internal/email/imap` | Manual | `manual` | No |
| `internal/telegram` | Unit (mocks) | — | Yes |
| `migrations/` | Migration | `migration` | Yes (separate job) |

---

## Definition of Done for Stage 1

1. `make build` produces a working binary
2. `make test` passes with no failures
3. `make lint` passes with no errors
4. `make test-migrations` passes
5. Running `email-agent run --config config.yaml` starts polling and sends Telegram notifications
6. Graceful shutdown on `SIGINT` (no goroutine leaks)
7. All required fields missing from config → binary exits with a clear error message
8. GitHub Actions pipeline is green on `main`

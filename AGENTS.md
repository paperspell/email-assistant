# AGENTS.md

## Project

Email Agent is a local-first Go daemon that monitors email accounts, detects new incoming emails, and sends Telegram notifications.

The system runs entirely on the user's machine as a single Go binary. No cloud backend is required.

## Architectural Guardrails

- **Local First** — all state stored in SQLite, no backend service
- **Single Binary** — one Go binary with embedded migrations
- **Privacy Focused** — email body not stored by default; LLM usage logged locally
- **Provider Agnostic** — email, LLM, and notification providers hidden behind interfaces
- **Explainable** — every classification has a human-readable reason
- **User Controlled** — no automatic email sending without explicit user approval

## Repository Structure

```
cmd/email-agent/     CLI entrypoint (cobra)
internal/
  pkg/               Internal utility packages (copied from go-sdk)
    errx/            Error wrapping with context, key-values, stacktrace
    log/             Logger interface and slog-based implementation
    idx/             ULID ID generation and deterministic hashing
    workers/         Goroutine worker pool abstractions
    slicex/          Generic slice helpers
    timex/           UTC time helper
    flow/            Must() panic helper
    ioutil/          Safe io.Closer wrapper
  domain/            Shared domain models (transport-agnostic)
  config/            Config loading from settings table
  db/                SQLite open, migrations, repositories
  email/             EmailProvider interface and IMAP implementation
  telegram/          Telegram send-only notifier
  scheduler/         Polling loop coordinator
migrations/          (see internal/db/migrations/ for embedded SQL files)
docs/                Architecture, guidelines, plans
  stages/            Per-stage implementation plans
```

## Documentation Index

| Document | Description |
|----------|-------------|
| [docs/architecture.md](docs/architecture.md) | C4 diagrams, package map, component responsibilities |
| [docs/coding-guidelines.md](docs/coding-guidelines.md) | Implementation standards and constraints |
| [docs/general-implementation-plan.md](docs/general-implementation-plan.md) | 10-stage product roadmap |
| [docs/importance-filter.md](docs/importance-filter.md) | Classification pipeline design |
| [docs/dependencies.md](docs/dependencies.md) | Preferred dependency versions |
| [docs/db-schema.md](docs/db-schema.md) | Mermaid ERD of the current SQLite schema — **update after every new migration** |
| [docs/stages/001-01-notification-foundation.md](docs/stages/001-01-notification-foundation.md) | Stage 1 detailed implementation plan |
| [docs/stages/001-02-encrypted-config.md](docs/stages/001-02-encrypted-config.md) | Stage 1-02 encrypted SQLite + config in DB plan |
| [docs/stages/002-01-importance-detection.md](docs/stages/002-01-importance-detection.md) | Stage 2-01 importance filter implementation plan |
| [docs/stages/008-03-imap-reconnect.md](docs/stages/008-03-imap-reconnect.md) | Stage 8-03 IMAP auto-reconnect + OAuth re-auth Telegram alert (implemented) |
| [docs/stages/008-04-token-hot-reload.md](docs/stages/008-04-token-hot-reload.md) | Stage 8-04 hot reload — OAuth token reload without restart (phase 1 done; phase 2 draft) |
| [docs/stages/008-05-fast-first-run.md](docs/stages/008-05-fast-first-run.md) | Stage 8-05 fast first run — baseline UID without downloading existing mail (implemented) |
| [docs/stages/008-06-first-run-backfill.md](docs/stages/008-06-first-run-backfill.md) | Stage 8-06 first-run backfill — process recent unread on first run, per-account opt-in (implemented) |

## Development

```bash
make setup       # install prerequisites (macOS)
make build       # build the binary
make test        # run unit tests
make test-migrations  # run migration tests
make lint        # run golangci-lint
make check       # lint + test + test-migrations
```

## Running Locally

```bash
# One-time setup — creates encrypted database at ~/.email-agent/email-agent.db
email-agent init

# Start the daemon
email-agent run

# Update a setting after init
email-agent config set account.poll_interval 2m
```

## Code Review Rules

These rules drive automated code review (Codex) on pull requests. Flag a change only when it
breaks one of the rules below or is a genuine correctness or privacy regression — style points
that `golangci-lint` already enforces are out of scope.

### This repository is public

- Real credentials never belong in the tree: IMAP passwords, OAuth client secrets, API keys, bot
  tokens, or a populated `~/.email-agent` database, including inside tests, fixtures, and docs.
  `config.example.yaml` carries placeholders only.
- Committed binaries are a blind spot — secret scanners skip them — so build output stays ignored
  rather than checked in.

### Privacy

- Email bodies and attachments are not stored by default and are never sent to an LLM provider
  beyond what the classification feature explicitly needs.
- Bodies, attachments, API keys, OAuth tokens, and passwords must not reach logs, not even inside
  wrapped error messages. Subjects and addresses count as user content too — log identifiers.
- Every LLM interaction is logged locally, and every classification carries a human-readable
  reason. A code path that decides something without recording why is incomplete.
- Nothing is sent from the user's mailbox without explicit user approval.

### Layering

- Domain models stay independent of IMAP, Telegram, and LLM specifics. Provider details leaking
  into `internal/domain/` or `internal/scheduler/` are a defect.
- A new provider is a new implementation behind the existing interface, not another branch in the
  scheduler.

### Errors, logging, time, IDs

- Wrap errors with `errx.Wrap(ctx, err, "message", "key", value)`; a bare
  `fmt.Errorf("...: %w", err)` loses the context keys. An error that is neither returned nor
  logged is a defect.
- `panic` is acceptable only for unrecoverable startup failures (`flow.Must`); never in the poll
  loop.
- Timestamps come from `timex.NowUTC()`, IDs from `idx`. `time.Now()` in new code is wrong.

### Daemon behaviour

- Every goroutine must exit on context cancellation; a poll or reconnect loop without a backoff
  spins against the provider and gets the account throttled.
- Retry only what is safe to repeat. Retrying a non-idempotent action is a defect, not a
  robustness feature.

### Database and configuration

- Schema changes go through a new embedded migration and are additive. Editing a migration that
  has already been applied corrupts existing installations — the daemon runs on real machines that
  will not be reinstalled.
- After a migration, `docs/db-schema.md` must be updated; migration tests live behind
  `-tags=migration`.
- A new setting needs a documented default and startup validation that fails fast when a required
  value is missing.

### Packaging

- Do not add `MemoryDenyWriteExecute` to the systemd unit. SQLite runs through a wasm runtime that
  needs writable-executable memory, so that hardening flag stops the daemon from starting.

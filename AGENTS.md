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

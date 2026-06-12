# dependencies.md

Status: Draft  
Version: 0.1

# Preferred Dependencies

## Go Version

```
go 1.26+
```

---

## CLI

| Purpose | Import Path | Version |
|---------|-------------|---------|
| CLI framework | `github.com/spf13/cobra` | v1.10.2 |

---

## Configuration

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Config from environment variables | `github.com/caarlos0/env/v10` | v10.0.0 |
| YAML config file parsing | `gopkg.in/yaml.v3` | v3.0.1 |

---

## Logging

Use Go standard library `log/slog` as the logger interface.

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Colorized terminal slog handler | `github.com/lmittmann/tint` | v1.1.3 |

---

## Email

| Purpose | Import Path | Version |
|---------|-------------|---------|
| IMAP client | `github.com/emersion/go-imap/v2` | v2.0.0-beta.8 |

> **Note:** go-imap v2 has no stable release yet. v2.0.0-beta.8 is the latest and is the actively maintained version. v1 (`github.com/emersion/go-imap v1.6.0`) is the last fully stable alternative if beta is not acceptable.

---

## Storage

| Purpose | Import Path | Version |
|---------|-------------|---------|
| SQLite driver (pure Go) | `modernc.org/sqlite` | v1.52.0 |
| Database migrations | `github.com/pressly/goose/v3` | v3.27.1 |

> **Note:** `modernc.org/sqlite` supports FTS5 (compiled in). It does NOT support loadable C extensions such as `sqlite-vec` or `sqlite-vss`. If vector search in SQLite becomes a requirement, switch to `github.com/mattn/go-sqlite3` (CGO) which can load external extensions.

---

## LLM Providers

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Anthropic Claude SDK | `github.com/anthropics/anthropic-sdk-go` | v1.50.1 |
| OpenAI SDK | `github.com/openai/openai-go/v3` | v3.26.0 |

---

## Telegram

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Telegram Bot API client | `github.com/PaulSonOfLars/gotgbot/v2` | v2.0.0-rc.34 |

---

## Classification and Features

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Language detection | `github.com/pemistahl/lingua-go` | v1.4.0 |

---

## Utilities

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Slice and map helpers | `github.com/elliotchance/pie/v2` | v2.9.1 |
| UUID generation | `github.com/google/uuid` | v1.6.0 |
| Retry with exponential backoff | `github.com/cenkalti/backoff/v4` | v4.3.0 |
| Concurrency helpers | `golang.org/x/sync` | latest |

---

## Testing

| Purpose | Import Path | Version |
|---------|-------------|---------|
| Assertions and mocks | `github.com/stretchr/testify` | v1.11.1 |

Use `testify/assert` for non-fatal assertions and `testify/require` for assertions that should stop the test immediately on failure.

---

## Standard Library Packages (no external dependency)

| Purpose | Package |
|---------|---------|
| Structured logging interface | `log/slog` |
| HTTP client | `net/http` |
| Context and cancellation | `context` |
| Concurrency | `sync` |
| Time | `time` |

---

## Out of Scope

The following are intentionally excluded:

- Redis or any networked cache
- Message queues
- Web frameworks (no HTTP server in MVP)
- Cloud SDKs (GCP, AWS, Firebase)
- gRPC
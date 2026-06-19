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

Configuration is stored in the encrypted SQLite database. No config file or env-parsing library is needed.

| Purpose | Import Path | Version |
|---------|-------------|---------|
| OS keychain (encryption key storage) | `github.com/zalando/go-keyring` | v0.2.8 |
| Terminal password input | `golang.org/x/term` | v0.44.0 |

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
| SASL mechanisms (XOAUTH2 client) | `github.com/emersion/go-sasl` | (via go-imap) |
| OAuth 2.0 flow + token refresh (Gmail) | `golang.org/x/oauth2` | v0.30.0 |

> **Note:** go-imap v2 has no stable release yet. v2.0.0-beta.8 is the latest and is the actively maintained version. v1 (`github.com/emersion/go-imap v1.6.0`) is the last fully stable alternative if beta is not acceptable.
>
> The Google OAuth endpoint is declared inline (in `internal/auth/oauth`) rather
> than importing `golang.org/x/oauth2/google`, to avoid pulling in the heavy
> `cloud.google.com/go` dependency.

---

## Storage

| Purpose | Import Path | Version |
|---------|-------------|---------|
| SQLite driver (pure Go, WASM-based) | `github.com/ncruces/go-sqlite3` | v0.35.0 |
| Adiantum full-database encryption VFS | `github.com/ncruces/go-sqlite3/vfs/adiantum` | v0.35.0 |
| Database migrations | `github.com/pressly/goose/v3` | v3.27.1 |

> **Note:** `ncruces/go-sqlite3` supports FTS5. It does NOT support loadable C extensions such as `sqlite-vec`. No CGO required.

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
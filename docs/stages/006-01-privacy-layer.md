# 006-01-privacy-layer.md

Status: Draft
Version: 0.1

# Stage 006 — Privacy Layer

## Goal

Reduce the information that leaves the user's machine when LLM classification
is active. Introduces a third content mode (`redacted_body`) that strips
recognisable sensitive patterns from the email body before it is sent to the
LLM provider. All external LLM calls are recorded in a local audit log.

No changes to Telegram notifications. No new external services.

---

## What Changes

| Before | After |
|--------|-------|
| `content.mode` has 2 values | 3 values: `headers_only`, `redacted_body`, `full_body` |
| Body sent verbatim when `full_body` | `redacted_body` replaces PII with placeholders before sending |
| No record of what was sent to LLM | Every LLM call logged to `llm_audit_log` table |

---

## Content Modes (updated)

| Mode | What the LLM receives |
|------|-----------------------|
| `headers_only` | From, Subject, Date, thread/newsletter signals only |
| `redacted_body` | Full body with PII replaced by `[EMAIL]`, `[PHONE]`, `[FINANCIAL]`, `[ADDRESS]`, `[TOKEN]` |
| `full_body` | Complete body, unmodified |

Default remains `headers_only`.

---

## Redaction Patterns

Applied in order. Each match is replaced with its placeholder tag.

| Pattern | Placeholder | Examples matched |
|---------|-------------|-----------------|
| Email address | `[EMAIL]` | `john@company.com` |
| Phone number | `[PHONE]` | `+1 (555) 123-4567`, `+48 600 123 456` |
| IBAN | `[FINANCIAL]` | `DE89 3704 0044 0532 0130 00` |
| Credit / debit card | `[FINANCIAL]` | `4111 1111 1111 1111` |
| UUID / API token | `[TOKEN]` | `550e8400-e29b-41d4-a716-446655440000`, long alphanumeric strings ≥ 20 chars |
| OTP / verification code | `[TOKEN]` | digits in context: `code: 123456`, `OTP: 7891` |
| Street address | `[ADDRESS]` | `123 Main St`, `ul. Marszałkowska 10` |

Redaction is regex-based (no extra LLM call, no network request). Patterns are
conservative — false positives (over-redacting) are acceptable; false negatives
(missing real PII) are not.

---

## Audit Log

Every outbound LLM call is recorded in a new `llm_audit_log` table.

Stored per call:

| Field | Description |
|-------|-------------|
| `id` | ULID |
| `email_id` | References `emails.id` |
| `provider` | `"anthropic"` or `"openai"` |
| `model` | Model name used |
| `content_mode` | `"headers_only"`, `"redacted_body"`, or `"full_body"` |
| `bytes_sent` | Size of the user message sent (after any redaction) |
| `created_at` | UTC timestamp |

No prompt content is stored — only metadata about the call.

---

## Directory Structure Changes

```
internal/
  privacy/
    redactor.go          # NEW: Redact(text string) string
    redactor_test.go     # NEW
  db/
    migrations/
      006_audit_log.sql  # NEW
    repo/
      audit_repo.go      # NEW: AuditRepo.Save
  scheduler/
    scheduler.go         # apply redaction; write audit log
  config/
    config.go            # content.mode: add redacted_body to docs/validation
  cmd/email-agent/
    cmd_init.go          # update llm init prompt to offer redacted_body
```

---

## Tasks

### T1 — Privacy redactor

`internal/privacy/redactor.go`

```go
// Redact replaces recognised sensitive patterns in text with placeholder tags.
func Redact(text string) string
```

Patterns (compiled once at package init via `sync.Once`):

```go
var patterns = []struct {
    re          *regexp.Regexp
    placeholder string
}{
    {emailRe,       "[EMAIL]"},
    {phoneRe,       "[PHONE]"},
    {ibanRe,        "[FINANCIAL]"},
    {creditCardRe,  "[FINANCIAL]"},
    {uuidRe,        "[TOKEN]"},
    {longTokenRe,   "[TOKEN]"},   // alphanumeric ≥ 20 chars
    {otpRe,         "[TOKEN]"},   // digits in OTP/code context
    {addressRe,     "[ADDRESS]"},
}
```

Apply patterns sequentially via `re.ReplaceAllString`. Return the redacted string.

Key regexes:

```
email      : [a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}
phone      : (\+?[\d][\d\s\-().]{6,14}[\d])
iban       : [A-Z]{2}\d{2}[A-Z0-9]{4,30}
credit card: \b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b
uuid       : [0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}
long token : [A-Za-z0-9_\-]{20,}
otp        : (?i)(code|otp|token|pin|passcode)[^0-9]{0,10}(\d{4,8})
address    : \b\d{1,5}\s+[A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,3}\s+(St|Street|Ave|Avenue|Rd|Road|Blvd|Dr|Lane|Ln|ul\.|al\.)
```

---

### T2 — Migration: 006_audit_log.sql

```sql
-- +goose Up

CREATE TABLE IF NOT EXISTS llm_audit_log (
    id           TEXT PRIMARY KEY,
    email_id     TEXT NOT NULL REFERENCES emails(id),
    provider     TEXT NOT NULL,
    model        TEXT NOT NULL,
    content_mode TEXT NOT NULL,
    bytes_sent   INTEGER NOT NULL,
    created_at   DATETIME NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS llm_audit_log;
```

---

### T3 — Audit repo

`internal/db/repo/audit_repo.go`

```go
type AuditEntry struct {
    ID          string
    EmailID     string
    Provider    string
    Model       string
    ContentMode string
    BytesSent   int
    CreatedAt   time.Time
}

type AuditRepo struct { db *sql.DB }

func NewAuditRepo(db *sql.DB) *AuditRepo
func (r *AuditRepo) Save(ctx context.Context, e AuditEntry) error
```

---

### T4 — Config: validate redacted_body

`internal/config/config.go`

`content.mode` currently accepts `headers_only` and `full_body`. Add `redacted_body`
to the valid set in `validate()`:

```go
switch c.Content.Mode {
case "", "headers_only", "redacted_body", "full_body":
    // valid
default:
    return fmt.Errorf("config: unknown content.mode %q", c.Content.Mode)
}
```

---

### T5 — Scheduler: apply redaction and write audit log

`internal/scheduler/scheduler.go`

Add `AuditRepo *repo.AuditRepo` and `ContentMode string` to `Config`.

In `processMessage`, before the LLM call:

```go
body := msg.Body
if s.cfg.ContentMode == "redacted_body" {
    body = privacy.Redact(body)
}

req := llm.ClassifyRequest{
    // ... existing fields ...
    Body: body,
}
```

After a successful LLM call, write the audit entry:

```go
_ = s.cfg.AuditRepo.Save(ctx, repo.AuditEntry{
    ID:          idx.GenerateID(),
    EmailID:     e.ID,
    Provider:    s.cfg.LLMProvider.Name(),
    Model:       classification.Source, // contains "llm:anthropic" etc.
    ContentMode: s.cfg.ContentMode,
    BytesSent:   len(llm.FormatUserMessage(req)),
    CreatedAt:   timex.NowUTC(),
})
```

Audit write failure is non-fatal: log the error and continue.

---

### T6 — cmd_init.go: offer redacted_body

`cmd/email-agent/cmd_init.go` — update the body access prompt in `configureLLM`:

```
  Body access (headers_only/redacted_body/full_body) [headers_only]:
```

Update the inline description printed before the prompt:

```
  headers_only  — subject and headers only (most private)
  redacted_body — body sent with PII replaced by [EMAIL], [PHONE], etc.
  full_body     — complete body, unmodified
```

---

### T7 — Wire up in main.go

```go
auditRepo := repo.NewAuditRepo(sqlDB)

sched := scheduler.New(scheduler.Config{
    // existing fields ...
    AuditRepo:   auditRepo,
    ContentMode: cfg.Content.Mode,
})
```

---

### T8 — Tests

| Package | What to test |
|---------|--------------|
| `internal/privacy` | Each pattern redacted correctly; non-sensitive text unchanged; multiple patterns in one string; empty string |
| `internal/privacy` | `redacted_body` mode: body is redacted before LLM call |
| `internal/privacy` | `full_body` mode: body sent verbatim |
| `internal/privacy` | `headers_only` mode: body empty, redactor not called |
| `internal/db/repo` | `AuditRepo.Save` round-trip |
| `internal/scheduler` | Audit entry written after successful LLM call |
| `internal/scheduler` | Audit write failure does not abort processing |

---

### T9 — Docs

- `docs/db-schema.md` — add `llm_audit_log` table; update migration version
- `docs/settings.md` — update `content.mode` row to list all three values
- `config.example.yaml` — update comment to mention `redacted_body`
- `docs/stages/rollout/005-01-llm-enrollment.md` — update body access prompt examples

---

## Dependencies

No new external dependencies. Redaction uses Go's standard `regexp` package.

---

## Recommended Task Order

```
T1  → privacy/redactor.go
T2  → migration 006_audit_log.sql
T3  → audit_repo.go
T4  → config validate redacted_body
T5  → scheduler: redaction + audit write
T6  → cmd_init.go: body access prompt update
T7  → main.go: wire AuditRepo + ContentMode
T8  → tests
T9  → docs
```

---

## Definition of Done

1. `make check` passes.
2. With `content.mode=redacted_body`, the text sent to the LLM contains
   `[EMAIL]`, `[PHONE]`, `[FINANCIAL]`, `[ADDRESS]`, or `[TOKEN]` in place of
   the corresponding patterns; the original body is not stored or transmitted.
3. With `content.mode=full_body`, the body is sent unchanged.
4. Every successful LLM call produces a row in `llm_audit_log`.
5. An audit write failure does not suppress the notification.
6. `email-agent init llm` presents all three body access options with descriptions.

# 005-01-llm-classification.md

Status: Draft
Version: 0.3

# Stage 005 — LLM Classification

## Goal

Replace the final notification decision with an LLM-based classification that produces a
richer result: a calibrated importance level, a category, a score, and a one-sentence
**summary** of what the email is actually about.

The rule-based scorer runs first as a cheap pre-filter. LLM is called only for emails that
pass the rule-based threshold. Both results are stored independently so divergences can be
audited over time.

No automatic email replies. No user-interaction changes. The only visible change to the
user is a better Telegram notification that includes a summary line.

**LLM is fully optional.** With no provider configured the daemon behaves exactly as in
Stage 2. LLM is set up independently via `email-agent init llm` and never required by
the full setup wizard.

---

## What Changes

| Before | After |
|--------|-------|
| Rule-based scorer makes the final call | LLM overrides rule-based for notifiable emails |
| Notification shows score reasons | Notification shows LLM summary + score |
| Single classification per email | Two classifications per email (rule_based + llm) |
| No email body fetched | Body optionally fetched (user-controlled) |
| No external AI calls | Anthropic or OpenAI called per notifiable email |

---

## Flow

```
processMessage:
  1. Fetch headers (always) + body (if content.mode = full_body)
  2. Rule-based classify → save as source="rule_based"
  3. If rule_level < min_importance → status=ignored, done (no LLM call)
  4. Call LLM provider → save as source="llm:{provider}"
  5. If |llm_score − rule_score| ≥ divergence_warn threshold → WARN log
  6. If llm_level < min_importance → status=ignored, done
  7. Send Telegram notification (with LLM summary)
  8. status=notified
```

Rule-based is the gate. LLM is the final arbiter.

---

## Content Mode

Controlled by the `content.mode` setting, configured via `email-agent init llm`:

| Mode | What LLM receives | Default |
|------|-------------------|---------|
| `headers_only` | From, Subject, Date, thread/newsletter signals | ✓ |
| `full_body` | All of the above + plain-text body (truncated to 3 000 chars) |  |

Body is stripped of HTML tags before being sent to the LLM.
Users who are privacy-sensitive can keep `headers_only` and still get better
classification than the rule-based scorer alone.

Content mode is only relevant when a provider is configured. It has no effect when
`llm.provider` is empty.

---

## Notification Message (updated)

```
📧 New email

From: John Doe <john@work.com>
Subject: Re: Q3 budget review
Date: 15 Jun 2026 11:30 UTC

Importance: important (score 82)
Summary: Finance team requesting Q3 budget approval before the board meeting on Friday.

[✓ Handled]  [✗ Ignore]  [ℹ Details]
```

The `details` command shows both classifications:

```
ℹ Email details

From: John Doe <john@work.com>
Subject: Re: Q3 budget review
Date: 15 Jun 2026 11:30 UTC

LLM classification: important (score 82)
Category: finance
Summary: Finance team requesting Q3 budget approval before the board meeting on Friday.

Rule-based classification: maybe (score 55)
Reasons:
  • baseline: +40
  • meeting keyword in subject: +20
  • unknown sender: -10
```

---

## LLM Provider Interface

`internal/llm/provider.go`

```go
type ClassifyRequest struct {
    FromEmail string
    FromName  string
    Subject   string
    Body      string // empty when content.mode = headers_only
    Language  string
}

type ClassifyResult struct {
    Level    domain.ImportanceLevel
    Category domain.Category
    Score    int      // 0–100, LLM's own assessment
    Reasons  []string // brief signal list
    Summary  string   // 1–2 sentence plain-English summary
}

type Provider interface {
    Classify(ctx context.Context, req ClassifyRequest) (ClassifyResult, error)
    Name() string // "anthropic" | "openai"
}
```

---

## Prompt Design

Same system prompt for both providers.

**System:**
```
You are an email importance classifier. Given email metadata and optionally the body,
you must return a JSON object with these fields:

  level    : "critical" | "important" | "maybe" | "ignore"
  category : "work" | "finance" | "legal" | "government" | "school" | "family" |
             "security" | "travel" | "shopping" | "recruiting" | "marketing" |
             "social" | "other"
  score    : integer 0–100 (your confidence-weighted importance)
  reasons  : array of short strings explaining the key signals
  summary  : one or two plain-English sentences describing what this email is about

Scoring guide:
  90–100 critical  — immediate action required
  70–89  important — should be read today
  30–69  maybe     — worth a glance but not urgent
  0–29   ignore    — newsletter, promotion, or irrelevant

Be conservative: err toward lower scores for unknown senders and marketing content.
Reply with JSON only, no prose.
```

**User:**
```
From: {from_name} <{from_email}>
Subject: {subject}
Date: {date}
Language: {language}
Is reply: {yes|no}
Has unsubscribe header: {yes|no}
{Body:\n{body_excerpt}\n}   ← omitted when headers_only
```

---

## Providers

### Anthropic

- Model: `claude-sonnet-4-6` (default; overridable via `llm.model`)
- Structured output via JSON tool use
- SDK: `github.com/anthropics/anthropic-sdk-go`
- API key: `llm.anthropic.api_key` (stored encrypted)

### OpenAI

- Model: `gpt-4o-mini` (default; overridable via `llm.model`)
- Structured output via `response_format: { type: "json_object" }`
- SDK: `github.com/sashabaranov/go-openai`
- API key: `llm.openai.api_key` (stored encrypted)

If `llm.provider` is empty or unset, the LLM step is skipped entirely and
rule-based classification is used as-is (existing behaviour preserved).

---

## Divergence Warning

If `|llm_score − rule_score| ≥ llm.score_divergence_warn` (default: 30), log at WARN:

```
level=warn msg="classification divergence"
  email_id=01J...
  rule_score=55 rule_level=maybe
  llm_score=88  llm_level=important
  provider=anthropic
```

This is useful for tuning the rule-based scorer over time.

---

## Directory Structure Changes

```
internal/
  llm/
    provider.go             # Provider interface, ClassifyRequest, ClassifyResult
    anthropic/
      client.go             # NEW: Anthropic implementation
      client_test.go        # NEW
    openai/
      client.go             # NEW: OpenAI implementation
      client_test.go        # NEW
  email/
    provider.go             # add Body field to Message
    imap/
      client.go             # optionally fetch plain-text body
  domain/
    classification.go       # add Summary field to Classification
  config/
    config.go               # add LLMConfig, ContentConfig
  db/
    migrations/
      005_llm_classification.sql  # NEW
    repo/
      classification_repo.go      # add source-aware methods
  scheduler/
    scheduler.go            # LLM integration
  telegram/
    bot.go                  # include summary in notification
    handler.go              # show both classifications in details
  cmd/email-agent/
    cmd_init.go             # add email-agent init llm subcommand (section already split)
```

---

## Tasks

### T1 — Domain model: Classification additions

`internal/domain/classification.go`

Add fields:
```go
type Classification struct {
    // existing fields ...
    Source  string // "rule_based" | "llm:anthropic" | "llm:openai"
    Summary string // empty for rule_based; LLM-generated sentence(s)
}
```

Add source constants:
```go
const (
    SourceRuleBased = "rule_based"
    SourceLLM       = "llm" // prefix; full value is "llm:{provider}"
)
```

---

### T2 — Migration: 005_llm_classification.sql

```sql
-- +goose Up

ALTER TABLE classifications ADD COLUMN source  TEXT NOT NULL DEFAULT 'rule_based';
ALTER TABLE classifications ADD COLUMN summary TEXT NOT NULL DEFAULT '';

-- +goose Down

-- irreversible
```

---

### T3 — Config additions

New config keys:

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `content.mode` | `headers_only` `full_body` | `headers_only` | What to send to the LLM |
| `llm.provider` | `anthropic` `openai` `` | `` (disabled) | LLM provider to use |
| `llm.anthropic.api_key` | string | — | Anthropic API key (encrypted) |
| `llm.openai.api_key` | string | — | OpenAI API key (encrypted) |
| `llm.model` | string | — | Model override (uses provider default if empty) |
| `llm.score_divergence_warn` | integer | `30` | Log WARN when scores differ by this much |

Add to `config.KnownKeys`, `Config` struct, `applySettings`, `defaults`.

```go
type LLMConfig struct {
    Provider            string
    AnthropicAPIKey     string
    OpenAIAPIKey        string
    Model               string
    ScoreDivergenceWarn int
}

type ContentConfig struct {
    Mode string // "headers_only" | "full_body"
}

type Config struct {
    // existing ...
    LLM     LLMConfig
    Content ContentConfig
}
```

Update `validate()` to make the entire LLM block optional: skip LLM key checks
when `cfg.LLM.Provider == ""`.

---

### T4 — IMAP: optional body fetch

`internal/email/provider.go` — add `Body` to `Message`:
```go
type Message struct {
    // existing fields ...
    Body string // plain-text body; empty when not fetched
}
```

`internal/email/imap/client.go` — extend `FetchSince`:

- When `fetchBody` is true, add `BODY[TEXT]` to the fetch items.
- Strip HTML tags (use `golang.org/x/net/html` tokenizer).
- Truncate to 3 000 characters.
- Fallback gracefully: if body fetch fails, log WARN and continue with empty body.

The `Client` struct gains a `FetchBody bool` field. The value flows from config
in `main.go` when constructing the IMAP client:

```go
imapClient := imapmail.NewClient(imapmail.Config{
    // existing fields ...
    FetchBody: cfg.Content.Mode == "full_body",
})
```

---

### T5 — LLM provider interface

`internal/llm/provider.go` — shared types and interface (shown above).

No concrete implementations here; each provider lives in its own sub-package.

---

### T6 — Anthropic provider

Adds dependency: `github.com/anthropics/anthropic-sdk-go` (run `go get` as part of this task).

`internal/llm/anthropic/client.go`

```go
type Client struct {
    client *anthropic.Client
    model  string
}

func New(apiKey, model string) *Client
func (c *Client) Name() string { return "anthropic" }
func (c *Client) Classify(ctx context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error)
```

Uses the Anthropic tool-use API:
- Define a `classify_email` tool with a JSON schema matching `ClassifyResult`.
- Send one `user` message with the formatted prompt.
- Parse the tool call arguments from the response.
- Return `ClassifyResult`.

---

### T7 — OpenAI provider

Adds dependency: `github.com/sashabaranov/go-openai` (run `go get` as part of this task).

`internal/llm/openai/client.go`

```go
type Client struct {
    client *openai.Client
    model  string
}

func New(apiKey, model string) *Client
func (c *Client) Name() string { return "openai" }
func (c *Client) Classify(ctx context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error)
```

Uses `response_format: json_object` with the same system + user prompt.
Unmarshals the JSON response into `ClassifyResult`.

---

### T8 — ClassificationRepo: source-aware methods

```go
// GetByEmailIDAndSource retrieves the classification for a given email and source.
// Returns nil, nil if not found.
func (r *ClassificationRepo) GetByEmailIDAndSource(
    ctx context.Context, emailID, source string,
) (*domain.Classification, error)
```

The existing `GetByEmailID` currently returns whichever row the DB returns first
(no `ORDER BY`). After adding `source`, it must explicitly prefer the `rule_based`
record so the handler's `details` command continues to work without changes:

```sql
SELECT ... FROM classifications WHERE email_id = ?
ORDER BY (source = 'rule_based') DESC LIMIT 1
```

Update `Save` and scan functions to include `source` and `summary` columns.

---

### T9 — Scheduler: LLM integration

`internal/scheduler/scheduler.go`

Add to `Config`:
```go
type Config struct {
    // existing ...
    LLMProvider         llm.Provider // nil when llm.provider is empty
    ScoreDivergenceWarn int
}
```

Update `processMessage`:

```go
// After rule-based classification is saved and threshold check passes:
if s.cfg.LLMProvider != nil {
    req := buildLLMRequest(msg, feats)
    llmResult, err := s.cfg.LLMProvider.Classify(ctx, req)
    if err != nil {
        // LLM failure is non-fatal: log and fall through to rule-based decision
        s.cfg.Logger.Warn(err, "uid", msg.UID)
    } else {
        llmClass := domain.Classification{...source: "llm:" + s.cfg.LLMProvider.Name(), summary: llmResult.Summary}
        _ = s.cfg.ClassificationRepo.Save(ctx, llmClass)
        s.logDivergence(ruleClass, llmClass)
        // Use LLM result for notification decision
        classification = llmClass
    }
}
```

LLM errors are **non-fatal**: fall back to rule-based result so a quota exhaustion
or network blip does not silence notifications entirely.

---

### T10 — Telegram: summary in notification

`internal/telegram/bot.go` — update `formatMessage`:
- If `c.Summary != ""`, include a `Summary: {text}` line.
- Remove the `Why: {reasons}` line from the main notification (reasons stay in `details`).

`internal/telegram/handler.go` — update `handleDetails` / `formatDetails`:
- Load both classifications: `rule_based` and `llm:*` (if present).
- Show LLM result first (level, score, summary), then rule-based reasons.

---

### T11a — Refactor cmd_init.go into section subcommands

**Prerequisite for T11.** Currently `cmd_init.go` is a single monolithic `runInit`
function. It must be split before `email-agent init llm` can be added cleanly and
before the LLM section can be kept out of the full setup wizard.

`cmd/email-agent/cmd_init.go`:

1. Extract a `sectionFn` type:
   ```go
   type sectionFn func(ctx context.Context, sc *bufio.Scanner,
       r *repo.SettingsRepo, current map[string]string) error
   ```

2. Extract three section functions from `runInit`:
   - `configureAccount(ctx, sc, r, current) error`
   - `configureTelegram(ctx, sc, r, current) error`
   - `configureNotifications(ctx, sc, r, current) error`

   Each section function reads current values from `current` and uses them as
   prompt defaults. Password/token fields display `(Enter to keep unchanged)` and
   skip saving when the user presses Enter.

3. Add `newInitSectionCmd` helper that opens the existing DB (key from keychain),
   runs migrations, loads current settings, and calls the given `sectionFn`.
   Returns an error if the DB does not yet exist.

4. Register the three existing sections as subcommands:
   ```go
   initCmd.AddCommand(
       newInitSectionCmd(dbPath, "account",       "...", configureAccount),
       newInitSectionCmd(dbPath, "telegram",      "...", configureTelegram),
       newInitSectionCmd(dbPath, "notifications", "...", configureNotifications),
       // email-agent init llm — added in T11
   )
   ```

5. `runFullInit` (the root `init` command) calls all three section functions in
   sequence as before. It still creates the DB and generates the encryption key
   on first run. It does **not** call `configureLLM`.

6. When the DB already exists and `email-agent init` is run without a subcommand,
   hint the user: `"To reconfigure a single section use: email-agent init <account|telegram|notifications|llm>"`.

---

### T11 — email-agent init llm subcommand

The `init` command will have section subcommands after T11a. Add `email-agent init llm`
following the same pattern.

**LLM is never prompted during `email-agent init` (full setup).** It is always a separate
opt-in step.

`cmd/email-agent/cmd_init.go` — add `configureLLM` and register it:

```go
initCmd.AddCommand(
    newInitSectionCmd(dbPath, "llm", "Configure LLM classification (optional)", configureLLM),
)
```

`configureLLM` prompt sequence:

```
LLM Classification

  Provider (anthropic / openai, Enter to disable) []:
```

If provider is left empty → save `llm.provider = ""` and exit. LLM is disabled.

If provider is `anthropic`:
```
  Anthropic API key (Enter to keep unchanged):
  Model override (Enter for default claude-sonnet-4-6) []:
  Body access (headers_only / full_body) [headers_only]:
```

If provider is `openai`:
```
  OpenAI API key (Enter to keep unchanged):
  Model override (Enter for default gpt-4o-mini) []:
  Body access (headers_only / full_body) [headers_only]:
```

Settings saved: `llm.provider`, `llm.anthropic.api_key` or `llm.openai.api_key`,
`llm.model` (omitted if empty), `content.mode`.

**Usage examples:**

```bash
# Enable Anthropic LLM with full body access
email-agent init llm

# Disable LLM (press Enter at provider prompt)
email-agent init llm

# Re-run any time to switch provider, rotate API key, or change body mode
email-agent init llm
```

---

### T12 — Tests

| Package | What to test |
|---------|--------------|
| `internal/llm/anthropic` | Prompt constructed correctly; JSON response parsed into ClassifyResult |
| `internal/llm/openai` | Same |
| `internal/scheduler` | LLM called for notifiable email; LLM error falls back to rule-based; divergence logged when scores differ |
| `internal/scheduler` | LLM disabled (nil provider) behaves as before |
| `internal/telegram` | `formatMessage` includes summary when present; omits it when empty |
| `internal/telegram` | `handleDetails` shows both classifications when LLM result exists |
| `internal/db/repo` | `GetByEmailIDAndSource` round-trip; `Save` with source and summary |

LLM provider tests use a fake HTTP server (no real API calls in CI).

---

### T13 — Docs

- `docs/db-schema.md` — add `source`, `summary` to classifications table; update migration version
- `docs/settings.md` — add LLM and Content sections
- `config.example.yaml` — add LLM and content entries

---

## Dependencies

New external dependencies:

| Package | Purpose | Added in |
|---------|---------|----------|
| `github.com/anthropics/anthropic-sdk-go` | Anthropic API client | T6 |
| `github.com/sashabaranov/go-openai` | OpenAI API client | T7 |
| `golang.org/x/net/html` | HTML tag stripping for body mode | T4 |

`golang.org/x/net` may already be an indirect dependency; check before adding.

---

## Recommended Task Order

```
T1  → domain: Source, Summary fields
T2  → migration: source, summary columns
T3  → config: LLMConfig, ContentConfig, validate() optional LLM
T4  → IMAP: Body field, FetchBody, html strip, truncate
T5  → LLM provider interface
T6  → Anthropic client (adds SDK)
T7  → OpenAI client (adds SDK)
T8  → ClassificationRepo: source-aware methods, ORDER BY rule_based
T9  → Scheduler: LLM integration, divergence logging
T10 → Telegram: summary in notification, both results in details
T11a→ Refactor cmd_init into section subcommands
T11 → email-agent init llm subcommand
T12 → Tests
T13 → Docs
```

---

## Definition of Done

1. `make check` passes.
2. With `llm.provider` empty, behaviour is identical to Stage 2 (rule-based only).
3. With `llm.provider=anthropic` and a valid key, notifiable emails receive an LLM
   classification and the notification includes a summary line.
4. LLM API error does not suppress the notification — rule-based result is used.
5. Divergence ≥ 30 points produces a WARN log entry with both scores.
6. `content.mode=full_body` causes the email body to be fetched and included in the
   LLM prompt; `headers_only` sends no body.
7. The `details` Telegram command shows both LLM and rule-based results when both exist.
8. Both provider implementations (Anthropic, OpenAI) pass their unit tests using a fake
   HTTP server.

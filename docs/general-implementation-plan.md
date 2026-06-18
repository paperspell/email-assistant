# general-implementation-plan.md

Status: Draft  
Version: 0.1

# Email Agent

## Vision

Email Agent is a local-first personal email assistant.

The application runs entirely on the user's machine as a single Go binary and helps the user monitor multiple email accounts, identify important emails, and receive notifications through Telegram.

The system prioritizes privacy, transparency, and user control.

No central backend service is required.

---

# Goals

The system should:

- Monitor one or more email accounts.
- Detect new incoming emails.
- Determine which emails are important.
- Notify the user about important emails via Telegram.
- Learn from user feedback.
- Support multiple LLM providers.
- Store all state locally.
- Operate as a background daemon.

---

# Non-Goals

The MVP should NOT include:

- Web UI.
- Cloud backend.
- Multi-user support.
- Attachment processing.
- Automatic email replies.
- Automatic execution of actions.
- Billing or subscription management.
- Complex semantic search.

---

# Core Principles

## Local First

All configuration and state must remain on the user's machine.

## Privacy First

Only the minimum required email content should be sent to LLM providers.

## Provider Independence

The system must support multiple LLM providers through a common abstraction.

## Explainability

The user should be able to understand why an email was classified as important.

## User Control

The system must never send emails automatically without explicit user approval.

---

# Functional Requirements

## Email Accounts

The system must support:

- Multiple email accounts
- IMAP as the initial protocol
- Future Gmail API support
- Future Microsoft Graph support

## Storage

The system must store:

- Configuration
- Sync state
- Email metadata
- Classification results
- User feedback
- Audit information

SQLite should be used as the primary datastore.

## LLM Providers

Initial providers:

- Anthropic Claude
- OpenAI

Future providers:

- Google Gemini
- Perplexity
- Local models
- OpenAI-compatible APIs

## Telegram

The system must:

- Send notifications
- Support commands in future stages
- Support reply workflows in future stages

---

# Privacy Requirements

The system should support:

## Metadata Only Mode

Only headers and metadata may be analyzed.

## Redacted Body Mode

Body content may be analyzed after removing obvious sensitive data.

## Full Body Mode

The original email body may be analyzed.

User chooses the mode.

---

# Product Evolution

## Stage 1 — Notification Foundation

Goal:

Notify the user about every new email.

Features:

- Local daemon
- SQLite
- One IMAP account
- Telegram integration
- Notification for every email

---

## Stage 2 — Importance Detection

Goal:

Notify only about important emails.

Features:

- Rule engine
- Importance scoring
- Categories
- Notification filtering

---

## Stage 3 — State Management

Goal:

Track lifecycle of emails.

Statuses:

- new
- notified
- important
- ignored
- handled
- reply_needed

---

## Stage 4 — Telegram Interaction

Goal:

Allow email management from Telegram.

Features:

- View details
- Mark handled
- Ignore sender
- Request classification explanation

---

## Stage 5 — LLM Classification

Goal:

Improve classification quality.

Features:

- Anthropic provider
- OpenAI provider
- Structured classification output
- Explanation generation

---

## Stage 6 — Privacy Layer

Goal:

Reduce information exposure.

Features:

- Privacy modes
- Redaction
- Audit logs
- Provider controls

---

## Stage 7 — Multi-Account Support

Goal:

Monitor multiple email accounts.

Features:

- Multiple IMAP accounts
- Independent sync state
- Per-account configuration

Detailed plan: `docs/stages/007-01-multi-account.md`.

OAuth readiness: this stage ships password auth only, but stores an `auth_type`
discriminator and routes provider construction through a factory, so a later
Gmail/Microsoft Graph OAuth backend slots in without a schema change or wiring
rewrite. The OAuth mechanics themselves (token storage, consent flow,
refresh-on-reconnect) are deferred to that backend stage.

---

## Stage 8 — Gmail OAuth Backend

Goal:

Authenticate Gmail accounts with OAuth instead of a password, using the
`auth_type` discriminator and provider factory introduced in Stage 7.

Features:

- Google OAuth client credentials stored once (global setting)
- Per-account refresh/access token storage
- Browser-based consent flow (loopback) during `account add`
- IMAP login via XOAUTH2 with automatic access-token refresh
- Graceful re-consent prompt when a refresh token is revoked or expires

Detailed plan: `docs/stages/008-01-gmail-oauth.md`.

Transport: Gmail over IMAP + XOAUTH2 (reuses the existing IMAP client). The
Gmail API and Microsoft Graph backends remain future work behind the same
`email.Provider` interface.

---

## Stage 9 — Daemon Mode

Goal:

Production deployment.

Features:

- Service installation
- Service management
- Graceful restart
- Recovery mechanisms

---

## Stage 10 — Telegram Workflows

Goal:

Operate email workflows from Telegram.

Features:

- Request details
- Draft reply generation
- Feedback collection

---

## Stage 11 — Reply Assistance

Goal:

Help the user respond.

Features:

- Reply draft generation
- Suggested responses
- Explicit confirmation before sending

---

## Stage 12 — Semi-Automatic Mode

Goal:

Reduce manual confirmations. Let the agent act on clear-cut emails on its own
and only ask the user about genuinely important or ambiguous ones.

Features:

- Autonomy levels: `manual` (today's behaviour), `semi_auto`, configurable per
  account or globally
- Confidence-based auto-actions: auto-ignore obvious low-importance mail,
  auto-mark newsletters/bulk as handled, without a Telegram prompt
- Per-category / per-importance automation rules (e.g. notify on `critical`,
  digest everything below `important`)
- Periodic digest summary instead of one Telegram message per low-priority email
- Auto-actions are limited to non-outbound, reversible triage (ignore, handled,
  label); replies and any outbound action still require explicit confirmation,
  per the User Control principle

This stage builds on the existing feedback loop (Stage 4) and LLM confidence
scoring (Stage 5), and complements — but never overrides — the
[User Control](#user-control) guarantee.

---

# Future Plans

These are concrete proposals identified during development that are not yet scheduled into a stage.

---

## Plan A — IMAP Connection Resilience

**Background:**
The IMAP client connects once at daemon startup and never reconnects. When the server drops the connection (observed with Zoho Mail after idle periods), all subsequent poll attempts fail with `use of closed network connection`. The backoff retries exhaust their window and the daemon stops polling until restarted. This is invisible in logs without `RetryNotify` because `backoff.Retry` discards intermediate errors.

**What was fixed:**
- Added `backoff.RetryNotify` to surface intermediate poll errors at DEBUG level.
- Added a logger to the IMAP client for per-step debug output (`uid search`, `fetch`, `body fetch`).
- Split the IMAP body fetch into a separate command to work around a Zoho server bug where combining `BODY[HEADER.FIELDS (...)]` and `BODY[TEXT]` in one FETCH produces a malformed section-spec response.
- Switched from `BODY[TEXT]` to `BODY.PEEK[TEXT]` so fetching the body does not set the `\Seen` flag on messages.

**Remaining work:**
Implement automatic reconnect in the IMAP client. When `FetchSince` or `fetchBodies` returns a connection-level error (EOF, `use of closed network connection`, or a parse error that closes the underlying TCP connection), the client should close and re-dial before the next poll attempt rather than failing repeatedly until the daemon is restarted.

Suggested approach: track a `broken` flag on the client; when set, `FetchSince` calls `Connect` before issuing any IMAP commands. Alternatively, wrap the reconnect in the scheduler's poll loop so a single reconnect attempt is made before the backoff retry gives up.

---

## Plan B — Rule Learning from LLM Divergence

**Background:**
The rule-based scorer and LLM often disagree significantly (divergence ≥ 30 points). In one observed case the rule-based scored an email 70 (important) while the LLM correctly scored it 10 (ignore): the email was sent from and to the same address with a vague invoice request and no real details. The rules fired on "urgent" in the subject and "pay/invoice" keywords in the body without detecting the self-email pattern. The divergence threshold is already logged at WARN level; the missing piece is making the rules improve from these signals.

**Proposed solutions (in recommended order):**

### B1 — Self-email rule (immediate)
Add a rule to `internal/importance/rules.go`: if `from_email == account_email`, apply a strong negative score (e.g. cap the total at 20). Self-sent emails are almost always test messages, automated scripts, or spoofing attempts. This is a direct correctness fix, not a weight-tuning issue.

### B2 — Divergence analytics command (medium)
Add `email-agent analyze` that queries classification pairs (`source=rule_based` and `source=llm:*`) with large divergence from the `classifications` table. Groups results by which rule `reason` strings appear most often on the high-scoring side when the LLM disagrees. Output example:

```
Rule signal                      Fired  LLM avg  Rule avg  Delta
urgent keyword in subject           12       18       68     -50
invoice keyword in body              8       22       62     -40
known sender bonus                   5       75       80      +5
```

The human reviews the report and decides which weights to adjust. No automatic changes, fully auditable. The `reason` field is already stored per classification so no schema change is needed.

### B3 — Automatic rule weight nudging (advanced)
When a divergence fires AND the LLM score is in a high-confidence zone (≤ 20 or ≥ 80), treat the LLM result as a weak training signal. For each rule that contributed to the gap (identified from `reason` strings), update a `rule_effectiveness` record: fire count, LLM-agree count, cumulative weight delta. Periodically compute a suggested weight adjustment: `current_weight × agree_rate`, clamped to configured min/max bounds, with a minimum sample size before any change is applied. Changes are proposed via `email-agent tune apply` and require explicit confirmation before being written to settings.

**Prerequisite for B2 and B3:** reason strings must encode the rule name and contribution delta in a stable, parseable format (e.g. `"urgent keyword in subject: +20"`). Current format already follows this convention.

**Recommended sequence:** implement B1 now, collect 2–4 weeks of production divergence data, then evaluate whether B2 alone is sufficient or B3 is warranted.

---

# Future Ideas

These ideas are intentionally outside the implementation plan.

## Automatic Actions

Potential future capabilities:

- Archive newsletters
- Categorize invoices
- Auto-label emails
- Schedule reminders

## Automatic Replies

Potential future capabilities:

- Rule-based replies
- Low-risk automated responses

This functionality must not be implemented in the MVP.

---

# Success Criteria

The project is considered successful when:

1. The user can run a single binary locally.
2. Multiple email accounts can be monitored.
3. Important emails can be identified reliably.
4. Telegram notifications are useful and actionable.
5. The system improves through user feedback.
6. Privacy remains under user control.
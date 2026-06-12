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

---

## Stage 8 — Daemon Mode

Goal:

Production deployment.

Features:

- Service installation
- Service management
- Graceful restart
- Recovery mechanisms

---

## Stage 9 — Telegram Workflows

Goal:

Operate email workflows from Telegram.

Features:

- Request details
- Draft reply generation
- Feedback collection

---

## Stage 10 — Reply Assistance

Goal:

Help the user respond.

Features:

- Reply draft generation
- Suggested responses
- Explicit confirmation before sending

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
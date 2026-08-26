# coding-guidelines.md

Status: Draft  
Version: 0.1

# Coding Guidelines

## Purpose

This document defines implementation standards and architectural constraints for the Email Agent project.

The primary goals are:

- Maintainability
- Simplicity
- Privacy
- Testability
- Long-term extensibility

---

# Core Principles

## Local First

The application must work without any backend service.

All state must be stored locally.

The application should remain useful even if no LLM provider is configured.

---

## Privacy First

The default implementation should minimize external data exposure.

Rules:

- Do not send attachments to LLM providers.
- Do not store raw email bodies by default.
- Do not transmit unnecessary email content.
- Log all external LLM interactions locally.

---

## Simplicity Over Cleverness

Prefer simple and understandable code.

Avoid:

- Excessive abstractions
- Generic frameworks
- Premature optimization
- Reflection-heavy solutions

---

## Single Responsibility

Each package should have a clear responsibility.

Avoid "god packages" and "god services".

---

# Technology Constraints

## Language

Go is the primary language.

Target:

text Go 1.26+

---

## Storage

Primary datastore:

text SQLite

No additional databases should be introduced without strong justification.

---

## Deployment

Target deployment:

text Single binary

No Docker requirement.

No Kubernetes requirement.

No backend infrastructure.

---

# Project Structure

Recommended layout:

text cmd/ internal/ docs/ migrations/

Public packages should be avoided unless necessary.

---

# Package Design

## Depend on Interfaces

External integrations should be hidden behind interfaces.

Examples:

text EmailProvider  LLMProvider  NotificationProvider

Business logic must not depend on provider SDKs.

---

## Keep Domain Independent

Domain models should not depend on:

- IMAP
- Telegram
- Anthropic
- OpenAI
- SQLite

Domain types should remain transport-agnostic.

---

# Error Handling

## Explicit Errors

Always return errors.

Avoid panic except for unrecoverable startup failures.

---

## Contextual Errors

Wrap errors with context.

Example:

text failed to fetch email metadata  failed to classify message  failed to send telegram notification

---

## Retry Strategy

Retry only when safe.

Examples:

text network failures  temporary provider failures  rate limiting

Do not retry indefinitely.

Use exponential backoff.

---

# Logging

## Structured Logging

Use structured logs.

Preferred fields:

text account_id  message_id  sender  provider  classification  error

---

## Avoid Sensitive Data

Do not log:

text email body  attachments  api keys  oauth tokens  passwords

---

# Configuration

## Local Configuration

Configuration lives in the `settings` table of the encrypted SQLite database. There is no
configuration file — `email-agent init` creates the database and seeds it, and
`email-agent config set <key> <value>` changes a setting afterwards.

`internal/config` loads the `Config` struct from the settings repository. Every key is declared in
`internal/config/keys.go`, and reads and writes must go through those constants rather than a raw
string literal, so that renames fail at compile time instead of silently pointing at a key nobody
writes any more.

Email accounts are not settings. They live in the `accounts` table, are managed with the `account`
subcommands, and may override polling and digest defaults per account.

`config.example.yaml` is documentation only and is never read by the application; it describes the
environment variables and CLI flags that control how the daemon starts. See
[settings.md](settings.md) for the settings themselves.

---

## Validation

Validate configuration on startup.

Fail fast when required settings are missing.

---

## Explicit Defaults

All defaults should be documented. `config.defaults()` is the runtime source of truth: it builds
the `Config` the daemon actually uses. `config.DefaultValues()` derives the display strings shown
by the CLI from it and covers only the settings that are user-visible, so a new default belongs in
`defaults()` first and in `DefaultValues()` only if the CLI should show it.

Avoid hidden behavior.

---

# Database Guidelines

## Migrations Required

All schema changes must use migrations.

Manual schema drift is not allowed.

---

## Stable Schema

Avoid frequent schema redesign.

Prefer additive migrations.

---

## Auditability

Important actions should be traceable.

Examples:

text email classified  notification sent  llm request performed  feedback received

---

# Testing Strategy

## Unit Tests

Required for:

text importance scoring  rule engine  feature extraction  language detection  privacy filters

---

## Integration Tests

Required for:

text sqlite repositories  imap integration  telegram integration  llm providers

Where possible, use mocks or local test fixtures.

---

## Deterministic Tests

Tests should not depend on:

text real email accounts  real telegram bots  real llm providers

---

# LLM Guidelines

## LLM as Assistant

LLMs assist decision-making.

They should not own business logic.

Preferred order:

text Rules     ↓ History     ↓ LLM

---

## Structured Output

LLMs should return structured JSON.

Avoid parsing free-form text.

---

## Provider Independence

All providers must implement the same internal interface.

The rest of the application should not know which provider is being used.

---

# Security Guidelines

## Secret Storage

Store secrets locally.

Never commit secrets to source control.

---

## Principle of Least Privilege

Request the minimum permissions necessary.

Examples:

text read-only email access  limited telegram permissions

---

## Explicit Confirmation

The system must require explicit user confirmation before sending emails.

---

# Privacy Modes

Supported modes:

text metadata_only  redacted_body  full_body

All components must respect the configured mode.

---

# Future Compatibility

Design should allow future support for:

text Gmail API  Microsoft Graph  Gemini  Perplexity  Local Models  Additional notification channels

without major refactoring.

---

# Out of Scope

The following are intentionally excluded from the MVP:

text Automatic replies  Autonomous agents  Workflow automation  Web dashboard  Cloud backend  Mobile application  Multi-user support

---

# Definition of Done

A feature is considered complete when:

1. Implementation is finished.
2. Tests are added.
3. Logging is present.
4. Configuration is documented.
5. Errors are handled.
6. Privacy implications are considered.
7. Documentation is updated.

---

# Architectural Guardrails

The project should always remain:

text Local First Single Binary SQLite Based Privacy Focused Provider Agnostic Explainable User Controlled

Any future change that violates these principles should be carefully reviewed before implementation.
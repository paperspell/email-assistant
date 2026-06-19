# architecture.md

Status: Draft  
Version: 0.1

# Email Agent Architecture

## Overview

Email Agent is a local-first Go daemon that monitors email accounts, stores state in SQLite, classifies email importance, and sends notifications to Telegram.

The system has no central backend. All configuration, state, sync metadata, classifications, and user feedback are stored locally.

External services are used only when explicitly configured:

- Email providers
- Telegram Bot API
- LLM providers such as Anthropic Claude or OpenAI

---

# Architectural Principles

## Local-First

The application runs as a local process on the user's machine or private server.

## Single Binary

The application should be distributed as a single Go binary.

## SQLite-Based State

SQLite is the default and only required persistent storage.

## Pluggable Providers

Email providers, LLM providers, and notification channels should be abstracted behind interfaces.

## Privacy by Design

The system should avoid sending raw email content to LLM providers unless explicitly configured.

---

# C4 Context Diagram

mermaid C4Context     title Email Agent - System Context      Person(user, "User", "Owns email accounts and Telegram channel")      System(emailAgent, "Email Agent", "Local-first email monitoring daemon")      System_Ext(emailProvider, "Email Providers", "IMAP, Gmail, Microsoft 365")     System_Ext(telegram, "Telegram", "Telegram Bot API and user-owned channel")     System_Ext(anthropic, "Anthropic Claude", "Optional LLM provider")     System_Ext(openai, "OpenAI", "Optional LLM provider")     System_Ext(futureLLM, "Future LLM Providers", "Gemini, Perplexity, local models, OpenAI-compatible APIs")      Rel(user, emailAgent, "Configures and runs locally")     Rel(emailAgent, emailProvider, "Fetches email metadata and content")     Rel(emailAgent, telegram, "Sends notifications and receives commands")     Rel(emailAgent, anthropic, "Optional classification")     Rel(emailAgent, openai, "Optional classification")     Rel(emailAgent, futureLLM, "Future optional classification")

---

# C4 Container Diagram

mermaid C4Container     title Email Agent - Container Diagram      Person(user, "User", "Receives Telegram notifications and gives feedback")      Container(emailAgent, "Email Agent Daemon", "Go binary", "Runs locally as foreground process or OS service")     ContainerDb(sqlite, "Local SQLite Database", "SQLite", "Stores config, sync state, metadata, classifications, feedback, audit logs")     Container(configFile, "Local Config File", "YAML/TOML", "Stores local configuration")          System_Ext(emailProvider, "Email Provider", "IMAP/Gmail/Microsoft Graph")     System_Ext(telegram, "Telegram Bot API", "Notifications and commands")     System_Ext(llmProviders, "LLM Providers", "Anthropic, OpenAI, future providers")      Rel(user, emailAgent, "Runs/configures")     Rel(emailAgent, configFile, "Reads")     Rel(emailAgent, sqlite, "Reads/writes")     Rel(emailAgent, emailProvider, "Polls/fetches emails")     Rel(emailAgent, telegram, "Sends notifications / receives commands")     Rel(emailAgent, llmProviders, "Classifies emails when needed")

---

# Main Email Processing Flow

## Description

The main flow starts when the daemon polls configured email accounts. New messages are detected using account-specific sync state. The agent fetches email metadata first, extracts features, applies local rules, and only calls an LLM if classification is uncertain and privacy settings allow it.

If the final decision is to notify, the agent sends a Telegram message and stores the notification state locally.

---

# Main Email Processing Sequence Diagram

mermaid sequenceDiagram     participant Scheduler     participant EmailProvider     participant SyncState     participant SQLite     participant FeatureExtractor     participant ImportanceFilter     participant LLMProvider     participant Telegram      Scheduler->>EmailProvider: Poll account inbox     EmailProvider-->>Scheduler: List new message IDs / UIDs      Scheduler->>SyncState: Check last processed UID     SyncState->>SQLite: Read sync state     SQLite-->>SyncState: Last processed state      Scheduler->>EmailProvider: Fetch email metadata     EmailProvider-->>Scheduler: Headers, subject, sender, date, flags      Scheduler->>SQLite: Store email metadata      Scheduler->>FeatureExtractor: Extract features     FeatureExtractor-->>Scheduler: Email features      Scheduler->>ImportanceFilter: Score email     ImportanceFilter->>SQLite: Load user rules and history     SQLite-->>ImportanceFilter: Rules, feedback, sender/domain history      alt Classification is confident         ImportanceFilter-->>Scheduler: Final classification     else Classification is uncertain and LLM allowed         ImportanceFilter->>LLMProvider: Classify minimal/redacted content         LLMProvider-->>ImportanceFilter: Structured classification         ImportanceFilter-->>Scheduler: Final classification     end      Scheduler->>SQLite: Store classification result      alt Email should notify         Scheduler->>Telegram: Send notification         Telegram-->>Scheduler: Telegram message ID         Scheduler->>SQLite: Store notification state     else Email ignored         Scheduler->>SQLite: Store ignored state     end

---

# Telegram Interaction Flow

## Description

In later stages, Telegram becomes an interaction layer. The user can mark emails as handled, mark classifications as wrong, mute senders, or request more details.

Telegram feedback is stored locally and used by the importance filter to improve future decisions.

---

# Telegram Interaction Sequence Diagram

mermaid sequenceDiagram     participant User     participant Telegram     participant TelegramHandler     participant SQLite     participant ImportanceFilter     participant EmailProvider     participant LLMProvider      User->>Telegram: Press button or send command     Telegram->>TelegramHandler: Incoming update      TelegramHandler->>SQLite: Resolve local email reference     SQLite-->>TelegramHandler: Email metadata and status      alt Mark handled         TelegramHandler->>SQLite: Update email status = handled         TelegramHandler->>Telegram: Confirm action     else Mark important / not important         TelegramHandler->>SQLite: Store feedback event         TelegramHandler->>ImportanceFilter: Update learned weights         ImportanceFilter->>SQLite: Persist updated preferences         TelegramHandler->>Telegram: Confirm feedback     else Show details         TelegramHandler->>EmailProvider: Fetch body if needed and allowed         EmailProvider-->>TelegramHandler: Email details         TelegramHandler->>Telegram: Send details     else Request reply draft         TelegramHandler->>EmailProvider: Fetch relevant email content         EmailProvider-->>TelegramHandler: Email content         TelegramHandler->>LLMProvider: Generate reply draft         LLMProvider-->>TelegramHandler: Suggested reply         TelegramHandler->>Telegram: Send draft for review     end

---

# Package Diagram

mermaid flowchart TD     cmd[cmd/email-agent]      config[internal/config]     db[internal/db]     service[internal/service]     scheduler[internal/scheduler]      email[internal/email]     imap[internal/email/imap]     gmail[internal/email/gmail - future]     graph[internal/email/graph - future]      features[internal/features]     importance[internal/importance]     privacy[internal/privacy]     llm[internal/llm]     anthropic[internal/llm/anthropic]     openai[internal/llm/openai]     localLLM[internal/llm/local - future]      telegram[internal/telegram]     reply[internal/reply]     audit[internal/audit]     domain[internal/domain]      cmd --> config     cmd --> db     cmd --> service     cmd --> scheduler      scheduler --> email     scheduler --> features     scheduler --> importance     scheduler --> telegram     scheduler --> audit      email --> imap     email --> gmail     email --> graph      importance --> features     importance --> privacy     importance --> llm     importance --> db      llm --> anthropic     llm --> openai     llm --> localLLM      telegram --> reply     telegram --> db      reply --> email     reply --> llm     reply --> db      audit --> db     features --> domain     importance --> domain

---

# Component Responsibilities

## cmd/email-agent

Application entry point.

Responsibilities:

- Parse CLI commands
- Load config
- Open database
- Start daemon or foreground mode
- Handle graceful shutdown

---

## internal/config

Responsibilities:

- Load local configuration file
- Validate required settings
- Support config path override
- Parse account, Telegram, privacy, and LLM settings

---

## internal/db

Responsibilities:

- Open SQLite database
- Run migrations
- Provide repositories
- Manage transactions

---

## internal/service

Responsibilities:

- Run app as foreground process
- Install/uninstall OS service
- Support Linux/systemd first
- Later support macOS launchd and Windows service

---

## internal/scheduler

Responsibilities:

- Coordinate polling loop
- Start per-account workers
- Apply rate limits and backoff
- Stop gracefully

---

## internal/email

Responsibilities:

- Define email provider interface (`Connect`, `FetchSince`, `MarkRead`, `FetchBody`, `Close`)
- Fetch messages
- Fetch metadata
- Fetch body when needed
- Mark messages read on user triage
- Send or draft replies in later stages

---

## internal/email/imap

Initial email provider implementation.

Responsibilities:

- Connect to IMAP server
- Authenticate
- List folders
- Poll inbox
- Fetch metadata and body
- Mark messages read (`UID STORE +FLAGS \Seen`) on demand
- Track message UIDs
- Serialize commands over the shared connection (mutex)

---

## internal/features

Responsibilities:

- Extract features from email headers
- Extract features from email structure
- Extract keywords and simple text signals
- Detect language
- Normalize text for filtering

---

## internal/importance

Responsibilities:

- Apply rule-based scoring
- Use learned user preferences
- Decide whether LLM is needed
- Produce final classification

---

## internal/privacy

Responsibilities:

- Apply privacy mode
- Redact sensitive data
- Remove attachments from LLM input
- Control what may be sent to external providers

---

## internal/llm

Responsibilities:

- Define common LLM provider interface
- Support structured classification
- Support reply draft generation in later stages
- Hide provider-specific SDK details

---

## internal/telegram

Responsibilities:

- Send notifications
- Receive commands
- Handle Telegram button actions
- Store Telegram message references

---

## internal/reply

Responsibilities:

- Generate reply drafts
- Require explicit user confirmation
- Send replies only when confirmed
- Never auto-send in MVP

---

## internal/audit

Responsibilities:

- Record important local events
- Track when content was sent to LLM
- Track user decisions
- Support future transparency/debugging

---

## internal/domain

Responsibilities:

- Shared domain models
- Email message model
- Classification model
- Account model
- User feedback model

---

# Data Flow Summary

mermaid flowchart LR     EmailProvider[Email Provider]     Agent[Local Email Agent]     SQLite[(SQLite)]     LLM[LLM Provider]     Telegram[Telegram Channel]      EmailProvider -->|Metadata first| Agent     Agent -->|Store metadata/state| SQLite     Agent -->|Optional minimal/redacted content| LLM     LLM -->|Classification| Agent     Agent -->|Notification| Telegram     Telegram -->|Feedback/commands| Agent     Agent -->|Store feedback| SQLite

---

# External Integrations

## Email Providers

Initial:

- IMAP

Future:

- Gmail API
- Microsoft Graph

## LLM Providers

Initial:

- Anthropic Claude
- OpenAI

Future:

- Google Gemini
- Perplexity
- Local models
- OpenAI-compatible APIs

## Telegram

Initial:

- Outgoing notifications

Future:

- Commands
- Feedback buttons
- Reply workflow

---

# Security and Privacy Notes

The system must:

- Avoid storing raw email bodies by default
- Avoid sending attachments to LLMs
- Log all LLM usage locally
- Support metadata-only mode
- Support redacted-body mode
- Require explicit user confirmation before sending replies

---

# MVP Architecture Scope

The MVP architecture should support future growth but implement only:

- Local config
- SQLite
- One or more IMAP accounts (one scheduler goroutine per enabled account)
- Email polling
- Telegram notifications
- Basic rule-based filtering
- No automatic replies
- No cloud backend
```

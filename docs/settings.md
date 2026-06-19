# Settings Reference

All application settings are stored in the encrypted SQLite database.

Initial setup via interactive wizard:
```
email-agent init
```

Update any setting after setup:
```
email-agent config set <key> <value>
```

---

## Logging

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `log.level` | `debug` `info` `warn` `error` | `info` | Verbosity of structured log output |
| `dev_mode` | `true` `false` | `false` | Colorized terminal output and more verbose logging |

---

## Email Accounts

Accounts are **not** stored as `config` keys. They live in a dedicated table and
are managed with the `account` subcommands (the database may hold any number of
accounts, each polled independently):

```
email-agent account add                 # interactive add
email-agent account list                # show all accounts
email-agent account edit    <email|name>
email-agent account remove  <email|name>
email-agent account enable  <email|name>
email-agent account disable <email|name>
```

Each account has these fields (prompted by `account add`/`edit`):

| Field | Values | Default | Description |
|-------|--------|---------|-------------|
| Name | string | — | Human-readable label; shown in notifications and log output |
| Email | email address | — | Account address; also the account identifier |
| Host | hostname | — | IMAP server hostname |
| Port | integer | `993` | IMAP server port |
| Username | string | email | IMAP login username (defaults to the email address) |
| Password | string | — | IMAP password — stored in the encrypted database |
| TLS | `true` `false` | `true` | Whether to use TLS; disable only for local/test servers |
| Poll interval | duration | `1m` | How often to poll for new messages (e.g. `30s`, `1m`, `5m`) |
| Auth type | `password` `oauth` | `password` | `password` = static IMAP password; `oauth` = Google OAuth (Gmail) |
| Enabled | `true` `false` | `true` | Disabled accounts are skipped by the daemon |

`email-agent init` configures the first account; use `account add` for more.

### Gmail via OAuth

For an `oauth` account, you do **not** enter a password. Instead:

1. Configure the global Google client once: `email-agent init oauth` (stores the
   keys below). See `docs/stages/rollout/008-01-gmail-oauth-setup.md` for the
   Google Cloud Console steps.
2. `email-agent account add` → choose auth type `oauth`. The CLI opens a browser
   for consent and stores a refresh token on the account (host/port default to
   `imap.gmail.com` / `993`).

The daemon then logs in over IMAP with XOAUTH2 and refreshes the access token
automatically. If the refresh token is revoked or expires, re-run
`email-agent account add` (or `account edit <email>`) to re-authorize.

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `oauth.google.client_id` | string | — | Google OAuth Desktop-app client ID (shared by all Gmail accounts) |
| `oauth.google.client_secret` | string | — | Google OAuth client secret — stored encrypted, masked in `config list` |

---

## Telegram

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `telegram.bot_token` | string | — | Bot token from @BotFather — stored encrypted |
| `telegram.chat_id` | integer | — | Numeric ID of the chat or channel to send notifications to |

---

## Notifications

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `notification.min_importance` | `critical` `important` `maybe` | `important` | Minimum importance level that triggers a Telegram notification |

### Importance levels

| Level | Score | Default behaviour |
|-------|-------|-------------------|
| `critical` | 90–100 | Notify |
| `important` | 70–89 | Notify |
| `maybe` | 30–69 | Skip |
| `ignore` | 0–29 | Skip |

Emails below the threshold are stored in the database with `status=ignored` and logged at `info` level, but no Telegram message is sent.

---

## LLM Classification (optional)

Configure via `email-agent init llm`. LLM is disabled when `llm.provider` is empty.

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `llm.provider` | `anthropic` `openai` `` | `` (disabled) | LLM provider; empty disables LLM |
| `llm.anthropic.api_key` | string | — | Anthropic API key — stored encrypted |
| `llm.openai.api_key` | string | — | OpenAI API key — stored encrypted |
| `llm.model` | string | — | Model override; uses provider default when empty |
| `llm.score_divergence_warn` | integer | `30` | Log WARN when LLM and rule-based scores differ by this much |

Provider defaults: Anthropic → `claude-sonnet-4-6`, OpenAI → `gpt-4o-mini`.

---

## Content

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `content.mode` | `headers_only` `redacted_body` `full_body` | `headers_only` | What is sent to the LLM; `redacted_body` strips PII before sending; `full_body` sends the complete plain-text body (truncated to 3 000 chars) |

Only relevant when `llm.provider` is set. Has no effect otherwise.

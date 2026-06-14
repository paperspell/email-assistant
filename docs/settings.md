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

## IMAP Account

| Key | Values | Default | Description |
|-----|--------|---------|-------------|
| `account.name` | string | — | Human-readable label used in log output |
| `account.email` | email address | — | Account address; also used as the account identifier |
| `account.imap.host` | hostname | — | IMAP server hostname |
| `account.imap.port` | integer | `993` | IMAP server port |
| `account.imap.username` | string | — | IMAP login username (usually the email address) |
| `account.imap.password` | string | — | IMAP password — stored encrypted |
| `account.imap.tls` | `true` `false` | `true` | Whether to use TLS; disable only for local/test servers |
| `account.poll_interval` | duration | `1m` | How often to poll for new messages (e.g. `30s`, `1m`, `5m`) |

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

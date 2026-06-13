# Email Agent

A local-first email monitoring daemon written in Go.

Monitors your email accounts, detects new incoming emails, and sends Telegram notifications — running entirely on your machine with no cloud backend required.

## Features

- Monitors IMAP email accounts
- Sends Telegram notifications for new emails
- Stores all state locally in SQLite
- Single binary, no external services required
- Privacy-first: email bodies not stored by default

## Requirements

- Go 1.26+
- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- An IMAP-enabled email account

## Installation

```bash
git clone https://github.com/paperspell/email-assistant
cd email-assistant
make build
```

The binary is written to `bin/email-agent`.

## Configuration

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml` with your IMAP server and Telegram settings. Sensitive values can be passed via environment variables instead of the config file:

| Environment variable | Description |
|----------------------|-------------|
| `IMAP_PASSWORD` | IMAP account password |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `LOG_LEVEL` | Log level: debug, info, warn, error |
| `DB_PATH` | Path to SQLite database file |

## Usage

```bash
# Start the daemon
email-agent run --config config.yaml

# Print version
email-agent version
```

## Development

```bash
make setup          # install prerequisites (macOS)
make test           # run unit tests
make test-migrations # run migration tests
make lint           # run linter
make check          # lint + test + migrations
```

See [AGENTS.md](AGENTS.md) for architecture overview and documentation index.

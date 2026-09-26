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

- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- An IMAP-enabled email account — for Gmail, an [app password](https://myaccount.google.com/apppasswords)
- An API key for the classifier: Anthropic, OpenAI or Gemini

## Installation

Pick one.

**Binary** — download the archive for your platform from the
[Releases](https://github.com/paperspell/email-assistant/releases) page, unpack it and put
`email-agent` on your `PATH`:

```bash
tar -xzf email-agent_*_linux_amd64.tar.gz
install -m 755 email-agent ~/.local/bin/
```

**Docker** — the container runs as an unprivileged user and keeps its database in a named volume:

```bash
cp .env.example .env
docker compose run --rm email-agent init      # prints EMAIL_AGENT_KEY once — paste it into .env
docker compose run --rm email-agent account add
docker compose up -d
```

**From source** — needs Go 1.26+:

```bash
git clone https://github.com/paperspell/email-assistant
cd email-assistant
make build                                    # writes bin/email-agent
```

## Setup

Run the interactive setup wizard once to create and configure the encrypted database:

```bash
email-agent init
```

The wizard will ask for your IMAP account and Telegram credentials. All settings are stored in an Adiantum-encrypted SQLite database at `~/.email-agent/email-agent.db`. The encryption key is saved to your OS keychain automatically.

On headless Linux servers where no keychain is available, the wizard will print the key for you to set as the `EMAIL_AGENT_KEY` environment variable.

## Usage

```bash
# Start the daemon
email-agent run

# Update a setting
email-agent config set account.poll_interval 2m
email-agent config set log.level debug

# Override database path
email-agent --db /custom/path/db.sqlite run

# Print version
email-agent version
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `EMAIL_AGENT_DB` | Database path override |
| `EMAIL_AGENT_KEY` | Encryption key (headless Linux fallback) |
| `LOG_LEVEL` | Log level override: debug, info, warn, error |

## Development

```bash
make setup          # install prerequisites (macOS)
make test           # run unit tests
make test-migrations # run migration tests
make lint           # run linter
make check          # lint + test + migrations
```

See [AGENTS.md](AGENTS.md) for architecture overview and documentation index.

## License

MIT — see [LICENSE](LICENSE).

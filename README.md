# Email Agent

A self-hosted email agent written in Go. It watches your inboxes and decides, mail by mail, what deserves
your attention now: important mail reaches your Telegram within minutes, everything else waits for an
evening digest.

It runs on your own machine or server, with no backend of its own: your mail goes only to the model
provider you pick and to your Telegram.

## Features

- **Triage, not forwarding.** Your own rules first, then a model sorts each email into critical, important,
  maybe or ignore, with a category and a short summary
- **Telegram notifications** for what matters, with buttons to mark it handled, open details, or teach the
  agent to ignore that sender, domain, list or kind of subject
- **Daily digest** of everything that was not worth a ping, marked read in one tap
- **Focus mode** for a busy work inbox: only mail addressed to you or mentioning you — including GitHub and
  GitLab review requests, mentions, and people's comments on your pull requests
- **Plain-language ignore rules**, e.g. "promotions unless they concern an order I placed"
- **Your choice of model:** Anthropic, OpenAI, Gemini or Jev (TypeSafe), and of how much of the body it may
  see — headers only, redacted, or full
- **Several mailboxes**, IMAP with a password or Google OAuth; each can report to its own Telegram chat, so
  one installation can serve friends or family
- **Bot messages and summaries in 12 languages**
- **Encrypted local storage** (SQLite with Adiantum); email bodies are not stored by default
- A Telegram alert when Gemini runs out of prepaid credits

## Requirements

- A Telegram bot token (from [@BotFather](https://t.me/BotFather))
- An IMAP-enabled email account — for Gmail, an [app password](https://myaccount.google.com/apppasswords)
- An API key for the classifier: Anthropic, OpenAI, Gemini or Jev (TypeSafe). Jev only decides
  what matters — its notifications and digest entries carry no summary

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
email-agent config set poll.default_interval 2m
email-agent config set log.level debug

# Override database path
email-agent --db /custom/path/db.sqlite run

# Print version
email-agent version
```

## Run as a service

On Linux, install the daemon as a systemd **user** unit — no root needed:

```bash
export EMAIL_AGENT_KEY=...            # the key 'email-agent init' printed, if not already in your shell
email-agent service install --start
loginctl enable-linger "$USER"        # keep it running after logout and start it at boot
journalctl --user -u email-agent -f
```

`install` writes `~/.config/systemd/user/email-agent.service` pointing at the binary that ran it, and
`~/.config/email-agent/env` (0600) holding the key. Re-running it never overwrites an existing key file.
`email-agent service uninstall` stops the service and removes the unit; the database and key stay.

For a system-wide install with the full sandboxing set, see [`contrib/email-agent.service`](contrib/email-agent.service).

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

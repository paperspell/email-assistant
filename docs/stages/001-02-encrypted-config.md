# 001-02-encrypted-config.md

Status: Draft
Version: 0.1

# Stage 001-02 — Encrypted SQLite + Config in DB + Init Wizard

## Goal

Replace `modernc.org/sqlite` with `github.com/ncruces/go-sqlite3` and enable Adiantum full-database encryption. Move all configuration from `config.yaml` into a `settings` table in SQLite. Add an interactive `init` wizard and a `config set` command. After this stage there is no config file — everything lives in the encrypted database.

---

## What Changes

| Before | After |
|--------|-------|
| `modernc.org/sqlite` | `github.com/ncruces/go-sqlite3` |
| Plaintext SQLite DB | Adiantum-encrypted SQLite DB |
| `config.yaml` | `settings` table in DB |
| `--config` flag on `run` | `--db` flag on all commands |
| No init step | `email-agent init` wizard |
| No config update | `email-agent config set key value` |

---

## DB Path Resolution

All commands resolve the DB path in this order:

1. `--db` flag
2. `EMAIL_AGENT_DB` env var
3. Default: `~/.email-agent/email-agent.db`

The `~/.email-agent/` directory is created if it does not exist.

---

## Encryption

Full-database encryption via `github.com/ncruces/go-sqlite3/vfs/adiantum`.

### Key management

The encryption key is a random 32-byte value generated once during `init`, base64-encoded for storage.

Resolution order at startup:

1. OS keychain (`github.com/zalando/go-keyring`) — service: `email-agent`, user: `db-key`
2. `EMAIL_AGENT_KEY` env var (base64-encoded) — fallback for headless Linux servers
3. Exit with clear error message if neither is available

On `init`:
- Generate random 32-byte key
- Attempt to save to OS keychain
- If keychain unavailable: print the key and instruct the user to set `EMAIL_AGENT_KEY`

---

## Settings Table

```sql
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);
```

### Setting keys

| Key | Type | Description |
|-----|------|-------------|
| `account.name` | string | Display name |
| `account.email` | string | Email address (also used as account ID) |
| `account.imap.host` | string | IMAP hostname |
| `account.imap.port` | integer | IMAP port |
| `account.imap.username` | string | IMAP username |
| `account.imap.password` | string | IMAP password (protected by DB encryption) |
| `account.imap.tls` | bool | Use TLS |
| `account.poll_interval` | duration | Poll interval (e.g. `1m`, `30s`) |
| `telegram.bot_token` | string | Telegram bot token (protected by DB encryption) |
| `telegram.chat_id` | integer | Telegram chat ID |
| `log.level` | string | Log level: debug, info, warn, error |
| `dev_mode` | bool | Colorized terminal logging |

Passwords and tokens are stored as plaintext strings in the DB — they are protected by the Adiantum encryption layer, not by additional field-level encryption.

---

## CLI Changes

### Persistent root flag

```
email-agent [--db path]
```

`--db` is available on all subcommands.

### New: `email-agent init`

Interactive wizard. Runs once to set up the DB from scratch.

```
email-agent init [--db path]

Setting up Email Agent.

An encryption key will be generated and saved to your OS keychain.
If the keychain is unavailable, you will be asked to store it manually.

IMAP account
  Name:              My Email
  Email:             user@example.com
  Host:              imap.example.com
  Port [993]:
  Username:          user@example.com
  Password:          ········
  TLS [true]:
  Poll interval [1m]:

Telegram
  Bot token:         ········
  Chat ID:           123456789

Initializing encrypted database at ~/.email-agent/email-agent.db...
Done. Run 'email-agent run' to start.
```

Behaviour:
- Creates the DB directory if needed
- Generates and stores the encryption key
- Opens the encrypted DB, runs migrations
- Saves all settings
- Fails with a clear message if DB already exists and is initialized (prompt to use `config set` instead)

### New: `email-agent config set <key> <value>`

Updates a single setting.

```
email-agent config set account.poll_interval 2m
email-agent config set log.level debug
email-agent config set telegram.chat_id 987654321
```

Password and token values are masked in log output. The command re-validates the affected section after saving.

### Changed: `email-agent run`

- Removes `--config` / `-c` flag
- Adds `--db` flag (inherited from root)
- On startup: if DB does not exist or settings are missing → exit with message: `database not initialized — run 'email-agent init' first`

### Removed

- `email-agent auth gmail` placeholder (not implemented yet)

---

## Directory Structure Changes

```
internal/
  db/
    db.go                     # open encrypted DB via ncruces + adiantum VFS
    migrations/
      003_settings.sql        # add settings table
    repo/
      settings_repo.go        # Get, Set, GetAll
      settings_repo_test.go
  config/
    config.go                 # load Config struct from SettingsRepo (not YAML)
    config_test.go            # updated tests
  auth/
    keychain/
      keychain.go             # save/load encryption key via go-keyring + env fallback
cmd/
  email-agent/
    main.go                   # add --db persistent flag, remove --config
    cmd_init.go               # init wizard
    cmd_config.go             # config set subcommand
```

### Removed

```
config.example.yaml           # replaced by init wizard
internal/config/config.go     # YAML loading removed, rewritten to load from DB
```

---

## Tasks

### T1 — Driver switch

Replace `modernc.org/sqlite` with `github.com/ncruces/go-sqlite3`.

- `internal/db/db.go`: change import, register Adiantum VFS, pass key when opening DB
- All other packages unchanged — both drivers implement `database/sql`

Driver registration:
```go
import (
    "github.com/ncruces/go-sqlite3/driver"
    _ "github.com/ncruces/go-sqlite3/embed"
    "github.com/ncruces/go-sqlite3/vfs/adiantum"
)

func init() {
    adiantum.Register()
}
```

Open with key:
```go
db, err := sql.Open("sqlite3", fmt.Sprintf(
    "file:%s?vfs=adiantum&_key=%s", path, base64Key,
))
```

---

### T2 — Key management

`internal/auth/keychain/keychain.go`

```go
const service = "email-agent"
const keyringUser = "db-key"

// Load returns the encryption key from keychain or EMAIL_AGENT_KEY env var.
// Returns an error if neither is available.
func Load() (string, error)

// Save stores the encryption key in the OS keychain.
// Returns false if keychain is unavailable (headless Linux).
func Save(key string) (bool, error)

// Generate creates a new random 32-byte key, base64-encoded.
func Generate() (string, error)
```

---

### T3 — Migration: 003_settings.sql

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS settings;
```

---

### T4 — SettingsRepo

`internal/db/repo/settings_repo.go`

```go
type SettingsRepo struct { db *sql.DB }

func (r *SettingsRepo) Get(ctx context.Context, key string) (string, error)
// Get returns "", nil if key does not exist

func (r *SettingsRepo) Set(ctx context.Context, key, value string) error

func (r *SettingsRepo) GetAll(ctx context.Context) (map[string]string, error)
```

---

### T5 — Config loading from DB

`internal/config/config.go` — rewrite `Load` to read from `SettingsRepo` instead of YAML.

```go
func Load(ctx context.Context, repo *repo.SettingsRepo) (*Config, error)
```

`Config` struct is unchanged. `validate()` is unchanged. `applyEnvOverrides` is removed — env vars only control DB path and encryption key, not application settings.

Remove:
- `gopkg.in/yaml.v3` usage
- YAML struct tags
- `os.ReadFile` config path logic

---

### T6 — Init wizard

`cmd/email-agent/cmd_init.go`

Steps:
1. Resolve DB path
2. Check DB does not already exist — if it does, exit with message
3. Generate encryption key, save to keychain (or print for manual storage)
4. Create DB directory, open encrypted DB, run migrations
5. Prompt for each setting in order (use `golang.org/x/term` for password fields)
6. Validate (same rules as `config.validate()`)
7. Save all settings via `SettingsRepo`
8. Print success message

---

### T7 — Config set command

`cmd/email-agent/cmd_config.go`

```
email-agent config set <key> <value>
```

Steps:
1. Load encryption key, open DB
2. Validate key is a known setting key — reject unknown keys
3. Save via `SettingsRepo.Set`
4. Print confirmation

---

### T8 — Update run command

- Remove `--config` / `-c` flag
- On startup: call `SettingsRepo.GetAll`, check required keys are present
- If DB missing or empty settings: exit with `run 'email-agent init' first`

---

### T9 — Remove config.yaml support

- Delete `config.example.yaml`
- Remove `gopkg.in/yaml.v3` from go.mod
- Remove `github.com/caarlos0/env/v10` from go.mod
- Update `README.md` and `AGENTS.md`
- Update `docs/dependencies.md`

---

### T10 — Dependencies

Add:
```
github.com/ncruces/go-sqlite3@latest
github.com/zalando/go-keyring@latest
golang.org/x/term@latest
```

Remove:
```
modernc.org/sqlite
gopkg.in/yaml.v3
github.com/caarlos0/env/v10
```

---

## Tests

| Package | What to test |
|---------|--------------|
| `internal/auth/keychain` | `Generate` returns non-empty base64; `Load` returns error when keychain empty and env var unset; `Load` returns env var value when set |
| `internal/db/repo` | `SettingsRepo.Get` returns `""` for missing key; `Set` then `Get` round-trip; `GetAll` returns all keys |
| `internal/config` | Load from populated settings; missing required key returns error; unknown poll\_interval format returns error |
| `internal/db` | Encrypted DB opens with correct key; fails to open with wrong key |

---

## Migration from Stage 001-01

Users who set up Stage 1 with `config.yaml`:

1. Run `email-agent init` — creates the new encrypted DB
2. Delete `config.yaml` and the old `email-agent.db`

No automated migration. The init wizard re-collects all settings interactively.

---

## Definition of Done

1. `make check` passes
2. `email-agent init` creates an encrypted DB, stores all settings, saves key to keychain
3. `email-agent run` starts the daemon using config from DB
4. `email-agent config set account.poll_interval 2m` updates the setting
5. Opening the DB file with a SQLite browser without the key shows only encrypted bytes
6. On headless Linux with `EMAIL_AGENT_KEY` set: full flow works without keychain
7. IMAP polling and Telegram notifications work identically to Stage 001-01

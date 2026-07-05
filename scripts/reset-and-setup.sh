#!/usr/bin/env bash
#
# reset-and-setup.sh — wipe the local email-agent database and bootstrap a fresh
# install, configured for one or more Gmail accounts (OAuth by default).
#
# It configures global settings first, then adds mailboxes:
#   1. `init oauth`        stores the Google Client ID + secret (creates the DB).
#   2. `init llm`          picks the LLM provider + settings (Enter to disable).
#   3. `init telegram`     bot token + chat id.
#   4. `init notifications` min importance.
#   5. `account add`       once per mailbox (browser consent each time).
#
# Section commands create the encrypted DB on first use, so OAuth/LLM are set up
# before any mailbox — no need to run the full `init` wizard first.
#
# Each `email-agent` command below is itself interactive — the script only
# enforces order and pauses between steps.
#
# Everything is configured through options (run with --help). The database path
# is passed to email-agent via --db, so the same path drives the wipe and every
# subsequent command.

set -euo pipefail

# --- defaults -----------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN="$REPO_ROOT/bin/email-agent"

DB_PATH="${EMAIL_AGENT_DB:-$HOME/.email-agent/email-agent.db}"
KEEP_DB=0      # --keep-db:  do not wipe the database
REMOVE_KEY=1   # --keep-key: reuse the existing Keychain key (init won't mint one)
NO_BACKUP=0    # --no-backup: skip the backup prompt before deleting

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Wipe the local email-agent database and bootstrap a fresh install.

Options:
  -d, --db PATH     Database path (default: \$EMAIL_AGENT_DB or
                    ~/.email-agent/email-agent.db). Passed to email-agent via --db.
      --keep-db     Do not delete the database; just (re)configure.
      --keep-key    Keep the Keychain encryption key so init reuses it
                    (a fresh DB stays readable by existing backups).
      --new-key     Delete the Keychain key so init mints a new one (default).
      --no-backup   Do not offer to back up the DB before deleting it.
  -h, --help        Show this help and exit.
EOF
}

# --- parse options ------------------------------------------------------------

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--db)      DB_PATH="${2:?--db requires a path}"; shift 2 ;;
    --db=*)       DB_PATH="${1#*=}"; shift ;;
    --keep-db)    KEEP_DB=1; shift ;;
    --keep-key)   REMOVE_KEY=0; shift ;;
    --new-key)    REMOVE_KEY=1; shift ;;
    --no-backup)  NO_BACKUP=1; shift ;;
    -h|--help)    usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# email-agent invocation bound to the chosen DB path.
agent() { "$BIN" --db "$DB_PATH" "$@"; }

info()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
warn()  { printf '\033[33m!  \033[0m %s\n' "$*"; }
pause() { read -r -p "$(printf '\033[35m?  %s \033[0m' "$1")" _ || true; }
confirm() {
  local ans
  read -r -p "$(printf '\033[35m?  %s [y/N] \033[0m' "$1")" ans || true
  ans="${ans//$'\r'/}" # some terminals send a trailing CR that read keeps
  [[ "$ans" == "y" || "$ans" == "Y" || "$ans" == "yes" ]]
}

# --- 0. stop a running daemon -------------------------------------------------

if pgrep -f 'email-agent run' >/dev/null 2>&1; then
  info "Stopping running email-agent daemon..."
  pkill -f 'email-agent run' || true
  sleep 1
fi

# --- 1. wipe the old database -------------------------------------------------

if [[ "$KEEP_DB" -eq 0 ]]; then
  if [[ -f "$DB_PATH" ]]; then
    warn "This will DELETE the database at: $DB_PATH"
    if [[ "$NO_BACKUP" -eq 0 ]] && confirm "Back it up to ${DB_PATH}.bak before deleting?"; then
      cp "$DB_PATH" "${DB_PATH}.bak"
      info "Backup written to ${DB_PATH}.bak"
    fi
    if ! confirm "Delete $DB_PATH now?"; then
      warn "Aborted."
      exit 1
    fi
    rm -f "$DB_PATH"
    info "Deleted $DB_PATH"
  else
    info "No existing database at $DB_PATH — nothing to delete."
  fi

  # Encryption key. With --keep-key we leave it so init reuses it (backups stay
  # readable). By default we remove it so init mints a fresh key.
  if [[ "$REMOVE_KEY" -eq 1 && "$(uname)" == "Darwin" ]]; then
    if security find-generic-password -s email-agent -a db-key >/dev/null 2>&1; then
      security delete-generic-password -s email-agent -a db-key >/dev/null 2>&1 || true
      info "Cleared old encryption key from Keychain (init will mint a new one)."
    fi
  elif [[ "$REMOVE_KEY" -eq 0 ]]; then
    info "--keep-key: leaving the Keychain key in place (init will reuse it)."
  fi
else
  info "--keep-db: leaving the existing database in place."
fi

# --- 2. build -----------------------------------------------------------------

info "Building bin/email-agent..."
( cd "$REPO_ROOT" && make build )

# --- 3. Google Cloud reminder -------------------------------------------------

cat <<'EOF'

Before continuing you need a Google OAuth *Desktop app* client
(Client ID + Client secret). One-time setup, ~10 min:
  docs/stages/rollout/008-01-gmail-oauth-setup.md

If you would rather use Gmail App Passwords (auth type = password) instead of
OAuth, you can add those accounts with `account add` and skip `init oauth`.

EOF
pause "Press Enter once you have your Client ID + secret ready (or to continue with App Passwords)..."

# --- 4. OAuth client (creates the DB on first use) ----------------------------

if confirm "Configure the Google OAuth client now (needed for Gmail OAuth)?"; then
  info "Configuring Google OAuth client..."
  agent init oauth
fi

# --- 5. LLM classification ----------------------------------------------------

info "Configuring LLM classification (press Enter at the provider prompt to disable)..."
agent init llm

# --- 6. Telegram + notifications ----------------------------------------------

info "Configuring Telegram..."
agent init telegram

info "Configuring notifications..."
agent init notifications

# --- 7. add mailboxes ---------------------------------------------------------

info "Now add your mailboxes. A browser opens for consent on each OAuth (Gmail) account."
while confirm "Add a new mailbox now?"; do
  agent account add
done

info "Configured accounts:"
agent account list || true

# --- 8. done ------------------------------------------------------------------

cat <<EOF

Done. Start the daemon with:
  $BIN --db "$DB_PATH" run

Re-authorize a mailbox later (e.g. when a refresh token expires):
  $BIN --db "$DB_PATH" account edit <email>
EOF

# settings-ui.md

Status: Draft
Version: 0.1

# Settings UI

## Overview

Configuration today is edited through the interactive CLI (`init`, `account`,
`rules`, `clauses`, `config set`) and, increasingly, through Telegram inline
menus (`internal/telegram/menu.go`, `handler_menu.go`, the rule menus from stage
009-03). This document sketches how to add a richer settings UI.

The central observation: settings already have a **model** — repositories over
the encrypted SQLite database (`SettingsRepo`, `AccountRepo`, `RuleRepo`,
`ClauseRepo`) consumed by `config.Load`. Any UI is just another front-end over
those same repositories. The question is which surface to add, not how to store
data.

---

## Design principles

- **One source of truth.** Every UI reads and writes the existing repos. No
  duplicated persistence or validation logic.
- **Secrets are write-only.** Telegram bot token, LLM API keys, OAuth client
  secret, IMAP passwords, and OAuth refresh tokens are stored encrypted and must
  never be echoed back. Follow the CLI pattern: masked fields with an "Enter to
  keep unchanged" semantic (`promptPassword`).
- **Local-first.** No configuration leaves the machine. A UI that exposes
  secrets must stay bound to the local host.

---

## Option 1 — Telegram menus (already in progress)

Extend the existing inline-menu surface.

- **Pros:** the bot is already the primary interface; works from any device; no
  new binary, port, or auth; the menu infrastructure exists.
- **Best for:** non-secret toggles and thresholds — min importance, enable/
  disable an account, poll interval, enable/disable a rule or clause, trigger a
  digest.
- **Hard limit:** **secrets cannot go through Telegram.** API keys, passwords,
  and the OAuth client secret would be persisted on Telegram's servers. Those
  stay in the CLI or a local UI.

Verdict: keep growing this for quick, non-secret adjustments.

---

## Option 2 — Local web UI served by the daemon (recommended for full settings)

The daemon serves an HTTP UI on `127.0.0.1:<port>` and embeds its static assets
into the binary via `embed`, preserving the single-binary property.

- **Why it fits:**
  - Local-first — secrets travel over loopback only, not a third party.
  - The **OAuth consent flow is already a localhost flow** (`oauth.Consent`
    opens a browser and listens on localhost for the redirect). An
    "Authorize with Google" button fits naturally, removing a separate CLI step.
  - Rich enough for accounts, rules, clauses, and LLM/content settings in one
    place.
- **Keep the stack small:** Go `html/template` + htmx avoids a JS build step.
  Masked secret fields mirror the CLI ("leave blank to keep unchanged").
- **Prior art:** Syncthing, Tailscale, and Pi-hole all pair a background daemon
  with a localhost dashboard.
- **Security:** bind strictly to `127.0.0.1`, add a CSRF token, and consider a
  first-run pairing token so other local users can't reach it.

Verdict: the best home for full configuration, including secrets and OAuth.

---

## Option 3 — Native desktop / tray app

For a "real application" feel, and a natural pairing with installers (see
[packaging-installers.md](packaging-installers.md)):

- **Wails** — Go backend + OS webview frontend (Electron-like, lighter); reuses
  the Go logic directly.
- **Fyne** — pure-Go native widgets, but pulls in OpenGL/cgo, which breaks the
  current CGO-free build story.
- **Tray/menubar** — a small icon with Start/Stop and "Open settings", opening
  either the web UI (Option 2) or a native window.

Verdict: nice for a packaged desktop product; more surface area to maintain.

---

## Recommendation

Combine **Telegram menus for quick non-secret toggles** (already underway) with
a **localhost web UI for full settings and secrets**. Both are thin front-ends
over the same repositories, so there is no logic to duplicate. A tray app can
come later as a shell around the web UI if a desktop product is pursued.

---

## The hard part: runtime reconfiguration

The non-obvious challenge is not the UI — it is applying changes to a running
daemon.

Today `main.go` calls `config.Load` **once** at startup and builds the
per-account schedulers, providers, and digest jobs a single time. A settings UI
implies changes should take effect without a manual restart.

Two approaches:

1. **Save + restart (v1).** The UI writes to the DB and prompts the user to
   restart the daemon. Simple and correct; poor UX for frequent edits.
2. **Hot reload (v2).** The daemon watches for config changes (signal, channel,
   or DB version bump), re-runs `config.Load`, and reconciles the running
   `errgroup` of schedulers — starting newly added/enabled accounts, stopping
   removed/disabled ones, and applying changed intervals or thresholds.

Hot reload is the substantive design work — comparable in scope to the service
lifecycle discussed in [packaging-installers.md](packaging-installers.md).
Start with save-and-restart, and design reload as a follow-up.

---

## Settings surface (reference)

The UI would cover the keys already defined in [settings.md](settings.md) and
the account/rule/clause models:

- **Telegram:** bot token (secret), chat ID.
- **Notifications:** min importance.
- **LLM:** provider, API key (secret), model override, content mode.
- **OAuth:** Google client ID, client secret (secret), per-account authorization.
- **Accounts:** add/edit/remove, enable/disable, host/port/TLS, poll interval,
  auth type, digest time.
- **Rules & clauses:** per-account ignore/allow rules and LLM ignore clauses.

---

## Open questions

- Port selection and conflict handling for the local web server.
- Local auth model (pairing token vs. none) and CSRF strategy.
- Whether the web UI is always-on with the daemon or launched on demand.
- How OAuth re-authorization surfaces in the UI when a refresh token expires.

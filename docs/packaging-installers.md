# packaging-installers.md

Status: Draft
Version: 0.1

# Packaging & Installers (Windows / macOS)

## Overview

Email Agent is distributed today as a single Go binary built with `make build`
(`bin/email-agent`). This document sketches how to turn that binary into
user-facing installers for macOS and Windows, and what work sits between "a
binary exists" and "a user double-clicks an installer and has a running agent".

The good news is the dependency stack is **CGO-free**, which makes
cross-compilation and CI simple.

---

## Why this is tractable: no cgo

| Concern | Library | Notes |
|---------|---------|-------|
| SQLite | `github.com/ncruces/go-sqlite3` | SQLite compiled to WASM, run via wazero — pure Go, no C toolchain |
| DB encryption | `lukechampine.com/adiantum` | Pure Go |
| OS keychain | `github.com/zalando/go-keyring` | macOS shells out to `security`; Windows uses Credential Manager (`wincred`); Linux uses D-Bus — no cgo |

Because nothing links C, every target can be cross-compiled from a single
machine (`GOOS`/`GOARCH`), and CI does not need native cross toolchains. Native
runners are still useful for **signing/notarization**, not for building.

---

## Recommended tooling: GoReleaser

A single `.goreleaser.yaml` can produce all artifacts from one build:

- Cross-compiled binaries: `darwin/amd64`, `darwin/arm64`, `windows/amd64`
  (and `linux/*` for parity).
- Archives (`.tar.gz`, `.zip`) with checksums.
- **macOS:** a Homebrew tap formula (the idiomatic path for a CLI) and,
  optionally, a `.pkg`/`.dmg`.
- **Windows:** a Scoop manifest and/or an installer (Inno Setup or WiX/MSI).
- GitHub Release with generated changelog.

Rough effort: a "download-and-run" release is an evening of work. Signed
installers with a service lifecycle are a few days, most of it spent on signing
and service integration rather than code.

---

## Per-platform notes

### macOS

- **Distribution options (easiest → richest):** Homebrew tap → `.pkg` → `.dmg`.
- **Gatekeeper:** without an Apple **Developer ID** signature and
  **notarization** (Apple Developer Program, ~$99/yr), first launch shows an
  "unidentified developer" warning. The binary still runs, but the UX suffers.
- **Service lifecycle:** register with `launchd` (a per-user **LaunchAgent** is
  the natural fit for a personal agent).
- **Paths:** encrypted DB defaults to `~/.email-agent/email-agent.db`; the
  encryption key lives in the login Keychain.

### Windows

- **Distribution options:** Scoop manifest, or a real installer via **Inno
  Setup** (simple) or **WiX/MSI** (enterprise-friendly).
- **SmartScreen:** without an Authenticode code-signing certificate, first run
  shows a SmartScreen warning. Same story as Gatekeeper.
- **Service lifecycle:** register as a **Windows Service**.
- **Paths:** DB under `%USERPROFILE%\.email-agent\`; the encryption key lives in
  Windows Credential Manager (already handled by `go-keyring`).

---

## The real work: daemon lifecycle, not packaging

Today the agent runs in the foreground via `email-agent run`. An installer
implies the agent should install as a background service with autostart. This is
the substantive part of the effort — the packaging itself is mechanical.

- Consider `github.com/kardianos/service`, which abstracts launchd / Windows
  Service / systemd behind one API and can add commands like
  `email-agent service install|start|stop`.
- The installer would simply invoke those commands post-install.
- Design questions to settle: log destination and rotation, restart-on-crash,
  required privileges (per-user vs system), and where the encrypted DB and key
  live on each OS.

The encryption key story is already cross-platform: Keychain (macOS) and
Credential Manager (Windows) are picked up automatically, with the
`EMAIL_AGENT_KEY` environment variable as a headless fallback.

---

## CI matrix

- Building: a single `ubuntu` runner suffices (no cgo).
- Signing/notarization: use native `macos-latest` and `windows-latest` runners,
  since the signing tools are platform-specific.
- Secrets required in CI: Apple Developer ID cert + notarization credentials;
  Windows Authenticode certificate.

---

## Suggested rollout

1. **v0 — bare binaries.** GoReleaser cross-compiles and publishes archives +
   checksums to GitHub Releases. Homebrew tap and Scoop manifest for CLI users.
2. **v1 — service integration.** Add `service install/start/stop` via
   `kardianos/service`; resolve logging/paths/permissions.
3. **v2 — signed installers.** Add signing + notarization; ship `.pkg`/`.dmg`
   and an Inno Setup/MSI installer that registers the service and autostarts it.

---

## Open questions

- Per-user vs system-wide install (affects where DB/key live and which service
  scope to use).
- Do we ship a tray/menubar affordance to start/stop the service and open
  settings? (See [settings-ui.md](settings-ui.md).)
- Auto-update strategy (Sparkle on macOS / winget/Scoop update / in-app check).

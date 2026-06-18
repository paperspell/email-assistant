# 008-01-gmail-oauth.md

Status: Draft
Version: 0.1

# Stage 008 — Gmail OAuth Backend

## Goal

Let a Gmail account authenticate with OAuth instead of a password. Gmail is
reached over the **existing IMAP client** using the **XOAUTH2** SASL mechanism;
the only new moving parts are obtaining and refreshing an OAuth access token.

This stage fills in the OAuth mechanics that Stage 7 deliberately deferred. The
`accounts.auth_type` discriminator and the `newProvider` factory `case "oauth"`
are already in place, so this is additive.

No Gmail REST API, no Microsoft Graph, no web server to host — the consent flow
uses a temporary `localhost` loopback listener started by the CLI itself.

---

## What Changes

| Before | After |
|--------|-------|
| Accounts authenticate with a static IMAP password only | Accounts may use `auth_type = "oauth"` |
| IMAP login uses `LOGIN user password` | OAuth accounts log in via `AUTHENTICATE XOAUTH2` with a bearer token |
| `accounts` has no token columns | `accounts` stores per-account refresh/access token + expiry |
| No Google client credentials | Global `oauth.google.client_id` / `client_secret` settings |
| `account add` only prompts for a password | `account add` with `auth_type=oauth` runs a browser consent flow |

---

## Credential Model

Two levels, mirroring how Google issues credentials:

| Credential | Scope | Storage | Entered |
|-----------|-------|---------|---------|
| Client ID / Client secret | Global (identifies the app to Google) | `settings` table | Once, via `email-agent init oauth` |
| Refresh token / Access token / expiry | Per account (grants mailbox access) | `accounts` table | Per mailbox, via the `account add` consent flow |

The Client ID/secret are the same for every Gmail mailbox; the tokens are
per-mailbox. The whole database is already encrypted at rest, so tokens need no
separate encryption.

### User-side prerequisite (outside this repo)

Documented for the user, not implemented here. In Google Cloud Console: create a
project, enable the Gmail API, configure the OAuth consent screen (scope
`https://mail.google.com/`, add self as test user), and create a **Desktop app**
OAuth client. The user brings back the Client ID and Client secret. In "Testing"
publishing mode Google expires refresh tokens after ~7 days — handled here by a
clear re-consent prompt (see T7), not worked around.

---

## Directory Structure Changes

```
internal/
  domain/
    account.go              # add OAuthRefreshToken, OAuthAccessToken, OAuthTokenExpiry
  db/
    migrations/
      008_oauth_tokens.sql  # NEW: add token columns to accounts
    repo/
      account_repo.go       # read/write the new token columns
  config/
    config.go               # OAuth global settings + validation
    keys.go                 # oauth.google.client_id / client_secret
  email/
    imap/
      client.go             # XOAUTH2 auth path when a token source is supplied
  auth/
    oauth/
      oauth.go              # NEW: Google config, consent (loopback) flow, persisting token source
      xoauth2.go            # NEW: minimal XOAUTH2 sasl.Client
      oauth_test.go         # NEW
cmd/email-agent/
  cmd_init.go               # NEW `init oauth` section (client id/secret)
  cmd_account.go            # auth_type prompt; oauth consent during add
  main.go                   # newProvider: build oauth token source for case "oauth"
```

---

## Tasks

### T1 — Token fields on the account

`internal/domain/account.go` — add to `Account`:

```go
OAuthRefreshToken string
OAuthAccessToken  string
OAuthTokenExpiry  time.Time
```

`AuthType == domain.AuthOAuth` (new const `"oauth"`) selects the OAuth path.

---

### T2 — Migration: 008_oauth_tokens.sql

```sql
-- +goose Up
ALTER TABLE accounts ADD COLUMN oauth_refresh_token TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN oauth_access_token  TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN oauth_token_expiry  DATETIME;

-- +goose Down
-- SQLite cannot drop columns pre-3.35; recreate without the token columns if
-- a rollback is required. Left as a no-op for forward-only deployments.
```

Existing password accounts get empty token columns — unaffected.

---

### T3 — Account repo: persist tokens

`internal/db/repo/account_repo.go` — extend `accountColumns`, the scan, and the
`Upsert` statement with the three token columns. Add a focused helper used by the
refreshing token source so a token refresh does not rewrite the whole row:

```go
func (r *AccountRepo) UpdateTokens(ctx context.Context, id string, t oauth.Tokens) error
```

Updates only `oauth_access_token`, `oauth_refresh_token`, `oauth_token_expiry`.

---

### T4 — Global OAuth settings

`internal/config/keys.go`:

```go
KeyOAuthGoogleClientID     = "oauth.google.client_id"
KeyOAuthGoogleClientSecret = "oauth.google.client_secret"
```

Add both to `KnownKeys`; `client_secret` is matched by the existing
`isSensitiveKey` ("secret") so `config list` masks it.

`internal/config/config.go` — new `OAuthConfig{GoogleClientID, GoogleClientSecret}`
on `Config`, populated in `applySettings`. Validation: if **any** loaded account
has `AuthType == oauth`, require both client id and secret:

```go
if hasOAuthAccount && (c.OAuth.GoogleClientID == "" || c.OAuth.GoogleClientSecret == "") {
    return fmt.Errorf("config: %s and %s are required for OAuth accounts — run 'email-agent init oauth'",
        KeyOAuthGoogleClientID, KeyOAuthGoogleClientSecret)
}
```

---

### T5 — OAuth package: consent flow + token source

`internal/auth/oauth/oauth.go`:

```go
// Config builds the golang.org/x/oauth2 config for Gmail IMAP access.
func GoogleConfig(clientID, clientSecret string) *oauth2.Config // scope https://mail.google.com/, google endpoint

// Consent runs the loopback authorization-code flow: starts a localhost
// listener, opens the browser, captures the code, exchanges it for tokens.
// access_type=offline + prompt=consent so a refresh token is always returned.
func Consent(ctx context.Context, cfg *oauth2.Config) (Tokens, error)

// TokenSource returns an oauth2.TokenSource seeded from stored tokens that
// refreshes the access token as needed and calls persist() whenever the token
// changes (so a rotated refresh token is written back to the DB).
func TokenSource(ctx context.Context, cfg *oauth2.Config, t Tokens,
    persist func(Tokens) error) oauth2.TokenSource
```

`Tokens{AccessToken, RefreshToken string; Expiry time.Time}`.

The loopback listener binds `127.0.0.1:0` (random free port), so the redirect
URI is `http://127.0.0.1:<port>/` — allowed for Desktop-app clients without any
console configuration. Browser opened with a small cross-platform `open`/`xdg-open`
helper; if it can't open, print the URL for manual paste.

`internal/auth/oauth/xoauth2.go` — `emersion/go-sasl` ships only OAUTHBEARER, so
add a ~15-line `sasl.Client` for XOAUTH2 (the mechanism Gmail documents):

```go
// ir = "user=" + email + "\x01auth=Bearer " + accessToken + "\x01\x01"
func NewXOAUTH2(username, token string) sasl.Client
```

---

### T6 — IMAP client: XOAUTH2 login

`internal/email/imap/client.go` — `Config` gains an optional token source:

```go
TokenSource oauth2.TokenSource // when set, authenticate with XOAUTH2 instead of LOGIN
Username    string             // still used as the XOAUTH2 identity
```

In `Connect`, after dial/select choice:

```go
if c.cfg.TokenSource != nil {
    tok, err := c.cfg.TokenSource.Token() // refreshes if expired
    if err != nil {
        return fmt.Errorf("imap oauth token: %w", err)
    }
    if err := cl.Authenticate(oauth.NewXOAUTH2(c.cfg.Username, tok.AccessToken)); err != nil {
        return fmt.Errorf("imap xoauth2: %w", err)
    }
} else {
    if err := cl.Login(c.cfg.Username, c.cfg.Password).Wait(); err != nil { ... }
}
```

Because the token source refreshes on `Token()`, each (re)connect picks up a
fresh access token — this is also what makes reconnect-after-idle work for OAuth.

---

### T7 — CLI: client credentials + consent during add

`cmd/email-agent/cmd_init.go` — new `init oauth` section:

```
Google OAuth (for Gmail accounts)
  Client ID:     <input>
  Client secret: <masked input>
```

`cmd/email-agent/cmd_account.go` — `addOrEditAccount` prompts for auth type
(`password` / `oauth`). For `oauth`:

- Skip the password prompt.
- Require global client id/secret to be set first (else point to `init oauth`).
- Run `oauth.Consent(...)`, store the returned tokens on the account.
- Host/port default to `imap.gmail.com` / `993`; username defaults to the email.

When the daemon later hits `invalid_grant` (revoked/expired refresh token), it
logs a clear instruction: re-run `email-agent account add` (or a new
`account reauth <email>`) to re-consent. No silent failure loop.

---

### T8 — Provider factory: OAuth case

`cmd/email-agent/main.go` — fill in `newProvider`'s `case "oauth"`:

```go
case domain.AuthOAuth:
    cfg := oauth.GoogleConfig(globalClientID, globalClientSecret)
    ts := oauth.TokenSource(ctx, cfg, oauth.Tokens{
        AccessToken:  acc.OAuthAccessToken,
        RefreshToken: acc.OAuthRefreshToken,
        Expiry:       acc.OAuthTokenExpiry,
    }, func(t oauth.Tokens) error {
        return accountRepo.UpdateTokens(ctx, acc.ID, t)
    })
    return imapmail.NewClient(imapmail.Config{
        Host: acc.Host, Port: acc.Port, Username: acc.Username,
        TokenSource: ts, TLS: acc.TLS, FetchBody: fetchBody,
        Logger: logger.With("component", "imap", "account", acc.Email),
    }), nil
```

`newProvider` therefore needs access to the global client creds and the account
repo (for token persistence) — pass them in.

---

### T9 — Tests

| Package | What to test |
|---------|--------------|
| `internal/auth/oauth` | XOAUTH2 initial-response byte format is exactly `user=..\x01auth=Bearer ..\x01\x01` |
| `internal/auth/oauth` | TokenSource calls `persist` when the access token is refreshed; not called when still valid |
| `internal/db/repo` | `UpdateTokens` writes only the token columns; `Upsert`/scan round-trip tokens |
| `internal/config` | OAuth account without client id/secret fails validation; password-only config still valid without them |
| `internal/email/imap` | `Connect` calls `Authenticate` (not `Login`) when a token source is set (fake server or interface seam) |

The browser consent flow is driven by a fake `oauth2.Config` endpoint in tests;
no real network calls.

---

### T10 — Docs

- `docs/settings.md` — add `oauth.google.*` keys and the `account add` OAuth flow.
- `docs/db-schema.md` — add the token columns; bump migration to `008`.
- `docs/dependencies.md` — add `golang.org/x/oauth2`.
- New `docs/stages/rollout/008-01-gmail-oauth-setup.md` — the step-by-step Google
  Cloud Console walkthrough for users (project, consent screen, Desktop client).

---

## Dependencies

New: `golang.org/x/oauth2` (and `golang.org/x/oauth2/google` for the endpoint).
Reuses `github.com/emersion/go-sasl` (already indirect) for the `sasl.Client`
interface. No web framework, no hosted redirect.

---

## Recommended Task Order

```
T1  → domain.Account token fields + AuthOAuth const
T2  → migration 008_oauth_tokens.sql
T3  → account_repo: persist tokens + UpdateTokens
T4  → config: oauth.google.* settings + validation
T5  → auth/oauth: consent flow + token source + xoauth2 client
T6  → imap client: XOAUTH2 login path
T7  → cmd: init oauth + account add consent
T8  → main.go: newProvider case "oauth"
T9  → tests
T10 → docs
```

---

## Definition of Done

1. `make check` passes.
2. `email-agent init oauth` stores Google Client ID/secret (secret masked in
   `config list`).
3. `email-agent account add` with auth type `oauth` opens the browser, completes
   consent, and stores a refresh token — no password entered.
4. The daemon polls the Gmail account over IMAP using XOAUTH2, refreshing the
   access token automatically without re-consent.
5. A revoked/expired refresh token produces a clear "re-run account add" log
   message rather than a silent retry loop.
6. Password accounts (Stage 7) continue to work unchanged.

---

## Out of Scope (future stages)

- Gmail REST API transport (history API, push notifications).
- Microsoft Graph / Outlook OAuth — same factory seam, separate stage.
- Automatic app verification / publishing — the user manages Google Cloud
  publishing status themselves; this stage only handles the token lifecycle.
- Per-account distinct Google client credentials (one global client is reused).

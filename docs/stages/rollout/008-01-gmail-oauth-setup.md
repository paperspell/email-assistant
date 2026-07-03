# 008-01-gmail-oauth-setup.md

# Gmail OAuth — Google Cloud Console setup

This is the one-time, out-of-repo setup needed before adding a Gmail account with
`auth_type = oauth`. You end up with two values — a **Client ID** and a
**Client secret** — which you give to `email-agent init oauth`.

There is **no server or web page to host**. The consent screen is hosted by
Google, and email-agent runs a temporary `localhost` listener to catch the
redirect. The OAuth client just identifies the app to Google; the actual mailbox
access comes from the token you grant on the consent screen.

---

## Steps (~10 minutes)

All in <https://console.cloud.google.com>:

1. **Create a project** — top bar → *New Project* (any name, e.g. `email-agent`).
   Select it.

2. **Enable the Gmail API** — *APIs & Services → Library* → search "Gmail API" →
   **Enable**.

3. **Configure the OAuth consent screen** — *APIs & Services → OAuth consent
   screen*:
   - User type: **External** (or **Internal** if you're on Google Workspace —
     simpler, and avoids the verification caveat below).
   - Fill in app name and your email for support + developer contact.
   - Add the scope `https://mail.google.com/` (full IMAP access).
   - Add your own Gmail address as a **Test user**.
   - Leave publishing status as **Testing**.

4. **Create the OAuth client** — *APIs & Services → Credentials → Create
   Credentials → OAuth client ID*:
   - Application type: **Desktop app**.
   - Create → copy the **Client ID** and **Client secret**.

---

## Wire it into email-agent

```
email-agent init oauth        # paste Client ID + Client secret (secret is masked)
email-agent account add       # choose auth type "oauth"; a browser opens for consent
```

After consent, a refresh token is stored (encrypted) on the account and the
daemon logs in with XOAUTH2, refreshing access tokens automatically.

---

## Caveats

- **Testing mode expires refresh tokens after ~7 days.** While the consent screen
  is in "Testing", Google invalidates refresh tokens weekly, so you'll need to
  re-authorize. To avoid this you must **publish** the app, which for the
  restricted `https://mail.google.com/` scope triggers Google's verification
  process. (Workspace **Internal** apps are not subject to this.)
- The **Client secret** for a Desktop-app client is not truly confidential — this
  is expected and normal for installed apps.
- When a refresh token is revoked/expired, the daemon **sends a Telegram alert**
  naming the account with re-authorization instructions, and logs the same error.
  It does not silently retry forever. The alert is sent once per outage (not on
  every poll). To recover:
  1. On the machine running the daemon: `email-agent account edit <email>` and
     answer **y** to "Re-authorize with Google now?" (a browser opens for consent).
  2. Restart the daemon (`email-agent run`) — the running process caches the old
     token in memory, so the new one only takes effect after a restart.

  A mailbox whose token has expired at daemon startup is skipped (with the alert)
  rather than taking the whole daemon down; other accounts keep polling.

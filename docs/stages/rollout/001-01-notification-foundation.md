# 001-01 — Notification Foundation — Local Verification

## 1. Run automated checks

```bash
make check
```

## 2. Build and verify binary starts

```bash
make build
./bin/email-agent version
```

## 3. Initialize

```bash
./bin/email-agent init
```

Follow the wizard. Enter your IMAP credentials and Telegram bot token and chat ID.

## 4. Run the daemon

```bash
./bin/email-agent run
```

## 5. Trigger a notification

Send yourself a test email and wait one poll interval.

Check that a Telegram message arrives and a log line like this appears:

```
notified  uid=...  subject=...
```

## 6. Verify no duplicates on restart

Stop with `Ctrl+C` and restart. No new Telegram messages should appear for already-seen emails.

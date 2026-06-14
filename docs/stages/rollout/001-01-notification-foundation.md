# 001 — Notification Foundation — Local Verification

## 1. Run automated checks

```bash
make check
```

## 2. Build and verify binary starts

```bash
make build
./bin/email-agent version
```

## 3. Configure

```bash
cp config.example.yaml config.yaml
# fill in account and telegram sections
export IMAP_PASSWORD=...
export TELEGRAM_BOT_TOKEN=...
```

## 4. Run the daemon

```bash
./bin/email-agent run --config config.yaml
```

## 5. Trigger a notification

Send yourself a test email and wait one poll interval.

Check that a Telegram message arrives and a log line like this appears:

```
notified  uid=...  subject=...
```

## 6. Verify no duplicates on restart

Stop with `Ctrl+C` and restart. No new Telegram messages should appear for already-seen emails.

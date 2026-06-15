# LLM Enrollment Guide

This guide covers how to enable LLM-based email classification after the daemon
is already running. LLM is fully optional — the daemon continues working with
rule-based scoring if you skip this step.

**Prerequisites:** `email-agent init` has been completed and the daemon runs.

---

## Option A — Anthropic (Claude)

### 1. Get an API key

1. Go to [console.anthropic.com](https://console.anthropic.com)
2. Create an account or log in
3. Navigate to **API Keys** → **Create Key**
4. Copy the key (starts with `sk-ant-...`)

### 2. Configure the agent

```bash
email-agent init llm
```

At the prompts:

```
LLM Classification
  (press Enter at provider prompt to disable LLM)

  Provider (anthropic/openai) []: anthropic
  Anthropic API key (Enter to keep unchanged): sk-ant-...
  Model override (Enter for default): 
  Body access (headers_only/full_body) [headers_only]: 
```

- **Provider** — type `anthropic`
- **API key** — paste your key (input is hidden)
- **Model override** — press Enter to use the default (`claude-sonnet-4-6`)
- **Body access** — `headers_only` sends only subject and headers (default, privacy-safe);
  `full_body` also sends the plain-text email body for richer classification

### 3. Restart the daemon

```bash
# Stop the running daemon (Ctrl+C or kill), then:
email-agent run
```

You should see in the logs:

```
level=INFO msg="LLM provider: anthropic" model=""
```

### 4. Verify

Send yourself a test email with a clear subject (e.g. "urgent meeting tomorrow").
The Telegram notification should now include a **Summary** line instead of the
rule signal list.

### Cost estimate

Claude Sonnet 4.6 is billed per token. A typical email classification costs
roughly 300–600 input tokens + ~100 output tokens. At current pricing that is
under $0.001 per email. Only emails that pass the rule-based threshold are sent
to the LLM.

---

## Option B — OpenAI

### 1. Get an API key

1. Go to [platform.openai.com](https://platform.openai.com)
2. Create an account or log in
3. Navigate to **API keys** → **Create new secret key**
4. Copy the key (starts with `sk-...`)
5. Make sure your account has a positive credit balance (Settings → Billing)

### 2. Configure the agent

```bash
email-agent init llm
```

At the prompts:

```
LLM Classification
  (press Enter at provider prompt to disable LLM)

  Provider (anthropic/openai) []: openai
  OpenAI API key (Enter to keep unchanged): sk-...
  Model override (Enter for default): 
  Body access (headers_only/full_body) [headers_only]: 
```

- **Provider** — type `openai`
- **API key** — paste your key (input is hidden)
- **Model override** — press Enter to use the default (`gpt-4o-mini`)
- **Body access** — same choice as above

### 3. Restart the daemon

```bash
email-agent run
```

You should see in the logs:

```
level=INFO msg="LLM provider: openai" model=""
```

### 4. Verify

Same as for Anthropic — look for the **Summary** line in the next Telegram
notification.

### Cost estimate

GPT-4o-mini is billed per token. A typical classification costs roughly
300–600 input tokens + ~100 output tokens, well under $0.001 per email.

---

## Switching provider

To switch from one provider to another, re-run the wizard:

```bash
email-agent init llm
```

Enter the new provider name and its API key. The previous provider's key remains
stored in the database but is ignored while the other provider is active.

## Disabling LLM

```bash
email-agent init llm
```

At the provider prompt, press **Enter** without typing anything. The daemon
will confirm `LLM disabled.` and fall back to rule-based classification on the
next restart.

## Changing body access mode

```bash
email-agent init llm
```

Re-enter your current provider and key (or press Enter to keep unchanged),
then choose `full_body` or `headers_only` at the body access prompt.

## Troubleshooting

**LLM errors in logs (`llm classify: ...`)**

The daemon logs LLM errors as warnings and falls back to the rule-based result
— no notifications are lost. Common causes:

- Invalid or expired API key → re-run `email-agent init llm` and enter a new key
- Quota exhausted → add credits to your account
- Network timeout → transient; the next email will retry

**Divergence warnings**

```
level=WARN msg="classification divergence: rule=55 llm=88 diff=33"
```

This is informational — the scores differ by more than the threshold (default 30).
The LLM result is used for the notification decision. If divergences are frequent,
it may indicate the rule-based scorer needs tuning.

**Checking current LLM settings**

```bash
email-agent config set --help   # list all keys
```

Or inspect directly:

```bash
email-agent config set llm.provider anthropic   # re-set to see current value
```

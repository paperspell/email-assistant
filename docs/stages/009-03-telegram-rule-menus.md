# 009-03-telegram-rule-menus.md

Status: Planned
Version: 0.1

# Stage 009-03 — Telegram Rule Menus

## Goal

Close the feedback loop in Telegram: turn the live-notification **Ignore** button
into a menu that creates a per-account rule, and turn **promote** (from the digest)
into rule learning — soft sender bump plus an offer to harden into an `allow` rule,
with reverse-lookup of whatever filtered the email.

Builds on the rule engine (**009-01**) and the digest/promote mechanics
(**009-02**). This sub-stage is pure Telegram UX over the existing engine.

---

## What Changes

| Before | After |
|--------|-------|
| **Ignore** button = −25 sender score + mark ignored | **Ignore** opens a menu: sender / domain / List-Id / subject pattern / define reason / once |
| Ignoring never creates a reusable rule | Each menu choice (except "once") creates a per-account `filter_rule` or `llm_clause` |
| Subject rules would need hand-typed patterns | The LLM proposes the subject pattern from the email; user confirms |
| Promote just re-sends the email | Promote also reverse-looks-up the filtering rule and offers remove / exception / always-important |

---

## Directory Structure Changes

```
internal/
  telegram/
    handler.go              # Ignore → menu; menu callbacks; promote follow-up
    menu.go                 # NEW: build ignore menu + promote follow-up keyboards
    pending.go              # NEW: pending text-input state (for "define reason")
  filter/
    suggest.go              # NEW: SuggestSubjectPattern (LLM, normalization fallback)
  db/
    migrations/
      013_pending_actions.sql  # NEW: pending_actions (await free-text reply)
    repo/
      pending_repo.go        # NEW
cmd/email-agent/
  (no new commands — management already exists via 009-01 `rules`/`clauses`)
```

---

## Tasks

### T1 — Ignore menu on live notifications

`telegram/handler.go` — the existing `case "ignore"` (Stage 7, −25 + mark ignored)
becomes "show the menu" instead of acting immediately. `telegram/menu.go` builds an
inline keyboard for the email:

```
Why ignore?
[ This sender ]      ignore:sender:<emailID>
[ This domain ]      ignore:domain:<emailID>
[ This list ]        ignore:listid:<emailID>   (only if the email has a List-Id)
[ Subject like this ]ignore:subject:<emailID>
[ Describe a reason ]ignore:reason:<emailID>
[ Just this once ]   ignore:once:<emailID>
[ Cancel ]           ignore:cancel:<emailID>
```

Each leaf callback:
- creates the matching `filter_rule` (`action=ignore`, `source=user`) for the
  email's account via the 009-01 `RuleRepo` — `sender`=From, `domain`=From domain,
  `list_id`=`msg.ListID`, `subject`=LLM-suggested pattern scoped to the sender;
- then performs the existing ignore action (−25 sender, `StatusIgnored`,
  `decided_by="rule:<id>"` for the rule cases / `llm:low`→ unchanged for "reason"),
  marks the message read, removes the keyboard, and confirms ("Rule added: ignore
  domain shop.com").
- **`once`** = today's behaviour: −25 + ignore, **no rule**.
- **`cancel`** = restore the original keyboard, do nothing.

`list_id` button is omitted when the email has no List-Id.

---

### T2 — Subject pattern suggestion

`internal/filter/suggest.go`:

```go
// SuggestSubjectPattern returns a short, generalizable substring for the subject.
// Uses the LLM when a provider is configured; otherwise normalizes the subject
// (lowercase, strip digits/dates/emoji/punctuation, keep the stable stem).
func SuggestSubjectPattern(ctx, llm.Provider, subject string) (string, error)
```

For `ignore:subject`, the bot shows the candidate and asks for confirmation:

```
Suggested pattern: "flash sale"  (matches subjects from this sender)
[ Use it ]  [ Edit ]  [ Cancel ]
```

"Use it" → create a `subject` rule, `matcher=substring`, `ScopeKind=sender`,
`ScopeValue=From`. "Edit" routes through the pending-input flow (T3). The matcher
stays a cheap substring at runtime — the LLM is only used here, at authoring time.

---

### T3 — Pending free-text input

"Describe a reason" and subject "Edit" need a free-text reply. Stage 009-02 already
enabled `message` updates; here we add a small **pending-action** mechanism.

`013_pending_actions.sql`:

```sql
CREATE TABLE pending_actions (
    chat_id     INTEGER NOT NULL,
    kind        TEXT NOT NULL,      -- 'clause' | 'subject_edit'
    email_id    TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    PRIMARY KEY (chat_id)
);
```

Flow: the menu choice writes a `pending_actions` row and prompts "Send the rule
text as a message." The next non-command message from that chat (handler_msg from
009-02) consumes the pending row and:
- `clause` → create an `llm_clause` (`action ignore`, `source=user`) for the account;
- `subject_edit` → create the `subject` rule with the user's text.

One pending action per chat (keyed by `chat_id`); a new menu choice overwrites it;
`/cancel` clears it.

---

### T4 — Promote follow-up (reverse-lookup)

Extends the 009-02 promote path. After an email is promoted, inspect its
`decided_by`:

- **`rule:<id>`** (a Tier-0 rule filtered it) — show:
  ```
  Promoted. It was hidden by rule #4 (ignore domain shop.com).
  [ Remove that rule ]   [ Add an exception ]   [ Keep rule ]
  ```
  - *Remove* → delete the rule.
  - *Add an exception* → create an `allow` rule on the **most specific** key
    (sender), so this sender overrides the broader ignore (precedence: allow >
    ignore from 009-01).
- **`baseline`** / **`llm:low`** (no rule to blame) — soft sender bump already
  applied by promote; additionally offer:
  ```
  Promoted. Always treat mail from alice@corp.com as important?
  [ Yes, add allow rule ]   [ No, just this time ]
  ```
  *Yes* → create an `allow` sender rule. This is the agreed (а)+(б): always the
  soft bump, plus an optional hard allow-rule.

For `llm:low`, identifying the exact clause is non-deterministic; we do **not**
auto-blame a clause — we offer the sender allow-rule instead.

---

### T5 — Wiring

`telegram/handler.go` dispatch gains the `ignore:*` and promote follow-up callback
actions and the pending-input branch in the message handler. `Handler` gains
`RuleRepo`, `ClauseRepo`, `PendingRepo`, and the `llm.Provider` (for subject
suggestion); all already constructed in `main.go` for 009-01/009-02.

---

## Tests

| Package | What to test |
|---------|--------------|
| `internal/telegram` | each `ignore:*` leaf creates the right rule/clause; `once` creates none; `cancel` restores keyboard |
| `internal/telegram` | pending-input: clause text becomes an `llm_clause`; new menu choice overwrites pending |
| `internal/filter` | `SuggestSubjectPattern` LLM path + normalization fallback when no provider |
| `internal/telegram` | promote follow-up: `rule:` offers remove/exception (exception = allow sender); `baseline`/`llm:low` offers allow sender |
| `internal/db/repo` | pending_actions single-row-per-chat semantics |

---

## Recommended Task Order

```
T1 ignore menu → T2 subject suggestion → T3 pending input → T4 promote follow-up → T5 wiring → tests
```

---

## Definition of Done

1. `make check` passes.
2. The live **Ignore** button opens the menu; each choice creates the correct
   per-account rule/clause (or none, for "once"); the email is ignored + marked read.
3. Subject rules are created from an LLM-suggested (editable) substring, scoped to
   the sender; runtime matching stays a cheap substring.
4. "Describe a reason" captures a free-text message into an `llm_clause`.
5. Promote reverse-looks-up the filtering rule and offers remove/exception, or —
   when no rule filtered it — offers an always-important allow rule (plus the soft
   bump that already happens).
6. All new rules are visible/editable through the 009-01 `rules`/`clauses` CLI.

---

## Out of Scope (future)

- Glob/regex subject matchers (substring only).
- Auto-blaming a specific LLM clause for an `llm:low` ignore.
- Positive (allow) LLM clauses; bulk rule editing UIs beyond the existing CLI.

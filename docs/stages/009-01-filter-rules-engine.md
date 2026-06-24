# 009-01-filter-rules-engine.md

Status: Planned
Version: 0.1

# Stage 009-01 — Filter Rules Engine (per-account)

## Goal

Introduce the **mechanical filtering layer** under the LLM: per-account rules and
ignore-only LLM clauses that the rest of Stage 9 (digest, Telegram menus) builds
on. This sub-stage delivers the *engine and CLI only* — no Telegram UX, no digest.

After this stage the daemon decides each email through a layered pipeline and
records **why** it decided (provenance), so later stages can explain and reverse
the decision.

```
allow rule (Tier-0) > ignore rule (Tier-0) > baseline gate > LLM (+ clauses) > min_importance
```

---

## What Changes

| Before | After |
|--------|-------|
| Rule scoring + LLM, no user-defined rules | Per-account `filter_rules` (allow/ignore) consulted before the LLM |
| LLM system prompt is fixed | Active per-account ignore **clauses** are injected into the prompt |
| Baseline gate uses `min_importance` to decide whether to run the LLM | Baseline gate cuts only obvious junk (`filter.baseline_floor`, default `maybe`); everything above reaches the LLM |
| No record of why an email was ignored | `emails.decided_by` stores `rule:<id>` / `baseline` / `llm:low` |
| Sender/domain scores are **global** | Sender/domain learning is **per-account** (started from scratch) |
| No rule management | `email-agent rules …` and `email-agent clauses …` CLI |
| `email.Message` lacks List-Id | `email.Message.ListID` captured from the `List-Id` header |

---

## Directory Structure Changes

```
internal/
  domain/
    rule.go                 # NEW: FilterRule, LLMClause, RuleAction/RuleType consts
    email.go                # add DecidedBy field
  email/
    email.go                # Message.ListID
    imap/client.go          # fetch + parse List-Id header
  filter/
    engine.go               # NEW: RuleEngine.Evaluate(rules, msg) -> (action, *FilterRule, bool)
    match.go                # NEW: sender/domain/list_id/subject matchers
    engine_test.go          # NEW
  db/
    migrations/
      010_filter_rules.sql      # NEW: filter_rules, llm_clauses, emails.decided_by
      011_scores_per_account.sql# NEW: recreate senders/domains keyed by (account_id, …)
    repo/
      rule_repo.go          # NEW: filter_rules CRUD (per account, ordered for list/number)
      clause_repo.go        # NEW: llm_clauses CRUD
      sender_repo.go        # add account_id to key + signatures
      domain_repo.go        # add account_id to key + signatures
      email_repo.go         # persist/read decided_by
  importance/
    filter.go               # Classify gains accountID; per-account sender/domain lookup
  llm/
    provider.go             # SystemPrompt(clauses []string); Classify uses clauses
    anthropic/…, openai/…   # build system prompt = SystemPrompt(req.IgnoreClauses)
  scheduler/
    scheduler.go            # new layered pipeline in processMessage; baseline floor
  config/
    keys.go, config.go      # filter.baseline_floor
cmd/email-agent/
  cmd_rules.go              # NEW: rules list/enable/disable/edit/remove/why
  cmd_clauses.go            # NEW: clauses list/enable/disable/remove
  cmd_account.go            # seed default clauses (set A) + example rules (set B) on add
  main.go                   # wire RuleEngine, RuleRepo, ClauseRepo into scheduler
```

---

## Tasks

### T1 — Domain types

`internal/domain/rule.go`:

```go
const (
    RuleActionIgnore = "ignore"
    RuleActionAllow  = "allow"

    RuleTypeSender  = "sender"   // exact email
    RuleTypeDomain  = "domain"   // exact domain
    RuleTypeListID  = "list_id"  // exact List-Id
    RuleTypeSubject = "subject"  // substring (glob/regex deferred)

    RuleSourceUser    = "user"
    RuleSourceDefault = "default"
)

type FilterRule struct {
    ID         string
    AccountID  string
    Action     string // ignore | allow
    Type       string // sender | domain | list_id | subject
    Matcher    string // "exact" | "substring"
    Value      string
    ScopeKind  string // subject rules: "sender" (bound to ScopeValue); "" = unscoped
    ScopeValue string
    Enabled    bool
    Source     string
    CreatedAt  time.Time
}

type LLMClause struct {
    ID        string
    AccountID string
    Text      string // natural-language ignore instruction
    Enabled   bool
    Source    string
    CreatedAt time.Time
}
```

`internal/domain/email.go` — add `DecidedBy string` to `Email`.

---

### T2 — Migration 010: rules, clauses, provenance

`internal/db/migrations/010_filter_rules.sql`:

```sql
-- +goose Up
CREATE TABLE filter_rules (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    action      TEXT NOT NULL DEFAULT 'ignore',   -- ignore | allow
    type        TEXT NOT NULL,                     -- sender | domain | list_id | subject
    matcher     TEXT NOT NULL DEFAULT 'exact',     -- exact | substring
    value       TEXT NOT NULL,
    scope_kind  TEXT NOT NULL DEFAULT '',          -- subject: 'sender'
    scope_value TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    source      TEXT NOT NULL DEFAULT 'user',      -- user | default
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_filter_rules_account ON filter_rules(account_id, enabled);

CREATE TABLE llm_clauses (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    text        TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    source      TEXT NOT NULL DEFAULT 'user',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_llm_clauses_account ON llm_clauses(account_id, enabled);

ALTER TABLE emails ADD COLUMN decided_by TEXT NOT NULL DEFAULT '';

-- +goose Down
DROP TABLE IF EXISTS filter_rules;
DROP TABLE IF EXISTS llm_clauses;
-- emails.decided_by left in place (SQLite cannot drop columns pre-3.35).
```

`decided_by` values: `''` (notified / allow), `rule:<id>`, `baseline`, `llm:low`.
Clauses are **ignore-only** in Stage 9 — no polarity column (positive intent is
expressed via `allow` rules).

---

### T3 — Migration 011: per-account scores (from scratch)

Sender/domain learning becomes per-account. Per the agreed decision we **start
from scratch** (the old global scores are coarse and re-learn cheaply):

```sql
-- +goose Up
DROP TABLE IF EXISTS senders;
DROP TABLE IF EXISTS domains;
CREATE TABLE senders (
    account_id       TEXT NOT NULL,
    email            TEXT NOT NULL,
    importance_score INTEGER NOT NULL DEFAULT 0,
    seen_count       INTEGER NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL,
    PRIMARY KEY (account_id, email)
);
CREATE TABLE domains ( … same shape, PRIMARY KEY (account_id, domain) );
-- +goose Down
-- forward-only.
```

`SenderRepo`/`DomainRepo` gain an `accountID` argument on `Get`/`Upsert`.
`importance.Filter.Classify` gains `accountID` and looks up scores per account.
`telegram.Handler.adjustSenderScore` (Stage 7) becomes per-account.

> Note: existing global `senders` data is dropped. This is intentional and was
> chosen over replication; learning resumes from the next feedback click.

---

### T4 — List-Id capture

`internal/email/email.go` — add `ListID string` to `Message`.
`internal/email/imap/client.go` — add `"List-Id"` to `extraHeaders` (line ~29)
and `msg.ListID = h.Get("List-Id")` alongside the other header parsing (~316).
List-Id is needed for `list_id` rules; without it those rules never match.

---

### T5 — Rule engine

`internal/filter/engine.go`:

```go
type RuleEngine struct{}

// Evaluate returns the first matching rule under precedence:
// enabled allow rules first, then enabled ignore rules. Within a kind, the most
// specific type wins: sender > list_id > subject > domain.
func (RuleEngine) Evaluate(rules []domain.FilterRule, msg email.Message) (action string, matched *domain.FilterRule, ok bool)
```

`internal/filter/match.go` — matchers:
- `sender`: case-insensitive exact `msg.FromEmail`.
- `domain`: case-insensitive suffix of the From address domain.
- `list_id`: case-insensitive contains of `msg.ListID`.
- `subject`: `matcher=substring` → case-insensitive `strings.Contains`; honour
  `ScopeKind=="sender"` (only when `msg.FromEmail == ScopeValue`).

Pure functions, no DB — fully unit-testable.

---

### T6 — LLM clause injection

`internal/llm/provider.go` — change clause plumbing without breaking providers:

```go
// SystemPrompt returns the base prompt plus any active ignore clauses.
func SystemPrompt(ignoreClauses []string) string
```

Add `IgnoreClauses []string` to `ClassifyRequest`. Each provider
(`anthropic`, `openai`) builds its system message from
`llm.SystemPrompt(req.IgnoreClauses)`. Clauses are rendered as a bounded,
clearly-delimited list, e.g.:

```
Additional user-defined ignore rules (treat matching mail as not important):
- Ignore promotional/marketing emails unless tied to an order, payment, or security.
- …
```

No hard cap on clause count; the CLI warns when many are active (prompt growth).

---

### T7 — New scheduler pipeline

`internal/scheduler/scheduler.go` — replace the gate in `processMessage`
(currently `Filter.Classify` → `shouldNotify` → LLM) with:

1. **Load** enabled rules + clauses for the account (once per poll, passed in).
2. **Allow rules** — `Evaluate` match with `action=allow` → force notify
   (`decided_by=""`), skip baseline/LLM.
3. **Ignore rules** — match with `action=ignore` → `StatusIgnored`,
   `decided_by="rule:<id>"`, **skip LLM**.
4. **Baseline** — `Filter.Classify(ctx, accountID, e.ID, msg)`. If
   `Level < filter.baseline_floor` (default `maybe`) → `StatusIgnored`,
   `decided_by="baseline"`, skip LLM. (Saves the rule-based classification row.)
5. **LLM** — build request with `IgnoreClauses` from active clauses. If
   `result.Level >= min_importance` → notify (`decided_by=""`); else
   `StatusIgnored`, `decided_by="llm:low"` (the classification + summary are saved
   so the digest can show them in 009-02).

`decided_by` is written via `EmailRepo` on the status update.

---

### T8 — Config: baseline floor

`internal/config/keys.go` — `KeyFilterBaselineFloor = "filter.baseline_floor"`.
`config.go` — `Filter.BaselineFloor domain.ImportanceLevel`, default `maybe`,
validated against the known levels. Add to `KnownKeys`/`DefaultValues`.

Lower it to `ignore` to send only clear-junk-or-better to the LLM (cheapest);
raise it to `important` to nearly disable the LLM. Default `maybe` matches the
"baseline cuts obvious junk, LLM judges the rest" intent.

---

### T9 — Repos

`rule_repo.go` — `List(accountID)` (ordered by `created_at` for stable numbering),
`Get(accountID, n)` by 1-based index, `Add`, `SetEnabled`, `Update`, `DeleteByIndex`.
`clause_repo.go` — analogous; `ActiveTexts(accountID)` returns enabled clause texts
for prompt injection.

---

### T10 — CLI

`cmd/email-agent/cmd_rules.go`:

```
email-agent rules list    <account>        # NUM TYPE ACTION VALUE SCOPE ENABLED SOURCE
email-agent rules enable  <account> <n>
email-agent rules disable <account> <n>
email-agent rules edit    <account> <n>    # change value / scope (fills B templates)
email-agent rules remove  <account> <n>
email-agent rules why     <id|uid>         # print the deciding rule + reason chain (provenance)
```

`cmd/email-agent/cmd_clauses.go`: `clauses list/enable/disable/remove <account> [n]`,
with a warning line when the active count is high.

`rules why` resolves `emails.decided_by`: `rule:<id>` → the rule + value;
`baseline` → the saved rule-based `reason` list; `llm:low` → the LLM level/score/summary.

---

### T11 — Default seeding

Defaults are created where an account is born — `addOrEditAccount` in
`cmd_account.go` (used by both `init` and `account add`):

- **Set A — default ignore clauses** (`source=default`, enabled), seeded for the
  account **only when an LLM provider is configured**:
  1. *Promo/marketing* — "Ignore promotional and marketing emails (sales,
     discounts, product announcements, webinars) unless tied to an active order,
     payment, shipping, or account security."
  2. *Social noise* — "Ignore automated social-media notifications: likes,
     reactions, new followers, 'people you may know', comment digests."
  3. *Newsletters/digests* — "Ignore periodic newsletter digests unless from a
     sender previously marked important."
- **Set B — example rules** (`source=default`): the CLI/init prompts
  *"Enable example filter rules? [y/N]"* and prints the listing. On **yes** they
  are inserted **enabled**; on **no**, inserted **disabled** (visible/editable via
  `rules list`). Examples: `ignore domain facebookmail.com`, a `list_id` template,
  a `subject` template (edited before enabling). No `noreply@` ignore (too risky).

**Migration backfill:** Set A clauses are also inserted for **all existing
enabled accounts** in migration 010 (decision: defaults go to every account). Set
B examples are not backfilled (opt-in).

---

### T12 — Wiring

`cmd/email-agent/main.go` — construct `RuleRepo`, `ClauseRepo`, `RuleEngine`, and
pass enabled rules/clause-texts into each account's `scheduler.Config` (loaded per
poll). `importance.NewFilter` calls updated for per-account scores.

---

## Tests

| Package | What to test |
|---------|--------------|
| `internal/filter` | each matcher; precedence allow>ignore and sender>list_id>subject>domain; subject scope binding |
| `internal/scheduler` | allow short-circuits to notify; ignore rule skips LLM + sets `decided_by=rule:`; baseline floor sets `baseline`; LLM-low sets `llm:low` |
| `internal/llm` | `SystemPrompt(clauses)` includes clauses; empty clauses == base prompt |
| `internal/db/repo` | rule/clause CRUD + stable 1-based indexing; per-account isolation of rules/scores |
| `internal/db` | migration 011 drops old scores; 010 backfills Set A to existing accounts |

---

## Recommended Task Order

```
T1 domain types → T2 mig 010 → T3 mig 011 (+repos/Filter signature) → T4 List-Id
→ T5 engine → T6 clause injection → T7 pipeline → T8 floor → T9 repos
→ T10 CLI → T11 seeding → T12 wiring → tests
```

---

## Definition of Done

1. `make check` passes.
2. Pipeline order is allow > ignore > baseline floor > LLM(+clauses) > threshold,
   and every ignored email carries a correct `decided_by`.
3. `rules`/`clauses` CLI manage per-account lists; `rules why` explains a decision.
4. Active clauses appear in the LLM system prompt; disabling one removes it.
5. New accounts get Set A clauses (when LLM configured) and are offered Set B;
   existing accounts receive Set A via migration.
6. Sender/domain learning is per-account; password/OAuth accounts unaffected.

---

## Out of Scope (later sub-stages / future)

- Daily digest, `/important`, Mark read / Remove → **009-02**.
- Telegram ignore-menu, promote→allow, LLM-suggested subject pattern → **009-03**.
- Glob/regex subject matchers (substring only here).
- Positive (allow) LLM clauses.

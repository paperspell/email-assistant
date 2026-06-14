# 002-01-importance-detection.md

Status: Draft
Version: 0.1

# Stage 002-01 — Importance Detection

## Goal

Replace "notify on every email" with "notify only on emails that matter."

The scheduler classifies each incoming email using a scoring pipeline — header signals, subject keyword detection, and built-in rules — and sends a Telegram notification only when the classification level meets the configured threshold (default: Important or above).

No body content is fetched in this stage. All signals come from email headers and the subject line.

No LLM, no user feedback learning, no Telegram interaction. Those come in later stages.

---

## What Changes

| Before | After |
|--------|-------|
| Every new email → notification | Only Important/Critical emails → notification |
| No classification | Classification stored per email |
| No feature extraction | Header + subject features extracted |
| No scoring | Rule-based score computed |

---

## Classification Levels

| Level | Score | Default behaviour |
|-------|-------|-------------------|
| Critical | 90+ | Notify |
| Important | 70–89 | Notify |
| Maybe | 30–69 | Skip |
| Ignore | 0–29 | Skip |

Thresholds are constants; the minimum level to notify is configurable via `notification.min_importance` setting (default: `important`).

---

## Scoring Formula

```
sender_score
+ domain_score
+ keyword_score
+ thread_score
= total_score  (clamped to 0–100)
```

Each component is described in the tasks below.

---

## Directory Structure Changes

```
internal/
  domain/
    classification.go      # ImportanceLevel, Category, Classification types
  features/
    features.go            # EmailFeatures struct
    extractor.go           # Extract(email.Message) → EmailFeatures
    keywords.go            # multilingual keyword dictionaries
    language.go            # language detection wrapper
  importance/
    filter.go              # Filter: Classify(EmailFeatures) → Classification
    rules.go               # built-in rule set
    scoring.go             # score computation
  email/
    imap/
      client.go            # extend to fetch extra headers
  db/
    migrations/
      003_classifications.sql
    repo/
      classification_repo.go
      sender_repo.go
      domain_repo.go
      classification_repo_test.go
      sender_repo_test.go
```

---

## Tasks

### T1 — Domain model additions

`internal/domain/classification.go`

```go
type ImportanceLevel string

const (
    LevelCritical  ImportanceLevel = "critical"
    LevelImportant ImportanceLevel = "important"
    LevelMaybe     ImportanceLevel = "maybe"
    LevelIgnore    ImportanceLevel = "ignore"
)

type Category string

const (
    CategoryWork       Category = "work"
    CategoryFinance    Category = "finance"
    CategoryLegal      Category = "legal"
    CategoryGovernment Category = "government"
    CategorySchool     Category = "school"
    CategoryFamily     Category = "family"
    CategorySecurity   Category = "security"
    CategoryTravel     Category = "travel"
    CategoryShopping   Category = "shopping"
    CategoryRecruiting Category = "recruiting"
    CategoryMarketing  Category = "marketing"
    CategorySocial     Category = "social"
    CategoryOther      Category = "other"
)

type Classification struct {
    ID         string
    EmailID    string
    Level      ImportanceLevel
    Category   Category
    Score      int
    Reason     []string  // human-readable signals that contributed
    ClassifiedAt time.Time
}
```

---

### T2 — Database migration: 003_classifications.sql

```sql
-- +goose Up

CREATE TABLE IF NOT EXISTS classifications (
    id             TEXT PRIMARY KEY,
    email_id       TEXT NOT NULL REFERENCES emails(id),
    level          TEXT NOT NULL,
    category       TEXT NOT NULL DEFAULT 'other',
    score          INTEGER NOT NULL DEFAULT 0,
    reason         TEXT NOT NULL DEFAULT '',  -- JSON array of reason strings
    classified_at  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS senders (
    id              TEXT PRIMARY KEY,
    email           TEXT NOT NULL UNIQUE,
    importance_score INTEGER NOT NULL DEFAULT 0,
    seen_count      INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
    id              TEXT PRIMARY KEY,
    domain          TEXT NOT NULL UNIQUE,
    importance_score INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS classifications;
DROP TABLE IF EXISTS senders;
DROP TABLE IF EXISTS domains;
```

---

### T3 — Repositories

**ClassificationRepo**

```go
func (r *ClassificationRepo) Save(ctx context.Context, c domain.Classification) error
func (r *ClassificationRepo) GetByEmailID(ctx context.Context, emailID string) (*domain.Classification, error)
```

**SenderRepo**

```go
func (r *SenderRepo) Get(ctx context.Context, email string) (*domain.Sender, error)
// Returns nil if not seen before
func (r *SenderRepo) Upsert(ctx context.Context, s domain.Sender) error
```

**DomainRepo**

```go
func (r *DomainRepo) Get(ctx context.Context, domain string) (*domain.DomainRecord, error)
func (r *DomainRepo) Upsert(ctx context.Context, d domain.DomainRecord) error
```

---

### T4 — IMAP client: additional header fields

Extend `email.Message` with extra header fields used by the scoring engine:

```go
type Message struct {
    // existing fields
    UID       uint32
    Subject   string
    FromEmail string
    FromName  string
    Date      time.Time
    // new
    InReplyTo       string // set → active conversation
    ListUnsubscribe string // set → likely newsletter
    Precedence      string // "bulk" or "list" → newsletter
    ToAddresses     []string
    CcAddresses     []string
}
```

Update `FetchSince` to request `BODY[HEADER.FIELDS (IN-REPLY-TO LIST-UNSUBSCRIBE PRECEDENCE TO CC)]` alongside `ENVELOPE`.

---

### T5 — Feature extraction

`internal/features/`

**EmailFeatures struct** (`features.go`)

```go
type EmailFeatures struct {
    // sender/domain
    FromEmail   string
    FromDomain  string
    // thread
    IsReply     bool  // InReplyTo is set
    // newsletter signals
    HasListUnsubscribe bool
    IsBulkPrecedence   bool
    // keyword groups detected in subject
    HasUrgentKeyword    bool
    HasMeetingKeyword   bool
    HasInvoiceKeyword   bool
    HasSecurityKeyword  bool
    HasDeadlineKeyword  bool
    HasInterviewKeyword bool
    HasGovernmentKeyword bool
    HasSchoolKeyword    bool
    // language
    Language string
    // sender/domain history (loaded from DB)
    SenderScore int
    DomainScore int
    SenderSeenCount int
}
```

**Extractor** (`extractor.go`)

```go
func Extract(msg email.Message, senderScore, domainScore, senderSeenCount int) EmailFeatures
```

Extracts domain from email address, detects language, matches keywords against subject.

**Keyword dictionaries** (`keywords.go`)

Keywords grouped by semantic meaning, covering English, Polish, Russian, Romanian, Italian, Belarusian, Ukrainian, Spanish, Portuguese, French, German, Kazakh, Yiddish:

```go
var urgent = []string{
    "urgent", "asap", "immediately", "critical",
    "pilny", "срочно", "urgent", "urgente",
}
var meeting = []string{
    "meeting", "call", "appointment", "spotkanie",
    "встреча", "întâlnire", "riunione",
}
var invoice = []string{
    "invoice", "payment", "bill", "faktura", "płatność",
    "счет", "оплата", "factură", "fattura",
}
// ... security, deadline, interview, government, school
```

**Language detection** (`language.go`)

Thin wrapper around `github.com/pemistahl/lingua-go`:

```go
func DetectLanguage(text string) string
// Returns ISO 639-1 code or "und" (undetermined).
// Supported: en, pl, ru, ro, it, be, uk, es, pt, fr, de, kk, yi
```

---

### T6 — Importance filter

`internal/importance/`

**Scoring** (`scoring.go`)

```go
func Score(f features.EmailFeatures) (score int, reasons []string)
```

Score components:

| Signal | Δ score | Reason text |
|--------|---------|-------------|
| Sender known, high score | +senderScore | sender known and trusted |
| Domain known, high score | +domainScore | domain previously important |
| `In-Reply-To` set | +20 | active conversation |
| `urgent` keyword | +25 | urgent keyword in subject |
| `meeting` keyword | +20 | meeting keyword in subject |
| `invoice` / `payment` keyword | +20 | invoice or payment keyword |
| `security` keyword | +25 | security-related keyword |
| `deadline` keyword | +20 | deadline keyword |
| `interview` keyword | +20 | interview keyword |
| `government` keyword or `.gov` domain | +30 | government sender |
| `school` keyword or `.edu` domain | +20 | school sender |
| `List-Unsubscribe` header present | –40 | newsletter (List-Unsubscribe) |
| `Precedence: bulk` or `Precedence: list` | –30 | bulk/list precedence |
| Unknown sender (seen_count == 0) | –10 | unknown sender |

Score is clamped to [0, 100].

**Filter** (`filter.go`)

```go
type Filter struct {
    SenderRepo *repo.SenderRepo
    DomainRepo *repo.DomainRepo
}

func (f *Filter) Classify(ctx context.Context, msg email.Message) (domain.Classification, error)
```

Sequence:
1. Load sender score and domain score from DB
2. Extract features
3. Compute score and reasons
4. Map score to level (Critical/Important/Maybe/Ignore)
5. Detect category from dominant keyword group
6. Return Classification

**Rules** (`rules.go`)

```go
func Level(score int) domain.ImportanceLevel
func Category(f features.EmailFeatures) domain.Category
```

Category is determined by the highest-weight keyword group detected. Defaults to `other`.

---

### T7 — Config additions

New settings key: `notification.min_importance`

Valid values: `critical`, `important`, `maybe` (default: `important`)

Add to `config.KnownKeys` and `config.Config`:

```go
type NotificationConfig struct {
    MinImportance domain.ImportanceLevel
}

type Config struct {
    // existing fields
    Notification NotificationConfig
}
```

Add to `init` wizard prompt and `applySettings`.

---

### T8 — Scheduler integration

Update `internal/scheduler/scheduler.go`:

1. Add `Filter *importance.Filter` and `ClassificationRepo *repo.ClassificationRepo` to `scheduler.Config`
2. In `processMessage`:
   a. Classify the message
   b. Save classification to DB
   c. If level < configured minimum → update email status to `ignored`, skip notification
   d. If level >= minimum → notify as before, update status to `notified`

```go
func (s *Scheduler) processMessage(ctx context.Context, msg email.Message) error {
    classification, err := s.cfg.Filter.Classify(ctx, msg)
    // ...
    if !s.shouldNotify(classification.Level) {
        // store email with status=ignored, store classification, done
        return nil
    }
    // existing notification path
}

func (s *Scheduler) shouldNotify(level domain.ImportanceLevel) bool {
    // compare level against cfg.MinImportance
}
```

3. Wire up in `cmd/email-agent/main.go`: create `Filter`, create `ClassificationRepo`, pass to scheduler.

---

### T9 — Tests

| Package | What to test |
|---------|--------------|
| `internal/features` | keyword detection for each group in each language; newsletter detection; language detection smoke test |
| `internal/importance` | score computation for each signal; level thresholds; category detection |
| `internal/db/repo` | ClassificationRepo save/get; SenderRepo upsert/get; DomainRepo upsert/get |
| `internal/scheduler` | important email → notified; newsletter → ignored; maybe → ignored by default |

---

### T10 — Update docs/db-schema.md

Add `classifications`, `senders`, `domains` tables to the ERD. Update the "current schema reflects migrations up to" line.

---

## Dependencies

No new external dependencies. All required packages are already present:

- `github.com/pemistahl/lingua-go` — language detection
- `github.com/pressly/goose/v3` — migrations

---

## Definition of Done

1. `make check` passes
2. Newsletter emails (`List-Unsubscribe` present) are not notified
3. Emails with `urgent`/`meeting`/`invoice` keywords in subject are notified
4. `email-agent config set notification.min_importance maybe` causes Maybe-level emails to also notify
5. Classification is stored in DB for every processed email
6. Every notification log line includes the classification level and reason

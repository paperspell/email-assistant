package domain

import "time"

// ImportanceLevel is the result of the importance classification pipeline.
type ImportanceLevel string

// Email status constants represent the processing lifecycle of a message.
const (
	LevelCritical  ImportanceLevel = "critical"
	LevelImportant ImportanceLevel = "important"
	LevelMaybe     ImportanceLevel = "maybe"
	LevelIgnore    ImportanceLevel = "ignore"
)

// Category groups an email by its primary topic.
type Category string

// Category values for email topic classification.
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

// Classification source constants.
const (
	SourceRuleBased = "rule_based"
	SourceLLM       = "llm" // prefix; full value is "llm:{provider}"
)

// Classification holds the result of classifying a single email.
type Classification struct {
	ID           string
	EmailID      string
	Level        ImportanceLevel
	Category     Category
	Score        int
	Reason       []string
	ClassifiedAt time.Time
	Source       string // SourceRuleBased or "llm:{provider}"
	Summary      string // empty for rule_based; LLM-generated sentence(s)
}

// Sender tracks per-sender statistics used by the importance filter. Scoped to a
// single account (the same address may carry different scores per mailbox).
type Sender struct {
	ID              string
	AccountID       string
	Email           string
	ImportanceScore int
	SeenCount       int
	UpdatedAt       time.Time
}

// Record tracks per-domain statistics used by the importance filter. Scoped to a
// single account.
type Record struct {
	ID              string
	AccountID       string
	Domain          string
	ImportanceScore int
	UpdatedAt       time.Time
}

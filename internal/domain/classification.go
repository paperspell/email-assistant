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

// Classification holds the result of classifying a single email.
type Classification struct {
	ID           string
	EmailID      string
	Level        ImportanceLevel
	Category     Category
	Score        int
	Reason       []string
	ClassifiedAt time.Time
}

// Sender tracks per-sender statistics used by the importance filter.
type Sender struct {
	ID              string
	Email           string
	ImportanceScore int
	SeenCount       int
	UpdatedAt       time.Time
}

// Record tracks per-domain statistics used by the importance filter.
type Record struct {
	ID              string
	Domain          string
	ImportanceScore int
	UpdatedAt       time.Time
}

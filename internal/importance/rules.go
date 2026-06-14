package importance

import (
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/features"
)

// Level maps a score to an ImportanceLevel.
func Level(score int) domain.ImportanceLevel {
	switch {
	case score >= 90:
		return domain.LevelCritical
	case score >= 70:
		return domain.LevelImportant
	case score >= 30:
		return domain.LevelMaybe
	default:
		return domain.LevelIgnore
	}
}

// Category determines the primary category from detected keyword signals.
// Defaults to CategoryOther when no specific group matched.
func Category(f features.EmailFeatures) domain.Category {
	switch {
	case f.HasGovernmentKeyword:
		return domain.CategoryGovernment
	case f.HasSecurityKeyword:
		return domain.CategorySecurity
	case f.HasInvoiceKeyword:
		return domain.CategoryFinance
	case f.HasInterviewKeyword:
		return domain.CategoryRecruiting
	case f.HasSchoolKeyword:
		return domain.CategorySchool
	case f.HasMeetingKeyword:
		return domain.CategoryWork
	case f.HasUrgentKeyword:
		return domain.CategoryWork
	case f.HasDeadlineKeyword:
		return domain.CategoryWork
	case f.HasListUnsubscribe || f.IsBulkPrecedence:
		return domain.CategoryMarketing
	default:
		return domain.CategoryOther
	}
}

package importance

import (
	"github.com/paperspell/email-assistant/internal/features"
)

const baseline = 40

// Score computes an importance score [0, 100] and a list of human-readable
// reasons explaining the result.
func Score(f features.EmailFeatures) (int, []string) {
	score := baseline
	var reasons []string

	// --- Negative signals (newsletters / bulk) ---
	if f.HasListUnsubscribe {
		score -= 40
		reasons = append(reasons, "newsletter (List-Unsubscribe header)")
	}
	if f.IsBulkPrecedence {
		score -= 30
		reasons = append(reasons, "bulk or list precedence")
	}

	// --- Sender / domain history ---
	if f.SenderSeenCount == 0 {
		score -= 10
		reasons = append(reasons, "unknown sender")
	} else if f.SenderScore > 0 {
		bonus := f.SenderScore / 5
		score += bonus
		reasons = append(reasons, "sender previously important")
	}
	if f.DomainScore > 0 {
		bonus := f.DomainScore / 5
		score += bonus
		reasons = append(reasons, "domain previously important")
	}

	// --- Thread signal ---
	if f.IsReply {
		score += 20
		reasons = append(reasons, "active conversation (In-Reply-To)")
	}

	// --- Keyword signals ---
	if f.HasGovernmentKeyword {
		score += 30
		reasons = append(reasons, "government sender or keyword")
	}
	if f.HasSecurityKeyword {
		score += 25
		reasons = append(reasons, "security keyword in subject")
	}
	if f.HasUrgentKeyword {
		score += 25
		reasons = append(reasons, "urgent keyword in subject")
	}
	if f.HasInvoiceKeyword {
		score += 20
		reasons = append(reasons, "invoice or payment keyword")
	}
	if f.HasMeetingKeyword {
		score += 20
		reasons = append(reasons, "meeting keyword in subject")
	}
	if f.HasDeadlineKeyword {
		score += 20
		reasons = append(reasons, "deadline keyword in subject")
	}
	if f.HasInterviewKeyword {
		score += 20
		reasons = append(reasons, "interview or job offer keyword")
	}
	if f.HasSchoolKeyword {
		score += 20
		reasons = append(reasons, "school keyword or education domain")
	}

	// Clamp to [0, 100]
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, reasons
}

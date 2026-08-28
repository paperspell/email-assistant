package importance

import (
	"fmt"

	"github.com/paperspell/email-assistant/internal/features"
)

const baseline = 40

// Score computes an importance score [0, 100] and a list of human-readable
// reasons explaining the result. Each reason includes its delta so the
// arithmetic is self-evident: sum all deltas + baseline = final score.
func Score(f features.EmailFeatures) (int, []string) {
	score := baseline
	reasons := []string{fmt.Sprintf("baseline: +%d", baseline)}

	// --- Negative signals (newsletters / bulk) ---
	// List-Unsubscribe marks bulk-capable sending infrastructure, not unimportant
	// mail: banks, government portals, invoices and school systems all set it. At
	// its former -40 it cancelled the whole baseline, so such mail scored 0 and
	// never reached the LLM. Kept as a mild hint instead, so a single positive
	// signal can still lift the message over the "maybe" line. Genuine bulk is
	// caught by Precedence below, and a message carrying both still lands at 0.
	if f.HasListUnsubscribe {
		score -= 15
		reasons = append(reasons, "bulk-capable sender (List-Unsubscribe header): -15")
	}
	if f.IsBulkPrecedence {
		score -= 30
		reasons = append(reasons, "bulk or list precedence: -30")
	}

	// --- Sender / domain history ---
	if f.SenderSeenCount == 0 {
		score -= 10
		reasons = append(reasons, "unknown sender: -10")
	} else if f.SenderScore > 0 {
		bonus := f.SenderScore / 5
		score += bonus
		reasons = append(reasons, fmt.Sprintf("sender previously important: +%d", bonus))
	}
	if f.DomainScore > 0 {
		bonus := f.DomainScore / 5
		score += bonus
		reasons = append(reasons, fmt.Sprintf("domain previously important: +%d", bonus))
	}

	// --- Thread signal ---
	if f.IsReply {
		score += 20
		reasons = append(reasons, "active conversation (In-Reply-To): +20")
	}

	// --- Keyword signals ---
	if f.HasGovernmentKeyword {
		score += 30
		reasons = append(reasons, "government sender or keyword: +30")
	}
	if f.HasSecurityKeyword {
		score += 25
		reasons = append(reasons, "security keyword in subject: +25")
	}
	if f.HasUrgentKeyword {
		score += 25
		reasons = append(reasons, "urgent keyword in subject: +25")
	}
	if f.HasInvoiceKeyword {
		score += 20
		reasons = append(reasons, "invoice or payment keyword: +20")
	}
	if f.HasMeetingKeyword {
		score += 20
		reasons = append(reasons, "meeting keyword in subject: +20")
	}
	if f.HasDeadlineKeyword {
		score += 20
		reasons = append(reasons, "deadline keyword in subject: +20")
	}
	if f.HasInterviewKeyword {
		score += 20
		reasons = append(reasons, "interview or job offer keyword: +20")
	}
	if f.HasSchoolKeyword {
		score += 20
		reasons = append(reasons, "school keyword or education domain: +20")
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

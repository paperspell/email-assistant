package filter

import (
	"strings"
	"unicode"
)

// maxPatternWords caps how many leading words the suggested subject pattern keeps,
// trading specificity for generalisation. The user can widen/narrow it via Edit.
const maxPatternWords = 3

// SuggestSubjectPattern derives a short, generalisable substring from a subject by
// normalising away the volatile parts (digits, dates, punctuation, emoji) and
// keeping the leading stable words. The result is matched as a cheap case-
// insensitive substring at runtime; this function runs only when a rule is created.
//
// Note: a richer LLM-authored suggestion is deferred — the classification-only
// llm.Provider has no general-completion entry point — so normalisation is used.
func SuggestSubjectPattern(subject string) string {
	var b strings.Builder
	for _, r := range subject {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(unicode.ToLower(r))
		case r == ' ' || r == '\t':
			b.WriteRune(' ')
		default:
			// digits, punctuation, emoji → separator
			b.WriteRune(' ')
		}
	}

	words := strings.Fields(b.String())
	kept := make([]string, 0, maxPatternWords)
	for _, w := range words {
		if len(w) < 2 { // drop stray single letters
			continue
		}
		kept = append(kept, w)
		if len(kept) == maxPatternWords {
			break
		}
	}

	pattern := strings.Join(kept, " ")
	if pattern == "" {
		// Nothing stable extracted: fall back to the trimmed, lowercased subject.
		return strings.ToLower(strings.TrimSpace(subject))
	}
	return pattern
}

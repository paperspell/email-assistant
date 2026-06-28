package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
)

func TestSuggestSubjectPattern(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"🎉 Flash Sale: 50% off ends tonight! (June 24)", "flash sale off"},
		{"Your weekly digest", "your weekly digest"},
		{"Receipt #4471 — order 9920", "receipt order"},
		{"RE: RE: meeting notes", "re re meeting"},
		{"1234 5678", "1234 5678"}, // no letters → fall back to lowercased subject
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, SuggestSubjectPattern(c.subject), "subject %q", c.subject)
	}
}

func TestSuggestSubjectPattern_MatchesSourceSubject(t *testing.T) {
	// A subject rule built from the suggestion must match the email it came from,
	// even across punctuation (normalised matching).
	subj := "Flash Sale: huge discounts!"
	p := SuggestSubjectPattern(subj)
	r := domain.FilterRule{
		Action: domain.RuleActionIgnore, Type: domain.RuleTypeSubject,
		Matcher: domain.MatcherSubstring, Value: p, Enabled: true,
	}
	assert.True(t, matches(r, email.Message{Subject: subj}),
		"pattern %q should match its source subject", p)
}

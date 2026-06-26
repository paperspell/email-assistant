package filter

import "github.com/paperspell/email-assistant/internal/domain"

// DefaultIgnoreClauses returns the Set A default LLM ignore clauses seeded for
// every account. They are nuanced ("... unless ...") so the LLM keeps an escape
// hatch, which is why they are safe to ship enabled. Must stay in sync with the
// historical backfill text in migration 010.
func DefaultIgnoreClauses() []string {
	return []string{
		"Ignore promotional and marketing emails (sales, discounts, product announcements, " +
			"webinars) unless tied to an active order, payment, shipping, or account security.",
		"Ignore automated social-media notifications: likes, reactions, new followers, " +
			`"people you may know", comment digests.`,
		"Ignore periodic newsletter digests unless from a sender previously marked important.",
	}
}

// ExampleRules returns the Set B example mechanical rules. They are shipped
// disabled by default (templates) so the user can enable/edit the ones they want;
// blunt active ignore rules risk dropping important mail.
func ExampleRules() []domain.FilterRule {
	return []domain.FilterRule{
		{
			Action:  domain.RuleActionIgnore,
			Type:    domain.RuleTypeDomain,
			Matcher: domain.MatcherExact,
			Value:   "facebookmail.com",
		},
		{
			Action:  domain.RuleActionIgnore,
			Type:    domain.RuleTypeListID,
			Matcher: domain.MatcherExact,
			Value:   "", // template: edit to the mailing list's List-Id, then enable
		},
		{
			Action:     domain.RuleActionIgnore,
			Type:       domain.RuleTypeSubject,
			Matcher:    domain.MatcherSubstring,
			Value:      "weekly digest", // template: edit the pattern, then enable
			ScopeKind:  "",              // global by default for the template
			ScopeValue: "",
		},
	}
}

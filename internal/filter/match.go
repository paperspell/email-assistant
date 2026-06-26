// Package filter evaluates per-account mechanical rules against an email before
// the LLM runs. Matching is cheap, deterministic, and side-effect free.
package filter

import (
	"strings"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/features"
)

// matches reports whether rule applies to msg. Pure and case-insensitive.
func matches(rule domain.FilterRule, msg email.Message) bool {
	switch rule.Type {
	case domain.RuleTypeSender:
		return strings.EqualFold(strings.TrimSpace(msg.FromEmail), strings.TrimSpace(rule.Value))
	case domain.RuleTypeDomain:
		return matchDomain(features.ExtractDomain(msg.FromEmail), rule.Value)
	case domain.RuleTypeListID:
		return rule.Value != "" && containsFold(msg.ListID, rule.Value)
	case domain.RuleTypeSubject:
		if rule.ScopeKind == domain.RuleTypeSender &&
			!strings.EqualFold(strings.TrimSpace(msg.FromEmail), strings.TrimSpace(rule.ScopeValue)) {
			return false
		}
		return rule.Value != "" && containsFold(msg.Subject, rule.Value)
	default:
		return false
	}
}

// matchDomain matches the exact domain or any subdomain of it
// (rule "shop.com" matches "deals.shop.com").
func matchDomain(fromDomain, ruleDomain string) bool {
	fromDomain = strings.ToLower(strings.TrimSpace(fromDomain))
	ruleDomain = strings.ToLower(strings.TrimSpace(ruleDomain))
	if fromDomain == "" || ruleDomain == "" {
		return false
	}
	return fromDomain == ruleDomain || strings.HasSuffix(fromDomain, "."+ruleDomain)
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(strings.TrimSpace(needle)))
}

package filter

import (
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
)

// Engine evaluates filter rules against emails. It is stateless; callers pass the
// account's enabled rules in.
type Engine struct{}

// NewEngine returns a rule engine.
func NewEngine() Engine { return Engine{} }

// typeRank ranks rule types by specificity (lower = more specific, wins ties).
func typeRank(t string) int {
	switch t {
	case domain.RuleTypeSender:
		return 0
	case domain.RuleTypeListID:
		return 1
	case domain.RuleTypeSubject:
		return 2
	case domain.RuleTypeDomain:
		return 3
	default:
		return 4
	}
}

// Evaluate returns the decision for msg under the precedence:
//
//	enabled allow rules beat enabled ignore rules; within an action the most
//	specific matching type wins (sender > list_id > subject > domain).
//
// ok is false when no rule matches (fall through to baseline/LLM).
func (Engine) Evaluate(
	rules []domain.FilterRule, msg email.Message,
) (action string, matched *domain.FilterRule, ok bool) {
	allow := bestMatch(rules, domain.RuleActionAllow, msg)
	if allow != nil {
		return domain.RuleActionAllow, allow, true
	}
	ignore := bestMatch(rules, domain.RuleActionIgnore, msg)
	if ignore != nil {
		return domain.RuleActionIgnore, ignore, true
	}
	return "", nil, false
}

// bestMatch returns the most specific enabled rule of the given action matching
// msg, or nil. On equal specificity the earliest-created rule (caller's order)
// wins, since callers list rules by created_at.
func bestMatch(rules []domain.FilterRule, action string, msg email.Message) *domain.FilterRule {
	var best *domain.FilterRule
	for i := range rules {
		r := &rules[i]
		if !r.Enabled || r.Action != action {
			continue
		}
		if !matches(*r, msg) {
			continue
		}
		if best == nil || typeRank(r.Type) < typeRank(best.Type) {
			best = r
		}
	}
	return best
}

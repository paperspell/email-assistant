package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
)

func rule(action, typ, value string) domain.FilterRule {
	return domain.FilterRule{
		ID: typ + ":" + value, Action: action, Type: typ,
		Matcher: domain.MatcherExact, Value: value, Enabled: true,
	}
}

func TestEngine_NoRules(t *testing.T) {
	_, _, ok := NewEngine().Evaluate(nil, email.Message{FromEmail: "a@b.com"})
	assert.False(t, ok)
}

func TestEngine_SenderIgnore(t *testing.T) {
	rules := []domain.FilterRule{rule(domain.RuleActionIgnore, domain.RuleTypeSender, "spam@x.com")}
	action, matched, ok := NewEngine().Evaluate(rules, email.Message{FromEmail: "SPAM@x.com"})
	assert.True(t, ok)
	assert.Equal(t, domain.RuleActionIgnore, action)
	assert.Equal(t, domain.RuleTypeSender, matched.Type)
}

func TestEngine_DomainMatchesSubdomain(t *testing.T) {
	rules := []domain.FilterRule{rule(domain.RuleActionIgnore, domain.RuleTypeDomain, "shop.com")}
	_, _, ok := NewEngine().Evaluate(rules, email.Message{FromEmail: "deals@news.shop.com"})
	assert.True(t, ok, "rule domain should match subdomains")

	_, _, ok = NewEngine().Evaluate(rules, email.Message{FromEmail: "x@notshop.com"})
	assert.False(t, ok)
}

func TestEngine_ListID(t *testing.T) {
	rules := []domain.FilterRule{rule(domain.RuleActionIgnore, domain.RuleTypeListID, "deals.shop.com")}
	_, _, ok := NewEngine().Evaluate(rules, email.Message{
		FromEmail: "a@b.com", ListID: "Shop Deals <deals.shop.com>",
	})
	assert.True(t, ok)
}

func TestEngine_SubjectSubstringAndScope(t *testing.T) {
	r := domain.FilterRule{
		ID: "s1", Action: domain.RuleActionIgnore, Type: domain.RuleTypeSubject,
		Matcher: domain.MatcherSubstring, Value: "flash sale", Enabled: true,
		ScopeKind: domain.RuleTypeSender, ScopeValue: "promo@x.com",
	}
	eng := NewEngine()

	// Matches: scoped sender + substring present.
	_, _, ok := eng.Evaluate([]domain.FilterRule{r}, email.Message{
		FromEmail: "promo@x.com", Subject: "Huge FLASH SALE today",
	})
	assert.True(t, ok)

	// Same subject, different sender → scope excludes it.
	_, _, ok = eng.Evaluate([]domain.FilterRule{r}, email.Message{
		FromEmail: "someone@else.com", Subject: "Huge FLASH SALE today",
	})
	assert.False(t, ok, "subject rule scoped to a sender must not match other senders")
}

func TestEngine_AllowBeatsIgnore(t *testing.T) {
	rules := []domain.FilterRule{
		rule(domain.RuleActionIgnore, domain.RuleTypeDomain, "x.com"),
		rule(domain.RuleActionAllow, domain.RuleTypeSender, "vip@x.com"),
	}
	action, _, ok := NewEngine().Evaluate(rules, email.Message{FromEmail: "vip@x.com"})
	assert.True(t, ok)
	assert.Equal(t, domain.RuleActionAllow, action, "allow rules take precedence over ignore")
}

func TestEngine_MoreSpecificTypeWins(t *testing.T) {
	rules := []domain.FilterRule{
		rule(domain.RuleActionIgnore, domain.RuleTypeDomain, "x.com"),
		rule(domain.RuleActionIgnore, domain.RuleTypeSender, "a@x.com"),
	}
	_, matched, ok := NewEngine().Evaluate(rules, email.Message{FromEmail: "a@x.com"})
	assert.True(t, ok)
	assert.Equal(t, domain.RuleTypeSender, matched.Type, "sender is more specific than domain")
}

func TestEngine_DisabledRuleIgnored(t *testing.T) {
	r := rule(domain.RuleActionIgnore, domain.RuleTypeSender, "a@x.com")
	r.Enabled = false
	_, _, ok := NewEngine().Evaluate([]domain.FilterRule{r}, email.Message{FromEmail: "a@x.com"})
	assert.False(t, ok)
}

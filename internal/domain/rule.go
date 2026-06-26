package domain

import "time"

// Filter rule actions.
const (
	// RuleActionIgnore drops a matching email before the LLM runs.
	RuleActionIgnore = "ignore"
	// RuleActionAllow forces a matching email to be treated as important.
	RuleActionAllow = "allow"
)

// Filter rule types (the email attribute a rule matches on).
const (
	RuleTypeSender  = "sender"  // exact From address
	RuleTypeDomain  = "domain"  // From address domain
	RuleTypeListID  = "list_id" // List-Id header
	RuleTypeSubject = "subject" // subject substring
)

// Filter rule matchers.
const (
	MatcherExact     = "exact"
	MatcherSubstring = "substring"
)

// Rule/clause provenance.
const (
	RuleSourceUser    = "user"
	RuleSourceDefault = "default"
)

// FilterRule is a per-account mechanical rule consulted before the LLM. The most
// specific matching rule wins (see internal/filter for precedence).
type FilterRule struct {
	ID         string
	AccountID  string
	Action     string // RuleActionIgnore | RuleActionAllow
	Type       string // RuleTypeSender | RuleTypeDomain | RuleTypeListID | RuleTypeSubject
	Matcher    string // MatcherExact | MatcherSubstring
	Value      string
	ScopeKind  string // subject rules: "sender" binds the rule to ScopeValue; "" = unscoped
	ScopeValue string
	Enabled    bool
	Source     string // RuleSourceUser | RuleSourceDefault
	CreatedAt  time.Time
}

// LLMClause is a per-account natural-language ignore instruction injected into the
// classification system prompt. Clauses are ignore-only in this stage.
type LLMClause struct {
	ID        string
	AccountID string
	Text      string
	Enabled   bool
	Source    string // RuleSourceUser | RuleSourceDefault
	CreatedAt time.Time
}

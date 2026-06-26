package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func TestRuleRepo_AddListIndexPerAccount(t *testing.T) {
	rr := NewRuleRepo(openTestDB(t))
	ctx := context.Background()

	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "r1", AccountID: "acc-a", Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeDomain, Value: "x.com", Enabled: true,
	}))
	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "r2", AccountID: "acc-a", Action: domain.RuleActionAllow,
		Type: domain.RuleTypeSender, Value: "vip@x.com", Enabled: true,
	}))
	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "r3", AccountID: "acc-b", Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeDomain, Value: "y.com", Enabled: true,
	}))

	a, err := rr.List(ctx, "acc-a")
	require.NoError(t, err)
	require.Len(t, a, 2, "per-account isolation")

	second, err := rr.GetByIndex(ctx, "acc-a", 2)
	require.NoError(t, err)
	assert.Equal(t, "r2", second.ID, "1-based indexing follows insertion order")

	_, err = rr.GetByIndex(ctx, "acc-a", 3)
	assert.Error(t, err, "out-of-range index errors")
}

func TestRuleRepo_EnableEditDelete(t *testing.T) {
	rr := NewRuleRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "r1", AccountID: "a", Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeSubject, Matcher: domain.MatcherSubstring,
		Value: "old", Enabled: false,
	}))

	require.NoError(t, rr.SetEnabled(ctx, "r1", true))
	require.NoError(t, rr.UpdateValue(ctx, "r1", "new pattern", domain.RuleTypeSender, "boss@x.com"))

	got, err := rr.GetByIndex(ctx, "a", 1)
	require.NoError(t, err)
	assert.True(t, got.Enabled)
	assert.Equal(t, "new pattern", got.Value)
	assert.Equal(t, "boss@x.com", got.ScopeValue)

	require.NoError(t, rr.Delete(ctx, "r1"))
	list, err := rr.List(ctx, "a")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRuleRepo_ListEnabledOnly(t *testing.T) {
	rr := NewRuleRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "on", AccountID: "a", Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeDomain, Value: "x.com", Enabled: true,
	}))
	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "off", AccountID: "a", Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeDomain, Value: "y.com", Enabled: false,
	}))

	enabled, err := rr.ListEnabled(ctx, "a")
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	assert.Equal(t, "on", enabled[0].ID)
}

func TestClauseRepo_AddActiveTextsDelete(t *testing.T) {
	cr := NewClauseRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, cr.Add(ctx, domain.LLMClause{ID: "c1", AccountID: "a", Text: "Ignore promo", Enabled: true}))
	require.NoError(t, cr.Add(ctx, domain.LLMClause{ID: "c2", AccountID: "a", Text: "Ignore social", Enabled: false}))
	require.NoError(t, cr.Add(ctx, domain.LLMClause{ID: "c3", AccountID: "b", Text: "Other account", Enabled: true}))

	texts, err := cr.ActiveTexts(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, []string{"Ignore promo"}, texts, "only enabled clauses for the account")

	n, err := cr.Count(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	clause, err := cr.GetByIndex(ctx, "a", 2)
	require.NoError(t, err)
	require.NoError(t, cr.Delete(ctx, clause.ID))
	left, err := cr.List(ctx, "a")
	require.NoError(t, err)
	assert.Len(t, left, 1)
}

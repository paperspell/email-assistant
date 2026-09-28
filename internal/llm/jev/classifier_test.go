package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"
)

// answers builds a response to the classifier's questions: the level with its
// probabilities, a category, and a noul value per extra question id.
func answers(levels map[string]float64, confidence float64, category string, nouls map[string]float64) string {
	best, bestP := "", -1.0
	for l, p := range levels {
		if p > bestP {
			best, bestP = l, p
		}
	}
	a := map[string]any{
		qLevel: map[string]any{
			"type": "choice", "choice": best, "probabilities": levels, "confidence": confidence,
		},
		qCategory: map[string]any{"type": "choice", "choice": category},
	}
	for id, p := range nouls {
		a[id] = map[string]any{"type": "noul", "noul": p}
	}
	b, _ := json.Marshal(map[string]any{"model": "jev-1.13.0", "answers": a, "usage": map[string]int{"input_tokens": 300}})
	return string(b)
}

// sentRequest decodes the request the stub received.
func sentRequest(t *testing.T, s *stub) (map[string]any, map[string]any) {
	t.Helper()
	require.Equal(t, 1, s.calls())
	var got struct {
		State     map[string]any `json:"state"`
		Questions map[string]any `json:"questions"`
	}
	require.NoError(t, json.Unmarshal(s.bodies[0], &got))
	return got.State, got.Questions
}

func TestClassify_AsksLevelAndCategoryAboutTheEmail(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusOK, body: answers(
		map[string]float64{"critical": 0.05, "important": 0.8, "maybe": 0.1, "ignore": 0.05}, 0.7, "finance", nil)})

	got, err := c.Classify(context.Background(), llm.ClassifyRequest{
		FromEmail: "billing@bank.example", FromName: "Bank", Subject: "Payment failed",
		To: []string{"me@example.com"}, Body: "Your card was declined.", HasListUnsubscribe: true,
	})
	require.NoError(t, err)

	state, questions := sentRequest(t, s)
	assert.Equal(t, "Bank <billing@bank.example>", state["from"])
	assert.Equal(t, []any{"me@example.com"}, state["to"])
	assert.Equal(t, "Payment failed", state["subject"])
	assert.Equal(t, "Your card was declined.", state["body"])
	assert.Equal(t, "no", state["is_reply"])
	assert.Equal(t, "yes", state["has_unsubscribe_header"])
	assert.NotContains(t, state, "mailbox_owner", "an unfocused account is not told whose mailbox it is")
	assert.Len(t, questions, 2)
	assert.Contains(t, questions, qLevel)
	assert.Contains(t, questions, qCategory)

	assert.Equal(t, domain.LevelImportant, got.Level)
	assert.Equal(t, domain.CategoryFinance, got.Category)
	assert.Empty(t, got.Summary, "Jev does not write")
	assert.Equal(t, []string{"jev: important (p 0.80, confidence 0.70)"}, got.Reasons)
}

func TestClassify_LevelOptionsRunFromMostImportant(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusOK, body: answers(map[string]float64{"ignore": 1}, 1, "other", nil)})

	_, err := c.Classify(context.Background(), llm.ClassifyRequest{Subject: "x"})
	require.NoError(t, err)

	body := string(s.bodies[0])
	order := []string{`"critical":`, `"important":`, `"maybe":`, `"ignore":`}
	for i := 1; i < len(order); i++ {
		assert.Less(t, strings.Index(body, order[i-1]), strings.Index(body, order[i]), order[i])
	}
}

func TestClassify_FocusedAccountIsAskedAboutItsFocus(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusOK, body: answers(
		map[string]float64{"important": 0.9, "maybe": 0.1}, 0.8, "work", map[string]float64{qInFocus: 0.2})})

	got, err := c.Classify(context.Background(), llm.ClassifyRequest{
		Subject:    "Build failed",
		Owner:      llm.Owner{Email: "me@work.example", Name: "Me Myself", Aliases: []string{"memyself"}},
		Focus:      "only mail addressed to me\nor mentioning me",
		FocusFacts: []string{"GitHub reason: ci_activity"},
	})
	require.NoError(t, err)

	state, questions := sentRequest(t, s)
	assert.Equal(t, "Me Myself <me@work.example>", state["mailbox_owner"])
	assert.Equal(t, []any{"memyself"}, state["owner_also_addressed_as"])
	assert.Equal(t, []any{"GitHub reason: ci_activity"}, state["established_facts"])
	require.Contains(t, questions, qInFocus)
	instructions := questions[qInFocus].(map[string]any)["instructions"].(string)
	assert.Contains(t, instructions, `"only mail addressed to me or mentioning me"`, "the focus is quoted on one line")

	assert.Equal(t, domain.LevelIgnore, got.Level, "out of focus is ignored however important it looks")
	assert.LessOrEqual(t, got.Score, 29)
	assert.Contains(t, got.Reasons, "outside the owner's focus (p 0.20)")
}

func TestClassify_EachIgnoreClauseIsItsOwnQuestion(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusOK, body: answers(
		map[string]float64{"maybe": 0.6, "important": 0.4}, 0.5, "shopping",
		map[string]float64{"ignore_0": 0.1, "ignore_1": 0.85})})

	got, err := c.Classify(context.Background(), llm.ClassifyRequest{
		Subject:       "Your order shipped",
		IgnoreClauses: []string{"school newsletters", "  ", "delivery updates"},
	})
	require.NoError(t, err)

	_, questions := sentRequest(t, s)
	assert.Len(t, questions, 4, "level, category and one per non-blank clause")
	assert.Contains(t, questions["ignore_1"].(map[string]any)["instructions"], `"delivery updates"`)

	assert.Equal(t, domain.LevelIgnore, got.Level)
	assert.Contains(t, got.Reasons, `matches ignore rule "delivery updates" (p 0.85)`)
	assert.NotContains(t, strings.Join(got.Reasons, "\n"), "school newsletters")
}

func TestClassify_BoundsALongBody(t *testing.T) {
	c, s, _ := newStub(t, reply{status: http.StatusOK, body: answers(map[string]float64{"ignore": 1}, 1, "other", nil)})

	_, err := c.Classify(context.Background(), llm.ClassifyRequest{
		Subject: "x", Body: strings.Repeat("я", maxBodyRunes+500),
	})
	require.NoError(t, err)

	state, _ := sentRequest(t, s)
	assert.Len(t, []rune(state["body"].(string)), maxBodyRunes, "cut on a rune, never inside a letter")
}

func TestClassify_ErrorsAreWrapped(t *testing.T) {
	c, _, _ := newStub(t, reply{status: http.StatusPaymentRequired})

	_, err := c.Classify(context.Background(), llm.ClassifyRequest{Subject: "x"})

	require.ErrorIs(t, err, llm.ErrOutOfCredits)
	assert.Contains(t, err.Error(), "jev classify")
}

func TestVerdictResult_ScoreFollowsProbabilitiesWithinTheLevelBand(t *testing.T) {
	tests := []struct {
		name   string
		level  domain.ImportanceLevel
		levels map[domain.ImportanceLevel]float64
		want   int
	}{
		{"certain ignore sits mid-band", domain.LevelIgnore,
			map[domain.ImportanceLevel]float64{domain.LevelIgnore: 1}, 15},
		{"ignore leaning to maybe scores higher", domain.LevelIgnore,
			map[domain.ImportanceLevel]float64{domain.LevelIgnore: 0.6, domain.LevelMaybe: 0.4}, 29},
		{"certain critical", domain.LevelCritical,
			map[domain.ImportanceLevel]float64{domain.LevelCritical: 1}, 95},
		{"narrow important is kept in its band", domain.LevelImportant,
			map[domain.ImportanceLevel]float64{domain.LevelImportant: 0.5, domain.LevelMaybe: 0.3, domain.LevelIgnore: 0.2}, 70},
		{"maybe", domain.LevelMaybe,
			map[domain.ImportanceLevel]float64{domain.LevelMaybe: 0.7, domain.LevelImportant: 0.3}, 59},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Verdict{Level: tt.level, Levels: tt.levels, InFocus: 1, Category: domain.CategoryOther}.Result()

			assert.Equal(t, tt.level, got.Level)
			assert.Equal(t, tt.want, got.Score)
		})
	}
}

func TestVerdictResult_ForcedIgnoreKeepsScoreBelowThirty(t *testing.T) {
	v := Verdict{
		Level:   domain.LevelCritical,
		Levels:  map[domain.ImportanceLevel]float64{domain.LevelCritical: 1},
		InFocus: 1,
		Clauses: []ClauseMatch{{Clause: "bank ads", P: 0.5}},
	}

	got := v.Result()

	assert.Equal(t, domain.LevelIgnore, got.Level, "a probability of exactly one half counts as a match")
	assert.Equal(t, 29, got.Score)
	assert.Equal(t, "jev: critical (p 1.00, confidence 0.00)", got.Reasons[0], "Jev's own call stays first")
}

func TestName(t *testing.T) {
	assert.Equal(t, "jev", New("k", "").Name())
}

var _ llm.Provider = (*Client)(nil)

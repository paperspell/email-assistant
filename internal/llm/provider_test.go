package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemPrompt_NoClauses_EqualsBase(t *testing.T) {
	assert.Equal(t, systemPrompt, SystemPrompt(nil, ""))
	assert.Equal(t, systemPrompt, SystemPrompt([]string{}, ""))
}

func TestSystemPrompt_WithClauses_AppendsBulletedList(t *testing.T) {
	p := SystemPrompt([]string{"Ignore newsletters", "Ignore social noise"}, "")
	assert.True(t, strings.HasPrefix(p, systemPrompt), "base prompt is preserved")
	assert.Contains(t, p, "user-defined ignore rules")
	assert.Contains(t, p, "- Ignore newsletters")
	assert.Contains(t, p, "- Ignore social noise")
}

func TestSystemPrompt_SkipsBlankClauses(t *testing.T) {
	p := SystemPrompt([]string{"  ", "Ignore X"}, "")
	assert.Contains(t, p, "- Ignore X")
	assert.NotContains(t, p, "- \n")
}

func TestSystemPrompt_SummaryLanguage(t *testing.T) {
	p := SystemPrompt(nil, "Russian")

	assert.Contains(t, p, "Russian", "язык резюме передан модели")
	assert.Contains(t, p, `"summary"`, "инструкция касается именно поля summary")
	assert.Contains(t, p, systemPrompt, "базовый промпт сохранён")
}

func TestSystemPrompt_LanguageAndClausesCombine(t *testing.T) {
	p := SystemPrompt([]string{"Ignore newsletters"}, "Russian")

	assert.Contains(t, p, "Russian")
	assert.Contains(t, p, "Ignore newsletters")
}

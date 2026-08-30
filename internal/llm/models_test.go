package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestedModels_PerProvider(t *testing.T) {
	assert.NotEmpty(t, SuggestedModels("anthropic"))
	assert.NotEmpty(t, SuggestedModels("openai"))
	assert.NotEmpty(t, SuggestedModels("gemini"))
	assert.Empty(t, SuggestedModels("mistral"), "неизвестный провайдер не предлагает моделей")

	for _, provider := range []string{"anthropic", "openai", "gemini"} {
		for _, c := range SuggestedModels(provider) {
			assert.NotEmpty(t, c.ID, provider)
			assert.NotEmpty(t, c.Hint, provider)
			// Подсказка намеренно без цен: они устаревают быстрее файла.
			assert.NotContains(t, c.Hint, "$", "подсказка не должна содержать цену")
		}
	}
}

func TestDefaultModel_IsFirstSuggestion(t *testing.T) {
	assert.Equal(t, "claude-sonnet-5", DefaultModel("anthropic"))
	assert.Equal(t, "gpt-5.6-terra", DefaultModel("openai"))
	assert.Equal(t, "gemini-2.5-flash", DefaultModel("gemini"))
	assert.Empty(t, DefaultModel("nope"))
}

func TestResolveModelChoice(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		current string
		want    string
	}{
		{"номер выбирает из списка", "2", "", "claude-opus-5"},
		{"первый номер", "1", "", "claude-sonnet-5"},
		{"произвольный id принимается дословно", "claude-future-9", "", "claude-future-9"},
		{"пустой ответ сохраняет текущее", "", "claude-haiku-4-5", "claude-haiku-4-5"},
		{"пустой ответ без текущего даёт умолчание", "", "", "claude-sonnet-5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveModelChoice("anthropic", tt.answer, tt.current)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveModelChoice_NumberOutOfRange(t *testing.T) {
	_, err := ResolveModelChoice("anthropic", "9", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1-3")
}

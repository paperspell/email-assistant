package llm

import "fmt"

// ModelChoice is one entry of a provider's suggested model list. Hint is a short
// relative characterisation, never a price: prices change more often than this
// file is edited, and a stale number is worse than none.
type ModelChoice struct {
	ID   string
	Hint string
}

// SuggestedModels lists the models offered at setup for a provider, cheapest
// last. The lists are deliberately short — three well-separated options beat a
// full catalogue when the task is picking one classifier — and never exhaustive:
// the setup wizard always accepts a model ID typed by hand, so a model released
// after this file was written is usable the day it ships.
func SuggestedModels(provider string) []ModelChoice {
	switch provider {
	case "anthropic":
		return []ModelChoice{
			{ID: "claude-sonnet-5", Hint: "recommended — balanced judgement and cost"},
			{ID: "claude-opus-5", Hint: "highest quality, several times the cost"},
			{ID: "claude-haiku-4-5", Hint: "cheapest and fastest, misses nuance more often"},
		}
	case "openai":
		return []ModelChoice{
			{ID: "gpt-5.6-terra", Hint: "recommended — balanced judgement and cost"},
			{ID: "gpt-5.6-sol", Hint: "highest quality, several times the cost"},
			{ID: "gpt-5.6-luna", Hint: "cheapest and fastest, misses nuance more often"},
		}
	case "gemini":
		return []ModelChoice{
			{ID: "gemini-2.5-flash", Hint: "recommended — balanced judgement and cost"},
			{ID: "gemini-2.5-pro", Hint: "highest quality, several times the cost"},
			{ID: "gemini-2.5-flash-lite", Hint: "cheapest and fastest, misses nuance more often"},
		}
	default:
		return nil
	}
}

// DefaultModel returns the model used when the user picks none.
func DefaultModel(provider string) string {
	choices := SuggestedModels(provider)
	if len(choices) == 0 {
		return ""
	}
	return choices[0].ID
}

// ResolveModelChoice maps what the user typed at the model prompt to a model ID.
// A number selects from the suggested list; anything else is taken literally, so
// an unlisted or brand-new model can be used without a code change. An empty
// answer keeps current (or the provider default when there is no current value).
func ResolveModelChoice(provider, answer, current string) (string, error) {
	choices := SuggestedModels(provider)
	if answer == "" {
		if current != "" {
			return current, nil
		}
		return DefaultModel(provider), nil
	}
	var n int
	if _, err := fmt.Sscanf(answer, "%d", &n); err == nil && fmt.Sprint(n) == answer {
		if n < 1 || n > len(choices) {
			return "", fmt.Errorf("pick 1-%d or type a model id, got %q", len(choices), answer)
		}
		return choices[n-1].ID, nil
	}
	return answer, nil
}

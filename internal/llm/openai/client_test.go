package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"

	openaiclient "github.com/paperspell/email-assistant/internal/llm/openai"
)

func fakeOpenAIServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{
				"prompt_tokens":     50,
				"completion_tokens": 30,
				"total_tokens":      80,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
}

func TestOpenAIClient_Classify(t *testing.T) {
	content, _ := json.Marshal(map[string]any{
		"level":    "important",
		"category": "finance",
		"score":    80,
		"reasons":  []string{"invoice keyword: +20", "baseline: +40"},
		"summary":  "Invoice for Q2 services attached.",
	})

	srv := fakeOpenAIServer(t, string(content))
	defer srv.Close()

	client := openaiclient.NewWithBaseURL("test-key", "", srv.URL)

	result, err := client.Classify(context.Background(), llm.ClassifyRequest{
		FromEmail: "billing@company.com",
		Subject:   "Invoice #123",
		Language:  "en",
	})
	require.NoError(t, err)

	assert.Equal(t, domain.LevelImportant, result.Level)
	assert.Equal(t, domain.CategoryFinance, result.Category)
	assert.Equal(t, 80, result.Score)
	assert.Equal(t, "Invoice for Q2 services attached.", result.Summary)
	assert.NotEmpty(t, result.Reasons)
}

func TestOpenAIClient_Name(t *testing.T) {
	client := openaiclient.New("key", "")
	assert.Equal(t, "openai", client.Name())
}

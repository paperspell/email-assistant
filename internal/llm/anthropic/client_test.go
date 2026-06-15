package anthropic_test

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

	anthropicclient "github.com/paperspell/email-assistant/internal/llm/anthropic"
)

func fakeAnthropicResponse(t *testing.T, input map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		inputJSON, _ := json.Marshal(input)
		resp := map[string]any{
			"id":   "msg_test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{{
				"type":  "tool_use",
				"id":    "toolu_test",
				"name":  "classify_email",
				"input": json.RawMessage(inputJSON),
			}},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "tool_use",
			"usage":       map[string]int{"input_tokens": 50, "output_tokens": 30},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
}

func TestAnthropicClient_Classify(t *testing.T) {
	srv := fakeAnthropicResponse(t, map[string]any{
		"level":    "important",
		"category": "work",
		"score":    75,
		"reasons":  []string{"urgent keyword: +25", "baseline: +40"},
		"summary":  "Urgent work meeting scheduled for tomorrow.",
	})
	defer srv.Close()

	client := anthropicclient.NewWithBaseURL("test-key", "", srv.URL)

	result, err := client.Classify(context.Background(), llm.ClassifyRequest{
		FromEmail: "boss@work.com",
		FromName:  "Boss",
		Subject:   "urgent meeting tomorrow",
		Language:  "en",
	})
	require.NoError(t, err)

	assert.Equal(t, domain.LevelImportant, result.Level)
	assert.Equal(t, domain.CategoryWork, result.Category)
	assert.Equal(t, 75, result.Score)
	assert.Equal(t, "Urgent work meeting scheduled for tomorrow.", result.Summary)
	assert.NotEmpty(t, result.Reasons)
}

func TestAnthropicClient_Name(t *testing.T) {
	client := anthropicclient.New("key", "")
	assert.Equal(t, "anthropic", client.Name())
}

func TestAnthropicClient_FormatUserMessage_IncludesBody(t *testing.T) {
	req := llm.ClassifyRequest{
		FromEmail: "x@y.com",
		Subject:   "Hello",
		Language:  "en",
		Body:      "Please see the attached document.",
	}
	msg := llm.FormatUserMessage(req)
	assert.Contains(t, msg, "Body:")
	assert.Contains(t, msg, "Please see the attached document.")
}

func TestAnthropicClient_FormatUserMessage_NoBody(t *testing.T) {
	req := llm.ClassifyRequest{
		FromEmail: "x@y.com",
		Subject:   "Hello",
		Language:  "en",
	}
	msg := llm.FormatUserMessage(req)
	assert.NotContains(t, msg, "Body:")
}

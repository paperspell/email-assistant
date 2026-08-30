package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"
)

func testRequest() llm.ClassifyRequest {
	return llm.ClassifyRequest{
		FromEmail: "billing@example.com",
		Subject:   "Payment receipt",
		Language:  "en",
	}
}

// serve starts a stub API returning the given status and body, and captures the
// request the client sent.
func serve(t *testing.T, status int, body string) (*Client, *http.Request, *[]byte) {
	t.Helper()
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotReq = r
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewWithBaseURL("test-key", "gemini-2.5-flash", srv.URL), gotReq, &gotBody
}

func TestClassify_ParsesResult(t *testing.T) {
	inner, err := json.Marshal(map[string]any{
		"level": "important", "category": "finance", "score": 82,
		"reasons": []string{"payment confirmation"}, "summary": "Card charged EUR 12.",
	})
	require.NoError(t, err)
	resp, err := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": string(inner)}}},
		}},
	})
	require.NoError(t, err)

	c, _, _ := serve(t, http.StatusOK, string(resp))
	got, err := c.Classify(context.Background(), testRequest())

	require.NoError(t, err)
	assert.Equal(t, domain.LevelImportant, got.Level)
	assert.Equal(t, domain.Category("finance"), got.Category)
	assert.Equal(t, 82, got.Score)
	assert.Equal(t, "Card charged EUR 12.", got.Summary)
	assert.Equal(t, []string{"payment confirmation"}, got.Reasons)
}

func TestClassify_SendsKeyAsHeaderNotQuery(t *testing.T) {
	// The API also accepts ?key=, but query strings land in proxy and server
	// logs; the header does not.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-goog-api-key"))
		assert.NotContains(t, r.URL.RawQuery, "test-key")
		assert.Contains(t, r.URL.Path, "/models/gemini-2.5-flash:generateContent")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{}"}]}}]}`)
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-key", "gemini-2.5-flash", srv.URL)
	_, err := c.Classify(context.Background(), testRequest())
	require.NoError(t, err)
}

func TestClassify_AsksForJSONAndCarriesTheSystemPrompt(t *testing.T) {
	c, _, body := serve(t, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"{}"}]}}]}`)
	req := testRequest()
	req.SummaryLanguage = "Russian"
	_, err := c.Classify(context.Background(), req)
	require.NoError(t, err)

	var sent generateRequest
	require.NoError(t, json.Unmarshal(*body, &sent))
	assert.Equal(t, "application/json", sent.GenerationConfig.ResponseMIMEType)
	require.NotNil(t, sent.SystemInstruction)
	assert.Contains(t, sent.SystemInstruction.Parts[0].Text, "email importance classifier")
	// The one setting that drives the summary language must reach the model.
	assert.Contains(t, sent.SystemInstruction.Parts[0].Text, `"Russian"`)
	assert.Contains(t, sent.Contents[0].Parts[0].Text, "billing@example.com")
}

func TestClassify_JoinsSplitParts(t *testing.T) {
	// One JSON document may arrive split across parts; concatenating them is the
	// difference between a parsed result and a parse error.
	c, _, _ := serve(t, http.StatusOK,
		`{"candidates":[{"content":{"parts":[{"text":"{\"level\":\"ig"},{"text":"nore\",\"score\":5}"}]}}]}`)

	got, err := c.Classify(context.Background(), testRequest())

	require.NoError(t, err)
	assert.Equal(t, domain.LevelIgnore, got.Level)
	assert.Equal(t, 5, got.Score)
}

func TestClassify_HTTPErrorCarriesTheBody(t *testing.T) {
	// The operator needs the API's own explanation — a bare status code does not
	// distinguish a bad key from an unknown model.
	c, _, _ := serve(t, http.StatusBadRequest,
		`{"error":{"message":"API key not valid","status":"INVALID_ARGUMENT"}}`)

	_, err := c.Classify(context.Background(), testRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "API key not valid")
}

func TestClassify_BlockedPromptIsNamed(t *testing.T) {
	c, _, _ := serve(t, http.StatusOK, `{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}`)

	_, err := c.Classify(context.Background(), testRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAFETY")
}

func TestClassify_TruncatedCandidateReportsFinishReason(t *testing.T) {
	// A candidate with no text is usually a token cap; the finish reason is the
	// only thing that says so.
	c, _, _ := serve(t, http.StatusOK,
		`{"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}]}`)

	_, err := c.Classify(context.Background(), testRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MAX_TOKENS")
}

func TestNew_DefaultsTheModel(t *testing.T) {
	assert.Equal(t, llm.DefaultModel("gemini"), New("k", "").model)
	assert.Equal(t, "gemini-2.5-pro", New("k", "gemini-2.5-pro").model)
}

func TestName(t *testing.T) {
	assert.Equal(t, "gemini", New("k", "").Name())
	var _ llm.Provider = New("k", "")
}

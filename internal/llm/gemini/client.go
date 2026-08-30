// Package gemini classifies emails with Google's Gemini models.
//
// The Generative Language API is called over plain HTTP rather than through the
// Google SDK: exactly one endpoint is used, the request and response shapes
// below are the whole surface, and the SDK would pull in a large dependency tree
// for no benefit here.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"
)

const (
	defaultBaseURL  = "https://generativelanguage.googleapis.com/v1beta"
	maxOutputTokens = 512
	requestTimeout  = 60 * time.Second
	// maxErrorBody bounds what is read from a failed response before it is put
	// into an error string.
	maxErrorBody = 2048
)

// Client classifies emails using the Gemini API in JSON mode.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// New creates a Client. model may be empty to use the default.
func New(apiKey, model string) *Client {
	return NewWithBaseURL(apiKey, model, defaultBaseURL)
}

// NewWithBaseURL creates a Client pointing at a custom base URL (used in tests).
func NewWithBaseURL(apiKey, model, baseURL string) *Client {
	if model == "" {
		model = llm.DefaultModel("gemini")
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// Name returns the provider identifier used in classification source tags.
func (c *Client) Name() string { return "gemini" }

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type generationConfig struct {
	ResponseMIMEType string `json:"responseMimeType"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
}

type generateRequest struct {
	SystemInstruction *content         `json:"system_instruction,omitempty"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type candidate struct {
	Content      content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

type generateResponse struct {
	Candidates []candidate `json:"candidates"`
	// PromptFeedback carries the reason a prompt was refused outright, in which
	// case no candidate is returned at all.
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

// Classify sends the email to a Gemini model and returns a structured result.
func (c *Client) Classify(ctx context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error) {
	body, err := json.Marshal(generateRequest{
		SystemInstruction: &content{
			Parts: []part{{Text: llm.SystemPrompt(req.IgnoreClauses, req.SummaryLanguage)}},
		},
		Contents: []content{{
			Role:  "user",
			Parts: []part{{Text: llm.FormatUserMessage(req)}},
		}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
			MaxOutputTokens:  maxOutputTokens,
		},
	})
	if err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: marshal request: %w", err)
	}

	// The model name is path data and the key is a header, never a query
	// parameter: query strings end up in proxy and server logs.
	endpoint := c.baseURL + "/models/" + url.PathEscape(c.model) + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		// A read error here loses the explanation but not the status code, which
		// is the part the operator cannot do without.
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if readErr != nil {
			return llm.ClassifyResult{}, fmt.Errorf("gemini classify: http %d", resp.StatusCode)
		}
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: http %d: %s",
			resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var out generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: decode response: %w", err)
	}
	if len(out.Candidates) == 0 {
		if reason := out.PromptFeedback.BlockReason; reason != "" {
			return llm.ClassifyResult{}, fmt.Errorf("gemini classify: prompt blocked (%s)", reason)
		}
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: empty response")
	}

	text := candidateText(out.Candidates[0])
	if strings.TrimSpace(text) == "" {
		// A truncated or filtered candidate carries no text; the finish reason is
		// what tells the operator which of the two happened.
		return llm.ClassifyResult{}, fmt.Errorf("gemini classify: no text in response (finish reason %q)",
			out.Candidates[0].FinishReason)
	}
	return parseJSON(text)
}

// candidateText joins the parts of a candidate. The API may split one JSON
// document across several parts, so they are concatenated before parsing.
func candidateText(c candidate) string {
	var b strings.Builder
	for _, p := range c.Content.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

type jsonOutput struct {
	Level    string   `json:"level"`
	Category string   `json:"category"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
	Summary  string   `json:"summary"`
}

func parseJSON(text string) (llm.ClassifyResult, error) {
	var out jsonOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("parse gemini json: %w", err)
	}
	return llm.ClassifyResult{
		Level:    domain.ImportanceLevel(out.Level),
		Category: domain.Category(out.Category),
		Score:    out.Score,
		Reasons:  out.Reasons,
		Summary:  out.Summary,
	}, nil
}

package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"

	openaisdk "github.com/sashabaranov/go-openai"
)

const (
	defaultModel = "gpt-4o-mini"
	maxTokens    = 512
)

// Client classifies emails using the OpenAI Chat Completions API with JSON mode.
type Client struct {
	client *openaisdk.Client
	model  string
}

// New creates a Client. model may be empty to use the default.
func New(apiKey, model string) *Client {
	if model == "" {
		model = defaultModel
	}
	return &Client{
		client: openaisdk.NewClient(apiKey),
		model:  model,
	}
}

// NewWithBaseURL creates a Client pointing at a custom base URL (used in tests).
func NewWithBaseURL(apiKey, model, baseURL string) *Client {
	if model == "" {
		model = defaultModel
	}
	cfg := openaisdk.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	return &Client{
		client: openaisdk.NewClientWithConfig(cfg),
		model:  model,
	}
}

// Name returns the provider identifier used in classification source tags.
func (c *Client) Name() string { return "openai" }

// Classify sends the email to an OpenAI model and returns a structured ClassifyResult.
func (c *Client) Classify(ctx context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error) {
	resp, err := c.client.CreateChatCompletion(ctx, openaisdk.ChatCompletionRequest{
		Model: c.model,
		Messages: []openaisdk.ChatCompletionMessage{
			{Role: openaisdk.ChatMessageRoleSystem, Content: llm.SystemPrompt(req.IgnoreClauses, req.SummaryLanguage)},
			{Role: openaisdk.ChatMessageRoleUser, Content: llm.FormatUserMessage(req)},
		},
		ResponseFormat: &openaisdk.ChatCompletionResponseFormat{
			Type: openaisdk.ChatCompletionResponseFormatTypeJSONObject,
		},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("openai classify: %w", err)
	}
	if len(resp.Choices) == 0 {
		return llm.ClassifyResult{}, fmt.Errorf("openai classify: empty response")
	}

	return parseJSON(resp.Choices[0].Message.Content)
}

type jsonOutput struct {
	Level    string   `json:"level"`
	Category string   `json:"category"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
	Summary  string   `json:"summary"`
}

func parseJSON(content string) (llm.ClassifyResult, error) {
	var out jsonOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("parse openai json: %w", err)
	}
	return llm.ClassifyResult{
		Level:    domain.ImportanceLevel(out.Level),
		Category: domain.Category(out.Category),
		Score:    out.Score,
		Reasons:  out.Reasons,
		Summary:  out.Summary,
	}, nil
}

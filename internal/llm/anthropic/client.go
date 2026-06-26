package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

const (
	defaultModel = "claude-sonnet-4-6"
	maxTokens    = int64(512)
)

// classifyToolName is the tool the model is forced to call.
const classifyToolName = "classify_email"

var classifyToolSchema = anthropicsdk.ToolInputSchemaParam{
	Properties: map[string]any{
		"level": map[string]any{
			"type": "string",
			"enum": []string{"critical", "important", "maybe", "ignore"},
		},
		"category": map[string]any{"type": "string"},
		"score":    map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
		"reasons":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"summary":  map[string]any{"type": "string"},
	},
	Required: []string{"level", "category", "score", "reasons", "summary"},
}

// Client classifies emails using the Anthropic Messages API with tool use.
type Client struct {
	client anthropicsdk.Client
	model  string
}

// New creates a Client. model may be empty to use the default.
func New(apiKey, model string) *Client {
	if model == "" {
		model = defaultModel
	}
	return &Client{
		client: anthropicsdk.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

// NewWithBaseURL creates a Client pointing at a custom base URL (used in tests).
func NewWithBaseURL(apiKey, model, baseURL string) *Client {
	if model == "" {
		model = defaultModel
	}
	return &Client{
		client: anthropicsdk.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
		),
		model: model,
	}
}

// Name returns the provider identifier used in classification source tags.
func (c *Client) Name() string { return "anthropic" }

// Classify sends the email to Claude and returns a structured ClassifyResult.
func (c *Client) Classify(ctx context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error) {
	tool := anthropicsdk.ToolUnionParamOfTool(classifyToolSchema, classifyToolName)
	tool.OfTool.Description = param.NewOpt("Classify an email by importance level, category, score, reasons, and summary.")

	msg, err := c.client.Messages.New(ctx, anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(c.model),
		MaxTokens: maxTokens,
		System: []anthropicsdk.TextBlockParam{
			{Text: llm.SystemPrompt(req.IgnoreClauses)},
		},
		Messages: []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(llm.FormatUserMessage(req))),
		},
		Tools:      []anthropicsdk.ToolUnionParam{tool},
		ToolChoice: anthropicsdk.ToolChoiceParamOfTool(classifyToolName),
	})
	if err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("anthropic classify: %w", err)
	}

	// Find the tool_use block
	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == classifyToolName {
			return parseToolInput(block.Input)
		}
	}
	return llm.ClassifyResult{}, fmt.Errorf("anthropic classify: no tool_use block in response")
}

type toolOutput struct {
	Level    string   `json:"level"`
	Category string   `json:"category"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
	Summary  string   `json:"summary"`
}

func parseToolInput(raw json.RawMessage) (llm.ClassifyResult, error) {
	var out toolOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return llm.ClassifyResult{}, fmt.Errorf("parse tool output: %w", err)
	}
	return llm.ClassifyResult{
		Level:    domain.ImportanceLevel(out.Level),
		Category: domain.Category(out.Category),
		Score:    out.Score,
		Reasons:  out.Reasons,
		Summary:  out.Summary,
	}, nil
}

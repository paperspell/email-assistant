package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/paperspell/email-assistant/internal/domain"
)

// ClassifyRequest carries the email fields sent to an LLM provider.
type ClassifyRequest struct {
	FromEmail          string
	FromName           string
	Subject            string
	Body               string // empty when content.mode = headers_only
	Language           string
	IsReply            bool
	HasListUnsubscribe bool
}

// ClassifyResult holds the structured output from an LLM provider.
type ClassifyResult struct {
	Level    domain.ImportanceLevel
	Category domain.Category
	Score    int
	Reasons  []string
	Summary  string
}

// Provider classifies emails using an LLM.
type Provider interface {
	Classify(ctx context.Context, req ClassifyRequest) (ClassifyResult, error)
	Name() string // "anthropic" | "openai"
}

const systemPrompt = `You are an email importance classifier. Given email metadata and optionally the body,
you must return a JSON object with these fields:

  level    : "critical" | "important" | "maybe" | "ignore"
  category : "work" | "finance" | "legal" | "government" | "school" | "family" |
             "security" | "travel" | "shopping" | "recruiting" | "marketing" |
             "social" | "other"
  score    : integer 0-100 (your confidence-weighted importance)
  reasons  : array of short strings explaining the key signals
  summary  : one or two plain-English sentences describing what this email is about

Scoring guide:
  90-100 critical  - immediate action required
  70-89  important - should be read today
  30-69  maybe     - worth a glance but not urgent
  0-29   ignore    - newsletter, promotion, or irrelevant

Be conservative: err toward lower scores for unknown senders and marketing content.
Reply with JSON only, no prose.`

// SystemPrompt returns the shared system prompt for all providers.
func SystemPrompt() string { return systemPrompt }

// FormatUserMessage formats a ClassifyRequest as the user turn text.
func FormatUserMessage(req ClassifyRequest) string {
	var b strings.Builder
	from := req.FromEmail
	if req.FromName != "" {
		from = fmt.Sprintf("%s <%s>", req.FromName, req.FromEmail)
	}
	fmt.Fprintf(&b, "From: %s\n", from)
	fmt.Fprintf(&b, "Subject: %s\n", req.Subject)
	fmt.Fprintf(&b, "Language: %s\n", req.Language)
	fmt.Fprintf(&b, "Is reply: %s\n", yesNo(req.IsReply))
	fmt.Fprintf(&b, "Has unsubscribe header: %s\n", yesNo(req.HasListUnsubscribe))
	if req.Body != "" {
		fmt.Fprintf(&b, "\nBody:\n%s\n", req.Body)
	}
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

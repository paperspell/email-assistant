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
	// SummaryLanguage is the language the summary must be written in, e.g.
	// "Russian". Empty leaves the model's default (English).
	SummaryLanguage string
	// IgnoreClauses are per-account natural-language ignore instructions appended
	// to the system prompt. Empty for accounts with no active clauses.
	IgnoreClauses []string
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
  summary  : one or two plain sentences describing what this email is about

Scoring guide:
  90-100 critical  - immediate action required
  70-89  important - should be read today
  30-69  maybe     - worth a glance but not urgent
  0-29   ignore    - newsletter, promotion, or irrelevant

Be conservative: err toward lower scores for unknown senders and marketing content.
Reply with JSON only, no prose.`

// sanitizeLanguage trims a configured language name and collapses anything that
// could restructure the prompt (line breaks, quotes) into spaces. Language names
// are short, so the value is also bounded.
func sanitizeLanguage(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", " ", "\r", " ", "\"", " ", "`", " ").Replace(s)
	s = strings.TrimSpace(s)
	const maxLen = 40
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// SystemPrompt returns the shared system prompt plus any active per-account
// ignore clauses. Clauses are rendered as a bounded, clearly-delimited list so
// the model treats matching mail as not important.
func SystemPrompt(ignoreClauses []string, summaryLanguage string) string {
	if len(ignoreClauses) == 0 && summaryLanguage == "" {
		return systemPrompt
	}
	var b strings.Builder
	b.WriteString(systemPrompt)
	if lang := sanitizeLanguage(summaryLanguage); lang != "" {
		// Only the summary is translated: level, category and reasons stay in the
		// fixed vocabulary the caller parses. The value is quoted and stripped of
		// line breaks so a stray setting cannot restructure the prompt.
		fmt.Fprintf(&b, "\n\nWrite the \"summary\" field in %q, "+
			"whatever language the email itself is in. "+
			"Leave every other field exactly as specified above.", lang)
	}
	if len(ignoreClauses) == 0 {
		return b.String()
	}
	b.WriteString("\n\nAdditional user-defined ignore rules " +
		"(treat matching mail as not important, i.e. level \"ignore\"):\n")
	for _, c := range ignoreClauses {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", c)
	}
	return b.String()
}

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

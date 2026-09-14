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

	// Owner identifies whose mailbox this is, so the model can tell mail
	// addressed to the owner from mail that merely reached them, and recognise
	// a mention of the owner inside a notification. Zero value: not told.
	Owner Owner
	// To and Cc are the message's recipients, lowercased.
	To []string
	Cc []string
	// Focus is the owner's own description of what they want to hear about
	// from this mailbox. When set, mail outside it is classified as ignore
	// whatever its content would otherwise merit.
	Focus string
}

// Owner is the mailbox owner as the classifier should know them.
type Owner struct {
	Email   string
	Name    string
	Aliases []string // names and handles the owner is addressed by
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

// sanitizeFocus bounds a focus description the way sanitizeLanguage bounds a
// language name, at a length that fits a few sentences.
func sanitizeFocus(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", " ", "\r", " ", "`", "'").Replace(s)
	s = strings.TrimSpace(s)
	const maxLen = 600
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

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

// SystemPrompt returns the shared system prompt plus the per-account additions:
// the owner's focus, any active ignore clauses, and the summary language. Each
// is rendered as a bounded, clearly-delimited block so a stray setting cannot
// restructure the prompt.
func SystemPrompt(req ClassifyRequest) string {
	summaryLanguage, ignoreClauses := req.SummaryLanguage, req.IgnoreClauses
	if len(ignoreClauses) == 0 && summaryLanguage == "" && req.Focus == "" {
		return systemPrompt
	}
	var b strings.Builder
	b.WriteString(systemPrompt)
	if focus := sanitizeFocus(req.Focus); focus != "" {
		// The focus overrides the generic scoring guide: it is the owner
		// narrowing what counts, so mail outside it is not "less important" but
		// out of scope. Stated before the ignore clauses, which then carve
		// exceptions out of whatever the focus lets through.
		b.WriteString("\n\nThe owner of this mailbox has narrowed what they want to hear about. " +
			"Only mail matching this description can be \"maybe\", \"important\" or \"critical\":\n")
		fmt.Fprintf(&b, "  %s\n", focus)
		b.WriteString("Everything else is level \"ignore\" with a score below 30, " +
			"however important it would look on its own. " +
			"The user turn names the owner and lists the recipients: " +
			"\"addressed to me\" means the owner's address is in To, not merely Cc; " +
			"\"mentions me\" means the owner's name or one of their handles appears in the " +
			"subject or body, including notifications from ticket, document and code-review " +
			"tools that say the owner was mentioned, assigned or asked for a review.")
	}
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
	if len(req.To) > 0 {
		fmt.Fprintf(&b, "To: %s\n", strings.Join(req.To, ", "))
	}
	if len(req.Cc) > 0 {
		fmt.Fprintf(&b, "Cc: %s\n", strings.Join(req.Cc, ", "))
	}
	if o := req.Owner; o.Email != "" {
		// Stated on the message itself, next to the recipients it is compared
		// against, rather than in the system prompt.
		owner := o.Email
		if o.Name != "" && !strings.EqualFold(o.Name, o.Email) {
			owner = fmt.Sprintf("%s <%s>", o.Name, o.Email)
		}
		fmt.Fprintf(&b, "Mailbox owner: %s\n", owner)
		if len(o.Aliases) > 0 {
			fmt.Fprintf(&b, "Owner is also addressed as: %s\n", strings.Join(o.Aliases, ", "))
		}
	}
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

package jev

import (
	"context"
	"fmt"
	"strings"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/llm"
)

const (
	// maxBodyRunes bounds the body put into the state. The state plus the
	// longest question must fit about 32k tokens, and TypeSafe notes that
	// accuracy falls as unrelated text grows; what decides an email's
	// importance is almost always on its first pages.
	maxBodyRunes = 16000
	// maxClauseRunes bounds one owner-written instruction the same way
	// llm.SystemPrompt bounds the focus.
	maxClauseRunes = 600

	// matchThreshold is the probability at which a noul counts as yes: the
	// focus is taken as matched, an ignore rule as applying. More likely than
	// not is exactly the call a text model makes when it answers yes or no.
	matchThreshold = 0.5

	qLevel    = "level"
	qCategory = "category"
	qInFocus  = "in_focus"
	// qIgnore prefixes one question per ignore clause: ignore_0, ignore_1…
	qIgnore = "ignore_"
)

// Name returns the provider identifier used in classification source tags.
func (c *Client) Name() string { return "jev" }

// Classify asks Jev about the email and flattens the answer into a result.
// The result has no summary: Jev decides, it does not write.
func (c *Client) Classify(ctx context.Context, req llm.ClassifyRequest) (llm.ClassifyResult, error) {
	v, err := c.Assess(ctx, req)
	if err != nil {
		return llm.ClassifyResult{}, err
	}
	return v.Result(), nil
}

// Verdict is Jev's answer about one email with its probabilities kept, before
// Result reduces it to what the scheduler stores.
type Verdict struct {
	// Level is the most probable level on the email's merits, before the
	// focus and the ignore clauses are applied.
	Level domain.ImportanceLevel
	// Levels is the probability of each level.
	Levels map[domain.ImportanceLevel]float64
	// Confidence is how concentrated Levels is, from 0 to 1.
	Confidence float64
	Category   domain.Category
	// InFocus is the probability that the email is what the owner wants from
	// this mailbox; 1 when the account has no focus.
	InFocus float64
	// Clauses holds one entry per ignore clause, in the order they were asked.
	Clauses []ClauseMatch
	// InputTokens is what the request was billed for.
	InputTokens int
}

// ClauseMatch is the probability that one ignore clause applies.
type ClauseMatch struct {
	Clause string
	P      float64
}

// Assess asks Jev every question about the email in one request.
func (c *Client) Assess(ctx context.Context, req llm.ClassifyRequest) (Verdict, error) {
	questions := map[string]Question{
		qLevel:    levelQuestion,
		qCategory: categoryQuestion,
	}
	focus := bounded(req.Focus, maxClauseRunes)
	if focus != "" {
		questions[qInFocus] = focusQuestion(focus)
	}
	var clauses []string
	for _, clause := range req.IgnoreClauses {
		if clause = bounded(clause, maxClauseRunes); clause != "" {
			questions[fmt.Sprintf("%s%d", qIgnore, len(clauses))] = clauseQuestion(clause)
			clauses = append(clauses, clause)
		}
	}

	resp, err := c.Evaluate(ctx, stateOf(req), questions)
	if err != nil {
		return Verdict{}, fmt.Errorf("jev classify: %w", err)
	}

	level := resp.Answers[qLevel]
	v := Verdict{
		Level:       domain.ImportanceLevel(level.Choice),
		Levels:      make(map[domain.ImportanceLevel]float64, len(level.Probabilities)),
		Confidence:  level.Confidence,
		Category:    domain.Category(resp.Answers[qCategory].Choice),
		InFocus:     1,
		InputTokens: resp.Usage.InputTokens,
	}
	for k, p := range level.Probabilities {
		v.Levels[domain.ImportanceLevel(k)] = p
	}
	if focus != "" {
		v.InFocus = *resp.Answers[qInFocus].Noul
	}
	for i, clause := range clauses {
		v.Clauses = append(v.Clauses, ClauseMatch{
			Clause: clause, P: *resp.Answers[fmt.Sprintf("%s%d", qIgnore, i)].Noul,
		})
	}
	return v, nil
}

// band is the score range of one level in the scoring guide the text models
// are given, with the point that stands for the level as a whole.
type band struct{ lo, mid, hi int }

var bands = map[domain.ImportanceLevel]band{
	domain.LevelCritical:  {90, 95, 100},
	domain.LevelImportant: {70, 80, 89},
	domain.LevelMaybe:     {30, 50, 69},
	domain.LevelIgnore:    {0, 15, 29},
}

// Result reduces the verdict to what the scheduler stores. The focus and the
// ignore clauses are applied here, in code, rather than folded into the level
// question: each is asked on its own and its probability is kept in the
// reasons, so a surprising verdict can be traced to the question that caused
// it. The score is the level probabilities weighted by the middle of each
// level's band, then kept inside the band of the level decided, so level and
// score never disagree.
func (v Verdict) Result() llm.ClassifyResult {
	level := v.Level
	reasons := []string{fmt.Sprintf("jev: %s (p %.2f, confidence %.2f)", v.Level, v.Levels[v.Level], v.Confidence)}
	if v.InFocus < matchThreshold {
		level = domain.LevelIgnore
		reasons = append(reasons, fmt.Sprintf("outside the owner's focus (p %.2f)", v.InFocus))
	}
	for _, m := range v.Clauses {
		if m.P >= matchThreshold {
			level = domain.LevelIgnore
			reasons = append(reasons, fmt.Sprintf("matches ignore rule %q (p %.2f)", m.Clause, m.P))
		}
	}

	var expected float64
	for l, p := range v.Levels {
		expected += p * float64(bands[l].mid)
	}
	b := bands[level]
	score := min(max(int(expected+0.5), b.lo), b.hi)

	return llm.ClassifyResult{
		Level:    level,
		Category: v.Category,
		Score:    score,
		Reasons:  reasons,
	}
}

// emailState is the email as Jev sees it. The fields mirror the user turn a
// text model gets (llm.FormatUserMessage), so the two read the same facts;
// flags are "yes"/"no" because the model reads words better than values.
type emailState struct {
	From                 string   `json:"from"`
	To                   []string `json:"to,omitempty"`
	Cc                   []string `json:"cc,omitempty"`
	MailboxOwner         string   `json:"mailbox_owner,omitempty"`
	OwnerAlsoAddressedAs []string `json:"owner_also_addressed_as,omitempty"`
	Subject              string   `json:"subject"`
	EstablishedFacts     []string `json:"established_facts,omitempty"`
	IsReply              string   `json:"is_reply"`
	HasUnsubscribeHeader string   `json:"has_unsubscribe_header"`
	Body                 string   `json:"body,omitempty"`
}

func stateOf(req llm.ClassifyRequest) emailState {
	s := emailState{
		From:                 req.FromEmail,
		To:                   req.To,
		Cc:                   req.Cc,
		Subject:              req.Subject,
		EstablishedFacts:     req.FocusFacts,
		IsReply:              yesNo(req.IsReply),
		HasUnsubscribeHeader: yesNo(req.HasListUnsubscribe),
		Body:                 truncate(req.Body, maxBodyRunes),
	}
	if req.FromName != "" {
		s.From = fmt.Sprintf("%s <%s>", req.FromName, req.FromEmail)
	}
	if o := req.Owner; o.Email != "" {
		s.MailboxOwner = o.Email
		if o.Name != "" && !strings.EqualFold(o.Name, o.Email) {
			s.MailboxOwner = fmt.Sprintf("%s <%s>", o.Name, o.Email)
		}
		s.OwnerAlsoAddressedAs = o.Aliases
	}
	return s
}

// levelQuestion restates the scoring guide of llm.SystemPrompt as the four
// options, most important first.
var levelQuestion = Choice(
	"How important is this email to the mailbox owner, and how soon must they read it? "+
		"Be conservative: unknown senders and marketing count as less important.",
	Option{string(domain.LevelCritical), "Immediate action required: a security alert on the owner's " +
		"accounts, a failed payment, a legal or official deadline, an urgent request from someone they know."},
	Option{string(domain.LevelImportant), "Should be read today: a person writing to the owner or asking " +
		"them something, a bill, a booking or an appointment, work that involves them."},
	Option{string(domain.LevelMaybe), "Worth a glance but not urgent: a relevant update, receipt or " +
		"notification that needs no action."},
	Option{string(domain.LevelIgnore), "A newsletter, promotion or marketing, a routine automated " +
		"notification, or anything irrelevant to the owner."},
)

var categoryQuestion = Choice(
	"What is this email about?",
	Option{string(domain.CategoryWork), "The owner's job: colleagues, clients, projects, code review, tickets."},
	Option{string(domain.CategoryFinance), "Banks, payments, invoices, bills, taxes, investments."},
	Option{string(domain.CategoryLegal), "Contracts, lawyers, courts, legal notices."},
	Option{string(domain.CategoryGovernment), "Government agencies, official documents, permits, public services."},
	Option{string(domain.CategorySchool), "Schools, universities, courses, children's education."},
	Option{string(domain.CategoryFamily), "Family members and personal matters."},
	Option{string(domain.CategorySecurity), "Account security: sign-ins, password resets, verification codes, breaches."},
	Option{string(domain.CategoryTravel), "Flights, trains, hotels, bookings, itineraries."},
	Option{string(domain.CategoryShopping), "Orders, deliveries, returns, shop receipts."},
	Option{string(domain.CategoryRecruiting), "Job offers, recruiters, applications, interviews."},
	Option{string(domain.CategoryMarketing), "Promotions, newsletters, advertising."},
	Option{string(domain.CategorySocial), "Social networks, communities, forums, event invitations."},
	Option{string(domain.CategoryOther), "Anything that fits none of the above."},
)

// focusQuestion carries the same definitions llm.SystemPrompt gives a text
// model, so "addressed to me" means the same thing to both.
func focusQuestion(focus string) Question {
	return Noul(
		fmt.Sprintf("Is this email what the mailbox owner wants to hear about from this mailbox: %q? ", focus)+
			"\"Addressed to me\" means the owner's address is in `to`, not merely in `cc`. "+
			"\"Mentions me\" means the owner's name or one of `owner_also_addressed_as` appears in "+
			"`subject` or `body`, including notifications from ticket, document and code-review tools "+
			"that say the owner was mentioned, assigned or asked for a review.",
		"The email matches the owner's description.",
		"The email is outside it, however important it would look on its own.",
	)
}

func clauseQuestion(clause string) Question {
	return Noul(
		fmt.Sprintf("The mailbox owner does not want to hear about mail described as %q. "+
			"Does this email match that description?", clause),
		"", "",
	)
}

// bounded trims s and cuts it to n runes. Line breaks are collapsed: a clause
// or focus is one sentence, and it is quoted into a question.
func bounded(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, n)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

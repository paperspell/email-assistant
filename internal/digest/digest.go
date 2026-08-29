// Package digest builds and renders the daily per-account digest of
// unimportant mail, and schedules its delivery.
package digest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
)

// Item is one numbered, LLM-judged-unimportant email shown in the digest.
type Item struct {
	SeqNo   int
	Email   domain.Email
	Summary string
	// Score is the importance score behind the decision, 0-100.
	Score int
}

// Counter summarises mail that was filtered without an LLM summary, grouped by
// provenance, so the digest can collapse it into one line (and the CLI expand it).
type Counter struct {
	ByRule   map[string]int // decided_by "rule:<id>" -> count
	Baseline int            // baseline score gate
	Other    int            // manual ignores / unknown provenance
	Total    int
}

// Digest is the built, ready-to-render daily digest for one account.
type Digest struct {
	AccountID string
	Date      string
	Items     []Item
	Counter   Counter
}

// Empty reports whether there is nothing to send.
func (d Digest) Empty() bool {
	return len(d.Items) == 0 && d.Counter.Total == 0
}

// Build gathers the account's ignored mail for the given date (in loc) and splits
// it into LLM-judged items (listed, with summaries) and a provenance counter.
func Build(
	ctx context.Context,
	emailRepo *repo.EmailRepo,
	classRepo *repo.ClassificationRepo,
	accountID, date string,
	loc *time.Location,
) (Digest, error) {
	from, to, err := dayBounds(date, loc)
	if err != nil {
		return Digest{}, err
	}

	emails, err := emailRepo.ListIgnoredByAccountInRange(ctx, accountID, from, to)
	if err != nil {
		return Digest{}, err
	}

	ids := make([]string, 0, len(emails))
	for _, e := range emails {
		ids = append(ids, e.ID)
	}
	byEmail, err := classRepo.GetAllByEmailIDs(ctx, ids)
	if err != nil {
		return Digest{}, err
	}

	d := Digest{AccountID: accountID, Date: date, Counter: Counter{ByRule: map[string]int{}}}
	seq := 0
	for _, e := range emails {
		// Every ignored email is listed, whoever made the call: a digest that hides
		// rule- and baseline-filtered mail cannot be audited by the person whose
		// rules produced it. The counter below still breaks the decisions down.
		seq++
		summary, score := verdict(byEmail[e.ID])
		d.Items = append(d.Items, Item{SeqNo: seq, Email: e, Summary: summary, Score: score})

		switch {
		case e.DecidedBy == "llm:low":
			// counted as listed only
		case strings.HasPrefix(e.DecidedBy, "rule:"):
			d.Counter.ByRule[e.DecidedBy]++
			d.Counter.Total++
		case e.DecidedBy == "baseline":
			d.Counter.Baseline++
			d.Counter.Total++
		default:
			d.Counter.Other++
			d.Counter.Total++
		}
	}
	return d, nil
}

// verdict picks the summary and score to show for one email, preferring the LLM
// classification over the rule-based one. Zero values when there is none.
func verdict(all []domain.Classification) (string, int) {
	var summary string
	var score int
	var haveLLM bool
	for _, c := range all {
		if strings.HasPrefix(c.Source, domain.SourceLLM) {
			if !haveLLM {
				summary, score, haveLLM = c.Summary, c.Score, true
			}
			continue
		}
		if !haveLLM {
			summary, score = c.Summary, c.Score
		}
	}
	return summary, score
}

// dayBounds returns [from, to) for the calendar day `date` (YYYY-MM-DD) in loc.
func dayBounds(date string, loc *time.Location) (from, to time.Time, err error) {
	if loc == nil {
		loc = time.Local
	}
	from, err = time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse digest date %q: %w", date, err)
	}
	return from, from.AddDate(0, 0, 1), nil
}

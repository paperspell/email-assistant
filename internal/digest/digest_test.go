package digest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
)

const (
	testAcct = "a@x.com"
	testDate = "2026-06-26"
)

func setup(t *testing.T) (*repo.EmailRepo, *repo.ClassificationRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))
	return repo.NewEmailRepo(sqlDB), repo.NewClassificationRepo(sqlDB)
}

// addIgnored inserts an ignored email on testDate with the given provenance, and
// optionally an LLM classification carrying a summary.
var uidSeq uint32

func addIgnored(t *testing.T, er *repo.EmailRepo, cr *repo.ClassificationRepo, id, decidedBy, summary string) {
	t.Helper()
	ctx := context.Background()
	uidSeq++
	e := domain.Email{
		ID: id, AccountID: testAcct, MessageUID: uidSeq, Subject: "Subj " + id,
		FromEmail: id + "@s.com", FromName: "Sender " + id,
		Date:       time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC),
		Status:     domain.StatusIgnored,
		ReceivedAt: time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC),
	}
	require.NoError(t, er.Upsert(ctx, &e))
	require.NoError(t, er.UpdateStatusDecidedBy(ctx, id, domain.StatusIgnored, decidedBy))
	if summary != "" {
		require.NoError(t, cr.Save(ctx, domain.Classification{
			ID: "c-" + id, EmailID: id, Level: domain.LevelIgnore, Score: 10,
			Summary: summary, Source: domain.SourceLLM + ":mock", ClassifiedAt: time.Now(),
		}))
	}
}

func TestBuild_ListsEveryIgnoredEmail(t *testing.T) {
	er, cr := setup(t)
	addIgnored(t, er, cr, "1", "llm:low", "A marketing email.")
	addIgnored(t, er, cr, "2", "llm:low", "A newsletter.")
	addIgnored(t, er, cr, "3", "rule:r1", "")
	addIgnored(t, er, cr, "4", "rule:r1", "")
	addIgnored(t, er, cr, "5", "baseline", "")
	addIgnored(t, er, cr, "6", "", "") // manual ignore

	d, err := Build(context.Background(), er, cr, testAcct, testDate, time.UTC)
	require.NoError(t, err)

	// Every ignored email is listed now, whoever decided it; the counter keeps
	// the provenance breakdown.
	require.Len(t, d.Items, 6, "all ignored emails are listed")
	assert.Equal(t, 1, d.Items[0].SeqNo)
	assert.Equal(t, "A marketing email.", d.Items[0].Summary)
	assert.Equal(t, 6, d.Items[5].SeqNo, "numbering is continuous across decision sources")

	assert.Equal(t, 4, d.Counter.Total)
	assert.Equal(t, 2, d.Counter.ByRule["rule:r1"])
	assert.Equal(t, 1, d.Counter.Baseline)
	assert.Equal(t, 1, d.Counter.Other)
	assert.False(t, d.Empty())
}

func TestBuild_EmptyDay(t *testing.T) {
	er, cr := setup(t)
	d, err := Build(context.Background(), er, cr, testAcct, testDate, time.UTC)
	require.NoError(t, err)
	assert.True(t, d.Empty())
}

func TestBuild_ExcludesOtherDays(t *testing.T) {
	er, cr := setup(t)
	addIgnored(t, er, cr, "1", "llm:low", "today")
	d, err := Build(context.Background(), er, cr, testAcct, "2026-06-25", time.UTC)
	require.NoError(t, err)
	assert.True(t, d.Empty(), "an email from the 26th must not appear in the 25th's digest")
}

func TestNextFire_TodayThenTomorrow(t *testing.T) {
	s := New(Config{Time: "20:00", Location: time.UTC})

	before := time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC), s.nextFire(before))

	after := time.Date(2026, 6, 26, 21, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 6, 27, 20, 0, 0, 0, time.UTC), s.nextFire(after),
		"after the time has passed, fire tomorrow")
}

func TestNextFire_HonoursTimezone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	s := New(Config{Time: "20:00", Location: ny})

	// 18:00 New York is before the 20:00 fire time in NY.
	now := time.Date(2026, 6, 26, 18, 0, 0, 0, ny)
	fire := s.nextFire(now)
	assert.Equal(t, 20, fire.Hour())
	assert.Equal(t, ny, fire.Location())
}

func TestFormatTelegram_ListsSenderTimeAndScore(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	require.NoError(t, err)

	d := Digest{
		AccountID: testAcct, Date: testDate, Loc: warsaw,
		Items: []Item{
			{SeqNo: 1, Email: domain.Email{
				FromName: "LinkedIn", FromEmail: "jobs@linkedin.com", Subject: "5 jobs",
				ReceivedAt: time.Date(2026, 8, 30, 7, 14, 0, 0, time.UTC),
			}, Summary: "Job alert.", Score: 22},
			{SeqNo: 2, Email: domain.Email{
				FromEmail: "sale@shop.com", Subject: "Sale",
				ReceivedAt: time.Date(2026, 8, 30, 18, 2, 0, 0, time.UTC),
			}, Score: 5},
		},
		// Same emails, broken down by provenance. It must not reach the message.
		Counter: Counter{Total: 2},
	}

	out := FormatTelegram(i18n.English(), d, testAcct)

	// Subject on its own line, then sender, receipt time and the labelled score.
	assert.Contains(t, out, "1. 5 jobs\n   LinkedIn <jobs@linkedin.com> · 09:14 · importance 22\n\n")
	// No display name: the address stands alone rather than rendering "<>" empty.
	assert.Contains(t, out, "2. Sale\n   sale@shop.com · 20:02 · importance 5")
	// The score is labelled, so a bare number cannot be mistaken for a count.
	assert.NotContains(t, out, "[22]")
	assert.Contains(t, out, "/important")
}

// The counter breaks down the very emails already listed, so printing it as
// "+N filtered" told the reader mail had been withheld from the list.
func TestFormatTelegram_OmitsTheProvenanceCounter(t *testing.T) {
	d := Digest{
		AccountID: testAcct, Date: testDate, Loc: time.UTC,
		Items: []Item{{SeqNo: 1, Email: domain.Email{
			FromEmail: "a@b.com", Subject: "s", ReceivedAt: time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC),
		}, Score: 3}},
		Counter: Counter{Total: 1, ByRule: map[string]int{"rule:x": 1}},
	}

	out := FormatTelegram(i18n.English(), d, testAcct)

	assert.NotContains(t, out, "filtered")
	assert.NotContains(t, out, "+1")
}

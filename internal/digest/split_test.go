package digest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

// bigDigest is shaped like the mailbox that failed in production: many items,
// long subjects, Cyrillic and emoji (which Telegram counts as two units each).
func bigDigest(n int) Digest {
	subjects := []string{
		"Trending stability issues for 2026-09-06 - Android space.paperspell.android.prod",
		"Re: [paperspell/document-service] feat(2400): normalize bot images before they reach GCS (PR #272)",
		"🌋😭С этими детьми мы проходим все «кризисы» — рассылка Института Ньюфелда",
		"Aliaksei, niedzielna okazja 🔥 — tylko dziś -40% na wszystko w sklepie",
		"Your receipt from Suno #2585-9336-7783",
	}
	d := Digest{AccountID: "anovikau@gmail.com", Date: "2026-09-06", Loc: time.UTC}
	for i := 1; i <= n; i++ {
		d.Items = append(d.Items, Item{
			SeqNo: i,
			Email: domain.Email{
				Subject:    subjects[i%len(subjects)],
				FromName:   "Some Fairly Long Sender Name",
				FromEmail:  fmt.Sprintf("sender%d@example-domain.com", i),
				ReceivedAt: time.Date(2026, 9, 6, 8, i%60, 0, 0, time.UTC),
			},
			Score: i % 100,
		})
	}
	return d
}

func tgLen(s string) int { return len(utf16.Encode([]rune(s))) }

func TestFormatTelegram_SplitsAnOversizedDigest(t *testing.T) {
	ru, err := i18n.NewPrinter("ru")
	require.NoError(t, err)
	d := bigDigest(106) // the 20 698-character digest Telegram rejected

	parts := FormatTelegram(ru, d, d.AccountID)

	require.Greater(t, len(parts), 1, "106 items cannot fit one message")
	for i, p := range parts {
		assert.LessOrEqual(t, tgLen(p), TelegramMessageLimit, "part %d exceeds Telegram's limit", i+1)
		assert.Contains(t, p, fmt.Sprintf("часть %d из %d", i+1, len(parts)))
	}
}

func TestFormatTelegram_SplitKeepsEveryItemOnceAndInOrder(t *testing.T) {
	d := bigDigest(106)

	parts := FormatTelegram(i18n.English(), d, d.AccountID)

	seq := regexp.MustCompile(`(?m)^(\d+)\. `)
	var seen []string
	for _, p := range parts {
		for _, m := range seq.FindAllStringSubmatch(p, -1) {
			seen = append(seen, m[1])
		}
	}
	require.Len(t, seen, 106, "every item must appear exactly once across the parts")
	for i, s := range seen {
		assert.Equal(t, fmt.Sprint(i+1), s,
			"numbering must run on across parts so /important <n> keeps meaning the same email")
	}
}

func TestFormatTelegram_HintOnlyOnTheLastPart(t *testing.T) {
	d := bigDigest(106)

	parts := FormatTelegram(i18n.English(), d, d.AccountID)

	for i, p := range parts[:len(parts)-1] {
		assert.NotContains(t, p, "/important", "part %d is not the last and must not carry the hint", i+1)
	}
	assert.Contains(t, parts[len(parts)-1], "/important")
}

func TestFormatTelegram_NeverSplitsAnItemAcrossParts(t *testing.T) {
	d := bigDigest(106)

	parts := FormatTelegram(i18n.English(), d, d.AccountID)

	// Every part must end where an item ends — on the hint or on an item's
	// meta line — never in the middle of a subject or a sender line.
	metaLine := regexp.MustCompile(`· importance \d+$`)
	for i, p := range parts {
		last := strings.TrimSpace(p[strings.LastIndex(strings.TrimSpace(p), "\n")+1:])
		ok := metaLine.MatchString(last) || strings.HasPrefix(last, "Reply /important")
		assert.True(t, ok, "part %d ends mid-item: %q", i+1, last)
	}
}

func TestFormatTelegram_AnItemLongerThanALimitIsTruncatedNotDropped(t *testing.T) {
	d := Digest{AccountID: "a@b.com", Date: "2026-09-11", Loc: time.UTC, Items: []Item{{
		SeqNo: 1, Score: 3,
		Email: domain.Email{
			Subject:    strings.Repeat("very long subject ", 400), // ~6 800 units on its own
			FromEmail:  "a@b.com",
			ReceivedAt: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC),
		},
	}}}

	parts := FormatTelegram(i18n.English(), d, d.AccountID)

	require.Len(t, parts, 1)
	assert.LessOrEqual(t, tgLen(parts[0]), TelegramMessageLimit)
	assert.Contains(t, parts[0], "1. very long subject", "the item is listed, just cut")
	assert.Contains(t, parts[0], "…")
}

func TestFormatTelegram_LimitIsMeasuredInUTF16Units(t *testing.T) {
	// Telegram counts an emoji as two units. A digest that fits by rune count
	// but not by UTF-16 count would be rejected exactly like the unsplit one.
	d := Digest{AccountID: "a@b.com", Date: "2026-09-11", Loc: time.UTC}
	for i := 1; i <= 40; i++ {
		d.Items = append(d.Items, Item{SeqNo: i, Email: domain.Email{
			Subject: strings.Repeat("🔥", 40), FromEmail: "a@b.com",
			ReceivedAt: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC),
		}})
	}

	parts := FormatTelegram(i18n.English(), d, d.AccountID)

	for i, p := range parts {
		assert.LessOrEqual(t, tgLen(p), TelegramMessageLimit, "part %d over the limit in UTF-16 units", i+1)
	}
}

// fakeSender records what the scheduler asked it to send and hands back
// sequential message ids, optionally failing on one part.
type fakeSender struct {
	parts  []string
	failAt int // 1-based part number to fail on; 0 never fails
}

func (f *fakeSender) SendDigest(_ context.Context, parts []string, _ string) ([]int64, error) {
	f.parts = parts
	var ids []int64
	for i := range parts {
		if f.failAt == i+1 {
			return ids, errors.New("telegram: boom")
		}
		ids = append(ids, int64(1000+i))
	}
	return ids, nil
}

func splitScheduler(t *testing.T, sender *fakeSender, n int) (*Scheduler, *repo.DigestRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))
	er, cr, dr := repo.NewEmailRepo(sqlDB), repo.NewClassificationRepo(sqlDB), repo.NewDigestRepo(sqlDB)
	for i := 1; i <= n; i++ {
		addIgnored(t, er, cr, fmt.Sprintf("e%03d", i), "llm:low", strings.Repeat("summary ", 4))
	}
	s := New(Config{
		AccountID: testAcct, AccountEmail: testAcct, Time: "20:00", Location: time.UTC,
		EmailRepo: er, ClassRepo: cr, DigestRepo: dr, Sender: sender, Logger: log.Noop{},
		Now: func() time.Time { return time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC) },
	})
	return s, dr
}

func TestRunOnce_PersistsEveryPartAndButtonsOnTheLast(t *testing.T) {
	sender := &fakeSender{}
	s, dr := splitScheduler(t, sender, 120)

	require.NoError(t, s.runOnce(context.Background()))

	require.Greater(t, len(sender.parts), 1, "120 items must have been split")
	// The stored digest points its keyboard at the last part, and every part
	// resolves back to it — a reply to part 1 must find the same digest.
	last := int64(1000 + len(sender.parts) - 1)
	d, err := dr.GetByTGMessageID(context.Background(), 1000)
	require.NoError(t, err)
	require.NotNil(t, d, "a reply to the first part must resolve")
	assert.Equal(t, last, d.TGMessageID, "the keyboard is on the last part")
	items, err := dr.Items(context.Background(), d.ID)
	require.NoError(t, err)
	assert.Len(t, items, 120)
}

func TestRunOnce_FailedPartLeavesNothingPersistedAndNamesTheLeftovers(t *testing.T) {
	sender := &fakeSender{failAt: 2}
	s, dr := splitScheduler(t, sender, 120)

	err := s.runOnce(context.Background())

	require.Error(t, err)
	// Part 1 reached the chat without buttons; the error must say so, since
	// the daemon retries tomorrow and the orphan would otherwise be a mystery.
	assert.Contains(t, err.Error(), "1 of")
	assert.Contains(t, err.Error(), "[1000]")
	d, derr := dr.GetByAccountAndDate(context.Background(), testAcct, "2026-06-26")
	require.NoError(t, derr)
	assert.Nil(t, d, "a half-sent digest must not be recorded as sent")
}

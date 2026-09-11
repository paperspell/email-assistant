package digest

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
)

// TelegramMessageLimit is Telegram's cap on message text, in UTF-16 code units —
// the unit Telegram measures in, so an emoji costs two and Cyrillic one.
const TelegramMessageLimit = 4096

// FormatTelegram renders the digest as one or more Telegram messages, each
// within TelegramMessageLimit. Items are never split across messages, numbering
// runs on across them, and the /important hint appears once, on the last. When
// everything fits, the single message is identical to what an unsplit digest
// always looked like.
//
// The Mark read / Remove buttons are attached separately by the bot, to the
// last part. It deliberately prints no "+N filtered" footer: the counter breaks
// down the same emails by provenance, and printing it as a total read as mail
// being withheld from the list.
func FormatTelegram(p *i18n.Printer, d Digest, accountEmail string) []string {
	if len(d.Items) == 0 {
		return []string{
			p.T("digest_title", "Account", accountEmail, "Date", d.Date) + "\n\n" + p.T("digest_empty"),
		}
	}

	blocks := make([]string, 0, len(d.Items))
	for _, it := range d.Items {
		blocks = append(blocks, itemBlock(p, it, d.Loc))
	}
	footer := p.T("digest_keep_hint")

	// The part header is the longest thing that varies with the split, so its
	// size is reserved up front with the widest plausible part numbers.
	headerBudget := utf16Len(p.T("digest_title_part",
		"Account", accountEmail, "Date", d.Date, "Part", 99, "Total", 99)) + 2

	groups := packBlocks(blocks, TelegramMessageLimit-headerBudget-utf16Len(footer)-1)

	parts := make([]string, 0, len(groups))
	for i, g := range groups {
		var b strings.Builder
		if len(groups) == 1 {
			b.WriteString(p.T("digest_title", "Account", accountEmail, "Date", d.Date))
		} else {
			b.WriteString(p.T("digest_title_part",
				"Account", accountEmail, "Date", d.Date, "Part", i+1, "Total", len(groups)))
		}
		b.WriteString("\n\n")
		for _, block := range g {
			b.WriteString(block)
		}
		if i == len(groups)-1 {
			b.WriteString(footer)
		}
		parts = append(parts, strings.TrimRight(b.String(), "\n"))
	}
	return parts
}

// itemBlock renders one email: two lines — the subject, then who sent it, when
// it arrived and the score behind the decision — followed by a blank line, so
// the list stays scannable on a phone. Numbered so `/important <n>` keeps
// working across parts.
func itemBlock(p *i18n.Printer, it Item, loc *time.Location) string {
	return fmt.Sprintf("%d. %s\n   %s\n\n",
		it.SeqNo,
		it.Email.Subject,
		p.T("digest_item_meta",
			"Sender", senderLabel(it.Email),
			"Time", receivedAt(it.Email, loc),
			"Score", it.Score,
		),
	)
}

// packBlocks groups consecutive blocks so that each group's total length stays
// within budget. A block longer than the budget on its own is truncated rather
// than dropped: a cut subject is still a listed email, an omitted one is not.
func packBlocks(blocks []string, budget int) [][]string {
	var groups [][]string
	var cur []string
	curLen := 0
	for _, blk := range blocks {
		if l := utf16Len(blk); l > budget {
			blk = truncateUTF16(blk, budget)
		}
		l := utf16Len(blk)
		if curLen+l > budget && len(cur) > 0 {
			groups = append(groups, cur)
			cur, curLen = nil, 0
		}
		cur = append(cur, blk)
		curLen += l
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// utf16Len measures s the way Telegram does.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// truncateUTF16 cuts s to at most n UTF-16 code units, ending in an ellipsis
// and keeping the trailing blank line that separates items.
func truncateUTF16(s string, n int) string {
	const tail = "…\n\n"
	keep := n - utf16Len(tail)
	if keep <= 0 {
		return tail
	}
	runes := []rune(strings.TrimRight(s, "\n"))
	units := 0
	for i, r := range runes {
		units += len(utf16.Encode([]rune{r}))
		if units > keep {
			return string(runes[:i]) + tail
		}
	}
	return s
}

// senderLabel renders the sender as "Name <email>", or the address alone when no
// display name was set or it merely repeats the address.
func senderLabel(e domain.Email) string {
	if e.FromName == "" || e.FromName == e.FromEmail {
		return e.FromEmail
	}
	return e.FromName + " <" + e.FromEmail + ">"
}

// receivedAt renders when the email arrived, as a local clock time. Digest items
// are all from one day, so the date would repeat on every line.
func receivedAt(e domain.Email, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return e.ReceivedAt.In(loc).Format("15:04")
}

// FormatCounter renders the expanded provenance breakdown for `digest show`.
func FormatCounter(d Digest) string {
	if d.Counter.Total == 0 {
		return "Filtered by rules/baseline: 0"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Filtered by rules/baseline: %d\n", d.Counter.Total)

	rules := make([]string, 0, len(d.Counter.ByRule))
	for k := range d.Counter.ByRule {
		rules = append(rules, k)
	}
	sort.Strings(rules)
	for _, k := range rules {
		fmt.Fprintf(&b, "   %-24s %d\n", k, d.Counter.ByRule[k])
	}
	if d.Counter.Baseline > 0 {
		fmt.Fprintf(&b, "   %-24s %d\n", "baseline (score gate)", d.Counter.Baseline)
	}
	if d.Counter.Other > 0 {
		fmt.Fprintf(&b, "   %-24s %d\n", "other / manual", d.Counter.Other)
	}
	return strings.TrimRight(b.String(), "\n")
}

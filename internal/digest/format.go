package digest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/i18n"
)

// FormatTelegram renders the digest as the plain-text body of a Telegram message
// in the user's language. The Mark read / Remove buttons are attached separately
// by the bot.
//
// Every email here is one that was not notified separately, so the list is the
// whole of what the digest covers. It deliberately prints no "+N filtered"
// footer: the counter breaks down the same emails by provenance, and printing it
// as a total read as mail being withheld from the list.
func FormatTelegram(p *i18n.Printer, d Digest, accountEmail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", p.T("digest_title", "Account", accountEmail, "Date", d.Date))

	if len(d.Items) == 0 {
		b.WriteString(p.T("digest_empty") + "\n\n")
	}
	// Two lines per email: the subject, then who sent it, when it arrived and the
	// score behind the decision. Separated by blank lines so the list stays
	// scannable on a phone, and numbered so `/important <n>` keeps working.
	for _, it := range d.Items {
		fmt.Fprintf(&b, "%d. %s\n   %s\n\n",
			it.SeqNo,
			it.Email.Subject,
			p.T("digest_item_meta",
				"Sender", senderLabel(it.Email),
				"Time", receivedAt(it.Email, d.Loc),
				"Score", it.Score,
			),
		)
	}

	if len(d.Items) > 0 {
		b.WriteString(p.T("digest_keep_hint"))
	}
	return strings.TrimRight(b.String(), "\n")
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

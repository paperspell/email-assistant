package digest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/paperspell/email-assistant/internal/i18n"
)

// FormatTelegram renders the digest as the plain-text body of a Telegram message
// in the user's language. The Mark read / Remove buttons are attached separately
// by the bot.
func FormatTelegram(p *i18n.Printer, d Digest, accountEmail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", p.T("digest_title", "Account", accountEmail, "Date", d.Date))

	if len(d.Items) == 0 {
		b.WriteString(p.T("digest_empty") + "\n\n")
	}
	// One line per email — number, score, subject — separated by blank lines, so
	// the list stays scannable on a phone and `/important <n>` keeps working.
	for _, it := range d.Items {
		fmt.Fprintf(&b, "%d. [%d] %s\n\n", it.SeqNo, it.Score, it.Email.Subject)
	}

	if d.Counter.Total > 0 {
		fmt.Fprintf(&b, "\n%s", p.N("digest_filtered", d.Counter.Total))
	}
	if len(d.Items) > 0 {
		b.WriteString("\n" + p.T("digest_keep_hint"))
	}
	return strings.TrimRight(b.String(), "\n")
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

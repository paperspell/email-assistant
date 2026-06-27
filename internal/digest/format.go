package digest

import (
	"fmt"
	"sort"
	"strings"
)

// FormatTelegram renders the digest as the plain-text body of a Telegram message.
// The Mark read / Remove buttons are attached separately by the bot.
func FormatTelegram(d Digest, accountEmail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🗂 Daily digest — %s — %s\n\n", accountEmail, d.Date)

	if len(d.Items) == 0 {
		b.WriteString("No items needing a glance.\n\n")
	}
	for _, it := range d.Items {
		fmt.Fprintf(&b, "%d. %s — %q\n", it.SeqNo, senderLabel(it), it.Email.Subject)
		if it.Summary != "" {
			fmt.Fprintf(&b, "   %s\n", it.Summary)
		}
	}

	if d.Counter.Total > 0 {
		fmt.Fprintf(&b, "\n+%d filtered by rules/baseline", d.Counter.Total)
	}
	if len(d.Items) > 0 {
		b.WriteString("\nReply /important <n,…> to keep an item.")
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

func senderLabel(it Item) string {
	if it.Email.FromName != "" {
		return it.Email.FromName
	}
	return it.Email.FromEmail
}

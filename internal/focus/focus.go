// Package focus decides, from headers and text alone, whether a message is
// addressed to the mailbox owner. It settles the cases a tool states outright
// — a review request, a push to a merge request — so the classifier is only
// asked about the ones that need reading, and its answer can be audited
// against a stated fact rather than a summary.
package focus

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/paperspell/email-assistant/internal/email"
)

// Verdict is the outcome of an assessment.
type Verdict int

const (
	// Unknown means nothing mechanical decides it; the classifier reads the mail.
	Unknown Verdict = iota
	// Directed means the owner is addressed — mentioned, assigned, asked for a
	// review, or named as a recipient by a person rather than a tool.
	Directed
	// NotDirected means a tool notification that no one aimed at the owner. It
	// is out of a focused mailbox's scope without consulting the classifier.
	NotDirected
)

// Assessment is a verdict with the facts behind it, phrased for both the
// classifier's prompt and the owner reading "Details".
type Assessment struct {
	Verdict Verdict
	// Strong is set on a Directed verdict that rests on an explicit address —
	// an @-mention, a tool saying "mentioned you", the owner added as reviewer
	// or assignee, a GitHub review request. Only a strong verdict guarantees a
	// notification; a weak one (a person's comment on the owner's thread) is a
	// hint the classifier weighs.
	Strong bool
	// Facts are what was established, e.g. "GitHub reason: review_requested".
	// Empty when nothing notable was found.
	Facts []string
}

// Owner is who the mailbox belongs to, for matching recipients and mentions.
type Owner struct {
	Email   string
	Aliases []string
	// Bots are the handles of automation accounts — an AI code reviewer, a
	// dependency updater — whose comments are noise rather than a person
	// writing. GitHub Apps are recognised by their "[bot]" suffix without
	// being listed; GitLab has no such convention, so they must be named.
	Bots []string
}

// isSelf reports whether a handle is the owner's own — one of their aliases,
// or the local part of their address, which is what most tools use.
func (o Owner) isSelf(handle string) bool {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if h == "" {
		return false
	}
	if at := strings.Index(o.Email, "@"); at > 0 && strings.EqualFold(o.Email[:at], h) {
		return true
	}
	for _, a := range o.Aliases {
		if strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a), "@")) == h {
			return true
		}
	}
	return false
}

// isBot reports whether a handle belongs to automation.
func (o Owner) isBot(handle string) bool {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if h == "" {
		return false
	}
	if strings.HasSuffix(h, "[bot]") {
		return true
	}
	for _, b := range o.Bots {
		if strings.ToLower(strings.TrimPrefix(strings.TrimSpace(b), "@")) == h {
			return true
		}
	}
	return false
}

// githubDirected are the GitHub reasons that mean someone aimed the
// notification at the owner.
var githubDirected = map[string]bool{
	"mention": true, "team_mention": true, "review_requested": true, "assign": true,
}

// githubComments are the reasons under which a comment reaches a participant:
// on a thread they authored, or one they commented on. Whether it matters
// turns on who wrote it — a person, or a bot.
var githubComments = map[string]bool{"author": true, "comment": true}

// githubUndirected are the reasons that mean the owner is merely watching:
// nothing in them is addressed to anyone.
var githubUndirected = map[string]bool{
	"subscribed": true, "ci_activity": true, "push": true, "state_change": true,
	"manual": true, "security_alert": true,
}

// directAddress are the phrases ticket, document and calendar tools use when
// a notification is about the recipient specifically — as opposed to the
// thread they follow. Second person, because the tool is talking to them.
var directAddress = []string{
	"mentioned you", "assigned you", "assigned to you", "assigned this to you",
	"requested your review", "requested a review from you", "asked you",
	"invited you", "wants you to",
	"упомянул вас", "упомянула вас", "назначил вам", "назначила вам", "назначил на вас",
}

// Assess derives what can be known without reading the message as prose.
func Assess(msg email.Message, owner Owner) Assessment {
	var a Assessment
	// An explicit address is strong evidence; the owner's name merely
	// appearing is not. Names sit in author and reviewer footers, calendar
	// organiser lines and "Aliaksei did X" activity reports far more often than
	// in someone asking the owner something — on real work mail, a plain name
	// match was right for five messages and wrong for eight.
	strong := false
	if phrase := addressedInSecondPerson(msg); phrase != "" {
		strong = true
		a.Facts = append(a.Facts, fmt.Sprintf("the notification addresses the owner directly (%q)", phrase))
	}
	if atMentionsOwner(msg, owner) {
		strong = true
		a.Facts = append(a.Facts, "owner is @-mentioned")
	}
	if role := ownerGivenRole(msg, owner); role != "" {
		strong = true
		a.Facts = append(a.Facts, "owner "+role)
	}
	if !strong && mentionsOwner(msg, owner) {
		a.Facts = append(a.Facts, "owner's name appears in the text, without being addressed")
	}
	directed := func() Verdict {
		a.Strong = true
		return Directed
	}

	n := msg.Notification
	// A tool echoing the owner's own action back to them — their push, their
	// comment, their approval — is never someone addressing them, however
	// often their name appears in it.
	if (n.Platform == "github" || n.Platform == "gitlab") && owner.isSelf(n.Sender) {
		a.Facts = append(a.Facts, "the owner's own activity, echoed by "+n.Platform)
		a.Verdict = NotDirected
		return a
	}
	switch n.Platform {
	case "github":
		a.Facts = append(a.Facts, "GitHub notification, reason: "+n.Reason)
		if n.Sender != "" {
			a.Facts = append(a.Facts, "acting user: "+n.Sender)
		}
		switch {
		case strong:
			a.Verdict = directed()
		case n.Reason == "review_requested" && msg.InReplyTo != "":
			// GitHub keeps this reason on every later message in a thread the
			// owner was asked to review. The request itself is the first
			// message; the rest are activity on that pull request, for the
			// classifier to weigh.
			a.Facts = append(a.Facts, "follow-up in a thread the owner was asked to review")
		case githubDirected[n.Reason]:
			a.Verdict = directed()
		case githubComments[n.Reason]:
			a.Verdict = commentVerdict(&a, n.Sender, owner)
		case githubUndirected[n.Reason]:
			a.Verdict = NotDirected
		}
		return a

	case "gitlab":
		if n.Sender != "" {
			a.Facts = append(a.Facts, fmt.Sprintf("GitLab %s by @%s", n.Reason, n.Sender))
		} else {
			a.Facts = append(a.Facts, "GitLab "+n.Reason)
		}
		switch {
		case strong:
			a.Verdict = directed()
		case n.Reason == "activity":
			// A push, approval, merge or resolved thread. GitLab sends these to
			// every participant; none of them is a person addressing the owner.
			a.Verdict = NotDirected
		case n.Reason == "comment":
			a.Verdict = commentVerdict(&a, n.Sender, owner)
		}
		return a
	}

	if strong {
		a.Verdict = directed()
		return a
	}
	if n.Automated {
		a.Facts = append(a.Facts, "automated notification (Auto-Submitted)")
	}
	// Where the owner's address sits is a fact for the classifier, not a
	// verdict: a SaaS digest is addressed to the owner personally too. Only the
	// classifier, reading the mail, can tell a person writing from a service
	// sending — which the focus text asks it to do.
	switch {
	case owner.Email != "" && containsFold(msg.To, owner.Email):
		a.Facts = append(a.Facts, "owner's address is in To")
	case owner.Email != "" && containsFold(msg.Cc, owner.Email):
		a.Facts = append(a.Facts, "owner's address is only in Cc")
	}
	return a
}

// atMentionsOwner reports whether an alias appears as an @-mention — the form
// Jira, Confluence, GitLab and Slack render when a person tags someone.
func atMentionsOwner(msg email.Message, owner Owner) bool {
	text := strings.ToLower(msg.Subject + "\n" + msg.Body)
	for _, alias := range owner.Aliases {
		alias = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(alias, "@")))
		if alias != "" && hasWord(text, "@"+alias) {
			return true
		}
	}
	return false
}

// ownerGivenRole reports how a tool, in the third person, says the owner was
// given something to do — "Aliaksei Novikau were added as reviewers",
// "requested review from Aliaksei Novikau", "assigned to Aliaksei Novikau" —
// or "". A footer listing "Assignee: Aliaksei Novikau" is not this: it
// describes the thread rather than reporting an action towards the owner.
func ownerGivenRole(msg email.Message, owner Owner) string {
	text := strings.ToLower(strings.Join(strings.Fields(msg.Subject+"\n"+msg.Body), " "))
	for _, alias := range owner.Aliases {
		alias = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(alias, "@")))
		if alias == "" {
			continue
		}
		q := regexp.QuoteMeta(alias)
		switch {
		case regexp.MustCompile(q + `[^.]{0,40}\badded as (?:an? )?reviewers?\b`).MatchString(text),
			regexp.MustCompile(`\breview from ` + q).MatchString(text):
			return "was asked for a review"
		case regexp.MustCompile(q + `[^.]{0,40}\badded as (?:an? )?assignees?\b`).MatchString(text),
			regexp.MustCompile(`\bassigned (?:\S+ )?to ` + q).MatchString(text):
			return "was assigned"
		}
	}
	return ""
}

// addressedInSecondPerson returns the direct-address phrase found in the
// subject or body, or "".
func addressedInSecondPerson(msg email.Message) string {
	text := strings.ToLower(msg.Subject + "\n" + msg.Body)
	for _, p := range directAddress {
		if strings.Contains(text, p) {
			return p
		}
	}
	return ""
}

// commentVerdict settles a comment on a thread the owner takes part in. A
// person commenting on the owner's merge request is writing to them and is
// wanted; an automated reviewer doing the same is noise. An unnamed sender is
// left to the classifier.
func commentVerdict(a *Assessment, sender string, owner Owner) Verdict {
	switch {
	case sender == "":
		return Unknown
	case owner.isBot(sender):
		a.Facts = append(a.Facts, "comment by an automation account")
		return NotDirected
	default:
		a.Facts = append(a.Facts, "comment by a person on a thread the owner takes part in")
		return Directed
	}
}

// mentionsOwner reports whether any alias appears as a whole word in the
// subject or body. Whole-word so that "Al" does not match "Almost", and
// case-insensitive so that "@Aliaksei" and "aliaksei" both count. A leading @
// on either side is ignored, since tools and people are inconsistent about it.
func mentionsOwner(msg email.Message, owner Owner) bool {
	text := strings.ToLower(msg.Subject + "\n" + msg.Body)
	for _, alias := range owner.Aliases {
		alias = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(alias, "@")))
		if alias == "" {
			continue
		}
		if hasWord(text, alias) {
			return true
		}
	}
	return false
}

// hasWord reports whether word occurs in text bounded by non-word characters.
func hasWord(text, word string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], word)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(word)
		before := i == 0 || !isWordRune(lastRune(text[:i]))
		after := end == len(text) || !isWordRune(firstRune(text[end:]))
		if before && after {
			return true
		}
		start = i + 1
	}
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), want) {
			return true
		}
	}
	return false
}

package focus

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/paperspell/email-assistant/internal/email"
)

var owner = Owner{
	Email:   "aliaksei.novikau@viber.com",
	Aliases: []string{"Aliaksei Novikau", "aliaksei.novikau", "@anovikau"},
	Bots:    []string{"aicode"},
}

// Each case is a real message from the work mailbox on the day focus mode went
// live, reduced to the fields that decide it.
func TestAssess_RealNotifications(t *testing.T) {
	tests := []struct {
		name string
		msg  email.Message
		want Verdict
		fact string
	}{
		{
			name: "AI reviewer comment on my merge request — the noise that prompted this",
			msg: email.Message{
				Subject: "Re: gam-cli  | BUS-28726: Add targeting entity commands (!5)",
				To:      []string{"aliaksei.novikau@viber.com"},
				Body:    "The Commenter commented: the change looks good and matches the suggestion.",
				Notification: email.Notification{
					Platform: "gitlab", Reason: "comment", Sender: "aicode", Automated: true},
			},
			want: NotDirected, fact: "comment by an automation account",
		},
		{
			name: "a person's comment on my merge request — wanted",
			msg: email.Message{
				Subject: "Re: kiro-config | Add UID2 architecture overview. (!28)",
				To:      []string{"aliaksei.novikau@viber.com"},
				Body:    "Andrei Romanchik commented: could you check this once more?",
				Notification: email.Notification{
					Platform: "gitlab", Reason: "comment", Sender: "andrei.romanchik", Automated: true},
			},
			want: Directed, fact: "comment by a person",
		},
		{
			name: "commits pushed to a merge request I review",
			msg: email.Message{
				Subject: "Re: rssp | BUS-29301: Remove Viber schain node for O&O inventory",
				To:      []string{"aliaksei.novikau@viber.com"},
				Body:    "Eliyahu Shvalb pushed 2 commits to merge request !1503",
				Notification: email.Notification{
					Platform: "gitlab", Reason: "activity", Sender: "eliyahu.shvalb", Automated: true},
			},
			want: NotDirected, fact: "GitLab activity",
		},
		{
			name: "merge request approved by someone else",
			msg: email.Message{
				Subject: "Re: rssp | BUS-29301: Remove Viber schain node for O&O inventory",
				To:      []string{"aliaksei.novikau@viber.com"},
				Body:    "Merge request !1503 was approved by Andrei Romanchik",
				Notification: email.Notification{
					Platform: "gitlab", Reason: "activity", Sender: "andrei.romanchik", Automated: true},
			},
			want: NotDirected,
		},
		{
			name: "GitHub review requested — the owner is only in Cc, the reason decides",
			msg: email.Message{
				Subject: "[rakuten-viber-ads/rssp.gitops] Release VX Engine v0.156.1 (PR #660)",
				To:      []string{"rssp.gitops@noreply.github.com"},
				Cc:      []string{"os-aliakse.novikau@rakuten.com"},
				Notification: email.Notification{
					Platform: "github", Reason: "review_requested", Sender: "eliyahu-shvalb_rakuten", Automated: true},
			},
			want: Directed, fact: "review_requested",
		},
		{
			name: "GitHub follow-up in a thread I was asked to review keeps the reason but is not the request",
			msg: email.Message{
				Subject:   "Re: [rakuten-viber-ads/rssp.gitops] Release VX Engine v0.156.1 (PR #660)",
				InReplyTo: "<rakuten-viber-ads/rssp.gitops/pull/660@github.com>",
				Notification: email.Notification{
					Platform: "github", Reason: "review_requested", Sender: "eliyahu-shvalb_rakuten", Automated: true},
			},
			want: Unknown, fact: "follow-up in a thread",
		},
		{
			name: "my own push, echoed back by GitLab — my name is all over it, nobody addressed me",
			msg: email.Message{
				Subject: "rssp | Fixed pipeline for BUS-28097-floor-agent-metrics",
				To:      []string{"aliaksei.novikau@viber.com"},
				Body:    "Aliaksei Novikau pushed to branch BUS-28097-floor-agent-metrics; pipeline fixed.",
				Notification: email.Notification{
					Platform: "gitlab", Reason: "activity", Sender: "aliaksei.novikau", Automated: true},
			},
			want: NotDirected, fact: "own activity",
		},
		{
			name: "my own comment on a thread, echoed back — not a person writing to me",
			msg: email.Message{
				Subject: "Re: rssp | BUS-29301: Remove Viber schain node",
				Body:    "Aliaksei Novikau commented: looks good, merging.",
				Notification: email.Notification{
					Platform: "gitlab", Reason: "comment", Sender: "aliaksei.novikau", Automated: true},
			},
			want: NotDirected, fact: "own activity",
		},
		{
			name: "GitHub subscribed thread I merely watch",
			msg: email.Message{
				Subject:      "[org/repo] Bump deps (PR #12)",
				Notification: email.Notification{Platform: "github", Reason: "subscribed", Automated: true},
			},
			want: NotDirected,
		},
		{
			name: "GitHub App comment on my PR is a bot without being listed",
			msg: email.Message{
				Subject: "Re: [org/repo] Feature (PR #7)",
				Notification: email.Notification{
					Platform: "github", Reason: "author", Sender: "dependabot[bot]", Automated: true},
			},
			want: NotDirected, fact: "automation account",
		},
		{
			name: "Confluence mention — the body says \"mentioned you\", not my name",
			msg: email.Message{
				Subject: "Remember to respond - these teammates mentioned you",
				To:      []string{"aliaksei.novikau@viber.com"},
				Body:    "Pavlo Yeremenko mentioned you in a comment on 'Ad Quality UI'",
			},
			want: Directed, fact: "addresses the owner directly",
		},
		{
			name: "a person writes to me directly — a fact for the classifier, not a verdict",
			msg: email.Message{
				Subject: "Quick question", FromEmail: "colleague@viber.com",
				To: []string{"aliaksei.novikau@viber.com"},
			},
			want: Unknown, fact: "owner's address is in To",
		},
		{
			name: "SaaS digest addressed to me personally is not a person writing",
			msg: email.Message{
				Subject: "Your Daily Digest from Datadog", FromEmail: "no-reply@datadoghq.com",
				To:              []string{"aliaksei.novikau@viber.com"},
				ListUnsubscribe: "<https://datadoghq.com/unsub>",
			},
			want: Unknown, fact: "owner's address is in To",
		},
		{
			name: "a person copies me on a thread — not settled",
			msg: email.Message{
				Subject: "FYI", FromEmail: "colleague@viber.com",
				To: []string{"someone@viber.com"}, Cc: []string{"aliaksei.novikau@viber.com"},
			},
			want: Unknown, fact: "only in Cc",
		},
		{
			name: "Jira issue update — automated, nobody addressed",
			msg: email.Message{
				Subject: "[JIRA] (BUS-28132) Update helm charts versions", To: []string{"aliaksei.novikau@viber.com"},
				Body:         "Andrei changed the status to In Progress.",
				Notification: email.Notification{Automated: true},
			},
			want: Unknown, fact: "automated notification",
		},
		{
			name: "Jira assignment — the tool says so in the second person",
			msg: email.Message{
				Subject: "[JIRA] (BUS-28200) Floor agent rollout", To: []string{"aliaksei.novikau@viber.com"},
				Body:         "Andrei Romanchik assigned this to you.",
				Notification: email.Notification{Automated: true},
			},
			want: Directed, fact: "assigned this to you",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Assess(tt.msg, owner)
			assert.Equal(t, tt.want, got.Verdict, "facts: %v", got.Facts)
			if tt.fact != "" {
				assert.Contains(t, joined(got.Facts), tt.fact)
			}
		})
	}
}

func TestMentionsOwner_WholeWordAndCaseInsensitive(t *testing.T) {
	o := Owner{Aliases: []string{"Al", "@anovikau"}}

	assert.True(t, mentionsOwner(email.Message{Body: "Thanks, Al!"}, o))
	assert.True(t, mentionsOwner(email.Message{Body: "cc @ANOVIKAU please"}, o))
	assert.True(t, mentionsOwner(email.Message{Body: "anovikau: see above"}, o), "the @ is optional on either side")
	// "Al" inside "Almost" is not a mention; a two-letter alias would otherwise
	// match half the mailbox.
	assert.False(t, mentionsOwner(email.Message{Body: "Almost done"}, o))
	assert.False(t, mentionsOwner(email.Message{Body: "no one here"}, Owner{Aliases: []string{" ", ""}}))
}

func joined(s []string) string {
	out := ""
	for _, x := range s {
		out += x + " | "
	}
	return out
}

// Every case is one of the 13 work messages the header layer ruled Directed in
// the classifier comparison, reduced to the text around the owner's name. Five
// really addressed the owner; eight only named them — and a plain name match
// had turned all thirteen into guaranteed notifications.
func TestAssess_StrongOnlyForAnExplicitAddress(t *testing.T) {
	o := Owner{
		Email:   "aliaksei.novikau@viber.com",
		Aliases: []string{"Aliaksei Novikau", "aliaksei.novikau"},
		Bots:    []string{"aicode"},
	}
	gitlab := func(reason, sender string) email.Notification {
		return email.Notification{Platform: "gitlab", Reason: reason, Sender: sender, Automated: true}
	}
	tests := []struct {
		name       string
		msg        email.Message
		wantVerd   Verdict
		wantStrong bool
		fact       string
	}{
		// --- addressed: must be strong ---
		{
			name: "Jira @-mention in the comment",
			msg: email.Message{
				Subject: "[JIRA] Amit Epstein mentioned you on BUS-29572",
				Body:    "@Aliaksei Novikau You summarized my notes better than claude :laughing:",
			},
			wantVerd: Directed, wantStrong: true, fact: "@-mentioned",
		},
		{
			name: "Confluence says mentioned you, never the name",
			msg: email.Message{
				Subject: "[Confluence] Hanna Shmihelskaya mentioned you in Sep 28 - 0.156.3_ADSE",
				Body:    "Hanna Shmihelskaya mentioned you on a page.",
			},
			wantVerd: Directed, wantStrong: true, fact: "mentioned you",
		},
		{
			name: "GitLab: added as a reviewer — activity, but addressed",
			msg: email.Message{
				Subject:      "Re: rssp | BUS-29627: Remove schain from Adview, admixer and Algorix",
				Body:         "The Commenter and Aliaksei Novikau were added as reviewers. -- View it on GitLab",
				Notification: gitlab("activity", "andrei.ramanchyk"),
			},
			wantVerd: Directed, wantStrong: true, fact: "asked for a review",
		},
		// --- only named: must not be strong ---
		{
			name: "GitLab push: the name is in the author/reviewer footer",
			msg: email.Message{
				Subject: "Re: rssp | BUS-28098: Floor agent integration test suite (!1507)",
				Body: "Andrei Ramanchyk pushed new commits. Branches: BUS-28098 to release " +
					"Author: Aliaksei Novikau Assignee: Aliaksei Novikau Reviewers: Eliyahu Shvalb, Andrei Ramanchyk",
				Notification: gitlab("activity", "andrei.ramanchyk"),
			},
			// A footer must not rescue a push from the cut, as a plain name did.
			wantVerd: NotDirected, fact: "without being addressed",
		},
		{
			name: "calendar reply to the owner's own meeting",
			msg: email.Message{
				Subject:   "Accepted: Floor price agent - exploration @ Tue Sep 29, 2026 (Aliaksei Novikau)",
				FromEmail: "ido.shirat@viber.com",
				To:        []string{"aliaksei.novikau@viber.com"},
				Body:      "BEGIN:VCALENDAR ORGANIZER;CN=Aliaksei Novikau:mailto:aliaksei.novikau@viber.com",
			},
			wantVerd: Unknown, fact: "without being addressed",
		},
		{
			name: "Jira reports the owner's own action",
			msg: email.Message{
				Subject: "[JIRA] (MON-10308) VX - Create narrow bid histogram table for the floor agent",
				Body: "Aliaksei Novikau [https://lab.vibelab.net/aliaksei.novikau] " +
					"mentioned this issue in a merge request",
				Notification: email.Notification{Automated: true},
			},
			wantVerd: Unknown, fact: "without being addressed",
		},
		{
			name: "GitLab footer 'Assignee:' is not 'assigned to'",
			msg: email.Message{
				Subject:      "Re: rssp | DOC-00000 Floor index artifact data contract",
				Body:         "Author: Aliaksei Novikau Assignee: Aliaksei Novikau Reviewer: The Commenter",
				Notification: gitlab("activity", "andrei.ramanchyk"),
			},
			wantVerd: NotDirected,
		},
		{
			name: "a person's comment on my PR: directed, but the model's call",
			msg: email.Message{
				Subject: "Re: [rakuten-viber-ads/rssp.gitops] Release VX Engine v0.156.3 (PR #662)",
				Notification: email.Notification{
					Platform: "github", Reason: "author", Sender: "ts-pavlo-yeremenko_rakuten", Automated: true},
			},
			wantVerd: Directed, wantStrong: false, fact: "comment by a person",
		},
		// --- third-person assignment phrasings ---
		{
			name: "requested review from the owner",
			msg: email.Message{
				Body:         "Eliyahu Shvalb requested review from Aliaksei Novikau",
				Notification: gitlab("activity", "eliyahu.shvalb"),
			},
			wantVerd: Directed, wantStrong: true, fact: "asked for a review",
		},
		{
			name: "assigned to the owner",
			msg: email.Message{
				Body:         "Andrei Ramanchyk assigned MON-10311 to Aliaksei Novikau",
				Notification: email.Notification{Automated: true},
			},
			wantVerd: Directed, wantStrong: true, fact: "was assigned",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Assess(tt.msg, o)
			assert.Equal(t, tt.wantVerd, got.Verdict, "facts: %v", got.Facts)
			assert.Equal(t, tt.wantStrong, got.Strong, "facts: %v", got.Facts)
			if tt.fact != "" {
				assert.Contains(t, joined(got.Facts), tt.fact)
			}
		})
	}
}

func TestAtMentionsOwner_NotAnEmailAddress(t *testing.T) {
	// "aliaksei.novikau@viber.com" contains the alias followed by @, not
	// preceded by it — an address in a header line is not a mention.
	o := Owner{Aliases: []string{"aliaksei.novikau"}}
	assert.False(t, atMentionsOwner(email.Message{Body: "mailto:aliaksei.novikau@viber.com"}, o))
	assert.True(t, atMentionsOwner(email.Message{Body: "cc @aliaksei.novikau please look"}, o))
}

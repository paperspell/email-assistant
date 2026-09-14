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

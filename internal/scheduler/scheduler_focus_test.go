package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
	"github.com/paperspell/email-assistant/internal/llm"
)

func jiraMention() email.Message {
	return email.Message{
		UID: 30, Subject: "[JIRA] Ivan mentioned you on PROJ-42", Date: time.Now(),
		FromEmail: "jira@viber.atlassian.net", FromName: "Jira",
		To: []string{"team-mobile@viber.com"}, Cc: []string{"aliaksei.novikau@viber.com"},
		Body: "@aliaksei.novikau could you look at the crash on Android 14?",
	}
}

func TestFocus_RequestCarriesOwnerRecipientsAndFocus(t *testing.T) {
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, jiraMention(), llmMock, "full_body")
	sched.cfg.AccountEmail = "aliaksei.novikau@viber.com"
	sched.cfg.AccountName = "Aliaksei Novikau"
	sched.cfg.Focus = "only mail addressed to me, or tickets and documents that mention me"
	sched.cfg.Aliases = []string{"Aliaksei", "@aliaksei.novikau"}

	pollOnce(t, sched, syncRepo, 29)

	req := llmMock.lastRequest()
	require.NotEmpty(t, req.Subject, "the message must have reached the classifier")
	// Recipients are the fact the focus is judged on; without To/Cc the model
	// cannot tell "addressed to me" from "copied".
	assert.Equal(t, []string{"team-mobile@viber.com"}, req.To)
	assert.Equal(t, []string{"aliaksei.novikau@viber.com"}, req.Cc)
	assert.Equal(t, "aliaksei.novikau@viber.com", req.Owner.Email)
	assert.Equal(t, "Aliaksei Novikau", req.Owner.Name)
	assert.Equal(t, []string{"Aliaksei", "@aliaksei.novikau"}, req.Owner.Aliases)
	assert.Equal(t, sched.cfg.Focus, req.Focus)
}

func TestFocus_UnfocusedAccountDoesNotNameTheOwner(t *testing.T) {
	// Accounts without a focus must keep the exact request they always sent, so
	// their classifications do not drift with this feature.
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, jiraMention(), llmMock, "full_body")
	sched.cfg.AccountEmail = "aliaksei.novikau@viber.com"
	sched.cfg.Aliases = []string{"Aliaksei"} // set but inert without a focus

	pollOnce(t, sched, syncRepo, 29)

	req := llmMock.lastRequest()
	assert.Empty(t, req.Focus)
	assert.Equal(t, llm.Owner{}, req.Owner)
	// Recipients are harmless context and are always passed.
	assert.Equal(t, []string{"team-mobile@viber.com"}, req.To)
}

func botComment() email.Message {
	m := jiraMention()
	m.UID = 31
	m.Subject = "Re: gam-cli  | BUS-28726: Add targeting entity commands (!5)"
	m.To = []string{"aliaksei.novikau@viber.com"}
	m.Cc = nil
	m.Body = "The Commenter commented on merge request !5: the change looks good."
	m.Notification = email.Notification{Platform: "gitlab", Reason: "comment", Sender: "aicode", Automated: true}
	return m
}

func TestFocus_SettledNotDirectedSkipsTheClassifier(t *testing.T) {
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, botComment(), llmMock, "full_body")
	sched.cfg.AccountEmail = "aliaksei.novikau@viber.com"
	sched.cfg.Focus = "only mail addressed to me"
	sched.cfg.BotHandles = []string{"aicode"}

	pollOnce(t, sched, syncRepo, 30)

	// The bot's comment never reached the model — had it, the mock would have
	// called it important and the owner would have been notified.
	assert.Empty(t, llmMock.requests, "a settled out-of-focus message must not cost a classifier call")
	e, err := sched.cfg.EmailRepo.GetByAccountAndUID(context.Background(), "test@example.com", 31)
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Equal(t, domain.StatusIgnored, e.Status)
	assert.Equal(t, "focus", e.DecidedBy)
	// The decision is recorded with the fact behind it, so Details can show
	// why rather than a summary the owner has to take on trust.
	all, err := sched.cfg.ClassificationRepo.GetAllByEmailID(context.Background(), e.ID)
	require.NoError(t, err)
	var found bool
	for _, c := range all {
		if c.Source == domain.SourceFocus {
			found = true
			assert.Contains(t, strings.Join(c.Reason, " "), "automation account")
		}
	}
	assert.True(t, found, "a focus classification must be saved")
}

func TestFocus_DirectedStillGoesToTheClassifierWithFacts(t *testing.T) {
	m := botComment()
	m.Notification.Sender = "andrei.romanchik" // a person, not a bot
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, m, llmMock, "full_body")
	sched.cfg.AccountEmail = "aliaksei.novikau@viber.com"
	sched.cfg.Focus = "only mail addressed to me"
	sched.cfg.BotHandles = []string{"aicode"}

	pollOnce(t, sched, syncRepo, 30)

	req := llmMock.lastRequest()
	require.NotEmpty(t, req.Subject, "a person's comment is for the classifier to summarise")
	assert.Contains(t, strings.Join(req.FocusFacts, "; "), "comment by a person")
}

func TestFocus_NoFocusMeansNoGate(t *testing.T) {
	// Without a focus the bot's comment is ordinary mail: it reaches the
	// classifier exactly as before this feature.
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelImportant, Score: 80}}
	sched, syncRepo := buildContentModeScheduler(t, botComment(), llmMock, "full_body")
	sched.cfg.BotHandles = []string{"aicode"}

	pollOnce(t, sched, syncRepo, 30)

	require.Len(t, llmMock.requests, 1)
	assert.Empty(t, llmMock.lastRequest().FocusFacts)
}

// floorScheduler is a focused work mailbox whose threshold is "important", with
// a classifier that always returns the given verdict.
func floorScheduler(
	t *testing.T, msg email.Message, result llm.ClassifyResult, llmErr error,
) (*Scheduler, *mockNotifier) {
	t.Helper()
	llmMock := &capturingLLMProvider{result: result}
	sched, syncRepo := buildContentModeScheduler(t, msg, llmMock, "full_body")
	sched.cfg.AccountEmail = "aliaksei.novikau@viber.com"
	sched.cfg.Focus = "only mail addressed to me, or tickets and documents that mention me"
	sched.cfg.Aliases = []string{"Aliaksei Novikau", "aliaksei.novikau"}
	sched.cfg.MinImportance = domain.LevelImportant
	if llmErr != nil {
		sched.cfg.LLMProvider = &mockLLMProvider{err: llmErr}
	}
	n := &mockNotifier{}
	sched.cfg.Notifier = n
	pollOnce(t, sched, syncRepo, msg.UID-1)
	return sched, n
}

// jiraMentionFromTheComparison is uid 12631 from the model comparison: Gemini's
// own summary said "Amit mentioned you", and it scored the message "ignore".
func jiraMentionFromTheComparison() email.Message {
	return email.Message{
		UID: 40, Subject: "[JIRA] Amit Epstein mentioned you on MON-10307", Date: time.Now(),
		FromEmail: "jira@rakuten-viber.atlassian.net", FromName: "Amit Epstein (Jira)",
		To:           []string{"aliaksei.novikau@viber.com"},
		Body:         "Amit Epstein mentioned you on MON-10307: this became urgent and blocks the start of testing.",
		Notification: email.Notification{Automated: true},
	}
}

func TestFocusFloor_DirectedMailIsNotifiedWhateverTheModelSays(t *testing.T) {
	sched, n := floorScheduler(t, jiraMentionFromTheComparison(),
		llm.ClassifyResult{Level: domain.LevelIgnore, Score: 15, Summary: "Amit mentioned you on MON-10307."}, nil)

	require.Len(t, n.sent, 1, "a direct mention must reach Telegram even when the model says ignore")
	e, err := sched.cfg.EmailRepo.GetByAccountAndUID(context.Background(), "test@example.com", 40)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusNotified, e.Status)

	all, err := sched.cfg.ClassificationRepo.GetAllByEmailID(context.Background(), e.ID)
	require.NoError(t, err)
	var model, floor *domain.Classification
	for i := range all {
		switch {
		case strings.HasPrefix(all[i].Source, domain.SourceLLM):
			model = &all[i]
		case all[i].Source == domain.SourceFocus:
			floor = &all[i]
		}
	}
	// Both are kept: what the model said, and why it was overruled.
	require.NotNil(t, model, "the model's own verdict is kept for audit")
	assert.Equal(t, domain.LevelIgnore, model.Level)
	require.NotNil(t, floor, "the floor is recorded as its own classification")
	assert.Equal(t, domain.LevelImportant, floor.Level)
	assert.Equal(t, 70, floor.Score)
	assert.Equal(t, "Amit mentioned you on MON-10307.", floor.Summary, "the model still writes the summary")
	assert.Contains(t, strings.Join(floor.Reason, " "), "directed at the owner")
}

func TestFocusFloor_LeavesAHigherVerdictAlone(t *testing.T) {
	sched, n := floorScheduler(t, jiraMentionFromTheComparison(),
		llm.ClassifyResult{Level: domain.LevelCritical, Score: 92, Summary: "Urgent."}, nil)

	require.Len(t, n.sent, 1)
	e, _ := sched.cfg.EmailRepo.GetByAccountAndUID(context.Background(), "test@example.com", 40)
	all, _ := sched.cfg.ClassificationRepo.GetAllByEmailID(context.Background(), e.ID)
	for _, c := range all {
		assert.NotEqual(t, domain.SourceFocus, c.Source, "a floor must never lower or rewrite a higher verdict")
	}
}

func TestFocusFloor_OnlyForDirectedMail(t *testing.T) {
	// A person's mail with the owner in To is "Unknown" to the header layer —
	// a fact for the model, not a verdict. The model's "ignore" stands.
	msg := email.Message{
		UID: 41, Subject: "Accepted: Floor price agent - exploration", Date: time.Now(),
		FromEmail: "ido.shirat@viber.com", To: []string{"aliaksei.novikau@viber.com"},
	}
	_, n := floorScheduler(t, msg, llm.ClassifyResult{Level: domain.LevelIgnore, Score: 15}, nil)

	assert.Empty(t, n.sent)
}

func TestFocusFloor_AppliesWhenTheModelIsDown(t *testing.T) {
	// A classifier outage must not silence a direct mention: the rule-based
	// fallback is floored too.
	_, n := floorScheduler(t, jiraMentionFromTheComparison(), llm.ClassifyResult{}, errors.New("provider down"))

	assert.Len(t, n.sent, 1)
}

func TestFocusFloor_NotWithoutAFocus(t *testing.T) {
	// An unfocused mailbox has no notion of "addressed to me": its model's
	// verdict is final, exactly as before this feature.
	llmMock := &capturingLLMProvider{result: llm.ClassifyResult{Level: domain.LevelIgnore, Score: 15}}
	sched, syncRepo := buildContentModeScheduler(t, jiraMentionFromTheComparison(), llmMock, "full_body")
	sched.cfg.MinImportance = domain.LevelImportant
	n := &mockNotifier{}
	sched.cfg.Notifier = n

	pollOnce(t, sched, syncRepo, 39)

	assert.Empty(t, n.sent)
}

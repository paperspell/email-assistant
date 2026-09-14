package scheduler

import (
	"context"
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

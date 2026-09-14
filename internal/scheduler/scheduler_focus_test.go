package scheduler

import (
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

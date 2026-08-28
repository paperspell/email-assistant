package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

const ruleChat = int64(99999) // matches makeUpdate's chat id

func newRuleHandler(t *testing.T) (
	*Handler, *repo.EmailRepo, *repo.RuleRepo, *repo.ClauseRepo, *repo.PendingRepo, *mockBotClient,
) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	er := repo.NewEmailRepo(sqlDB)
	rr := repo.NewRuleRepo(sqlDB)
	cr := repo.NewClauseRepo(sqlDB)
	pr := repo.NewPendingRepo(sqlDB)
	bot := &mockBotClient{}

	h := &Handler{
		Bot:                bot,
		EmailRepo:          er,
		SenderRepo:         repo.NewSenderRepo(sqlDB),
		ClassificationRepo: repo.NewClassificationRepo(sqlDB),
		RuleRepo:           rr,
		ClauseRepo:         cr,
		PendingRepo:        pr,
		Mailboxes:          map[string]Mailbox{"acc": &fakeMailbox{}},
		Logger:             log.Noop{},
	}
	return h, er, rr, cr, pr, bot
}

func insertEmail(t *testing.T, er *repo.EmailRepo, id, from, listID string, uid uint32) {
	t.Helper()
	require.NoError(t, er.Upsert(context.Background(), &domain.Email{
		ID: id, AccountID: "acc", MessageUID: uid, Subject: "Flash Sale: 50% off",
		FromEmail: from, FromName: "S", Date: time.Now(),
		Status: domain.StatusNotified, ReceivedAt: time.Now(), ListID: listID,
	}))
}

func msgUpd(text string) gotgbot.Update {
	return gotgbot.Update{UpdateId: 1, Message: &gotgbot.Message{Text: text, Chat: gotgbot.Chat{Id: ruleChat}}}
}

func TestIgnore_OpensMenu(t *testing.T) {
	h, er, rr, _, _, bot := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "ignore:e1", 50)))

	assert.Contains(t, bot.editedKeys, int64(50), "ignore opens the menu by editing the keyboard")
	rules, _ := rr.List(context.Background(), "acc")
	assert.Empty(t, rules, "no rule until a menu leaf is chosen")
}

func TestIgnoreLeaf_Sender(t *testing.T) {
	h, er, rr, _, _, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "ign_sender:e1", 50)))

	rules, _ := rr.List(context.Background(), "acc")
	require.Len(t, rules, 1)
	assert.Equal(t, domain.RuleTypeSender, rules[0].Type)
	assert.Equal(t, "a@shop.com", rules[0].Value)
	assert.Equal(t, domain.RuleActionIgnore, rules[0].Action)

	e, _ := er.GetByID(context.Background(), "e1")
	assert.Equal(t, domain.StatusIgnored, e.Status)
	assert.Equal(t, "rule:"+rules[0].ID, e.DecidedBy)
}

func TestIgnoreLeaf_Domain(t *testing.T) {
	h, er, rr, _, _, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@news.shop.com", "", 1)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "ign_domain:e1", 50)))

	rules, _ := rr.List(context.Background(), "acc")
	require.Len(t, rules, 1)
	assert.Equal(t, domain.RuleTypeDomain, rules[0].Type)
	assert.Equal(t, "news.shop.com", rules[0].Value)
}

func TestIgnoreLeaf_ListID(t *testing.T) {
	h, er, rr, _, _, bot := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "deals.shop.com", 1)
	insertEmail(t, er, "e2", "b@shop.com", "", 2)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "ign_listid:e1", 50)))
	rules, _ := rr.List(context.Background(), "acc")
	require.Len(t, rules, 1)
	assert.Equal(t, domain.RuleTypeListID, rules[0].Type)
	assert.Equal(t, "deals.shop.com", rules[0].Value)

	// Email without a List-Id: no rule, informative message.
	require.NoError(t, h.Handle(context.Background(), makeUpdate("q2", "ign_listid:e2", 51)))
	rules, _ = rr.List(context.Background(), "acc")
	assert.Len(t, rules, 1, "no rule created without a List-Id")
	assert.Contains(t, bot.followUps[len(bot.followUps)-1], "no List-Id")
}

func TestIgnoreLeaf_Once_NoRule(t *testing.T) {
	h, er, rr, _, _, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "ign_once:e1", 50)))

	rules, _ := rr.List(context.Background(), "acc")
	assert.Empty(t, rules)
	e, _ := er.GetByID(context.Background(), "e1")
	assert.Equal(t, domain.StatusIgnored, e.Status)
	assert.Empty(t, e.DecidedBy)
}

func TestIgnoreLeaf_Cancel_RestoresAndNoIgnore(t *testing.T) {
	h, er, rr, _, _, bot := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "ign_cancel:e1", 50)))

	rules, _ := rr.List(context.Background(), "acc")
	assert.Empty(t, rules)
	e, _ := er.GetByID(context.Background(), "e1")
	assert.Equal(t, domain.StatusNotified, e.Status, "cancel does not ignore the email")
	assert.Contains(t, bot.editedKeys, int64(50))
}

func TestIgnoreLeaf_Reason_PendingToClause(t *testing.T) {
	h, er, _, cr, pr, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)
	ctx := context.Background()

	require.NoError(t, h.Handle(ctx, makeUpdate("q1", "ign_reason:e1", 50)))
	p, _ := pr.Get(ctx, ruleChat)
	require.NotNil(t, p)
	assert.Equal(t, domain.PendingClause, p.Kind)

	// The next plain message becomes the clause text.
	require.NoError(t, h.Handle(ctx, msgUpd("ignore promotional webinars")))
	clauses, _ := cr.List(ctx, "acc")
	require.Len(t, clauses, 1)
	assert.Equal(t, "ignore promotional webinars", clauses[0].Text)
	gone, _ := pr.Get(ctx, ruleChat)
	assert.Nil(t, gone, "pending cleared after consumption")
}

func TestSubjectFlow_UseSuggestion(t *testing.T) {
	h, er, rr, _, pr, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)
	ctx := context.Background()

	require.NoError(t, h.Handle(ctx, makeUpdate("q1", "ign_subject:e1", 50)))
	p, _ := pr.Get(ctx, ruleChat)
	require.NotNil(t, p)
	assert.Equal(t, domain.PendingSubjectConfirm, p.Kind)
	assert.NotEmpty(t, p.Payload)

	require.NoError(t, h.Handle(ctx, makeUpdate("q2", "subj_use:e1", 50)))
	rules, _ := rr.List(ctx, "acc")
	require.Len(t, rules, 1)
	assert.Equal(t, domain.RuleTypeSubject, rules[0].Type)
	assert.Equal(t, domain.RuleTypeSender, rules[0].ScopeKind)
	assert.Equal(t, "a@shop.com", rules[0].ScopeValue)
}

func TestSubjectFlow_EditOverridesPattern(t *testing.T) {
	h, er, rr, _, _, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)
	ctx := context.Background()

	require.NoError(t, h.Handle(ctx, makeUpdate("q1", "ign_subject:e1", 50)))
	require.NoError(t, h.Handle(ctx, makeUpdate("q2", "subj_edit:e1", 50)))
	require.NoError(t, h.Handle(ctx, msgUpd("custom pattern")))

	rules, _ := rr.List(ctx, "acc")
	require.Len(t, rules, 1)
	assert.Equal(t, "custom pattern", rules[0].Value)
}

func TestPendingOverwrittenByNewChoice(t *testing.T) {
	h, er, _, _, pr, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)
	ctx := context.Background()

	require.NoError(t, h.Handle(ctx, makeUpdate("q1", "ign_reason:e1", 50)))
	require.NoError(t, h.Handle(ctx, makeUpdate("q2", "ign_subject:e1", 50)))

	p, _ := pr.Get(ctx, ruleChat)
	require.NotNil(t, p)
	assert.Equal(t, domain.PendingSubjectConfirm, p.Kind, "newer choice overwrites the pending action")
}

func TestPromoteFollowup_AllowFromSender(t *testing.T) {
	h, er, rr, _, _, _ := newRuleHandler(t)
	insertEmail(t, er, "e1", "a@shop.com", "", 1)

	require.NoError(t, h.Handle(context.Background(), makeUpdate("q1", "prom_allow:e1", 60)))

	rules, _ := rr.List(context.Background(), "acc")
	require.Len(t, rules, 1)
	assert.Equal(t, domain.RuleActionAllow, rules[0].Action)
	assert.Equal(t, domain.RuleTypeSender, rules[0].Type)
	assert.Equal(t, "a@shop.com", rules[0].Value)
}

func TestPromoteFollowup_RemoveRule(t *testing.T) {
	h, _, rr, _, _, _ := newRuleHandler(t)
	ctx := context.Background()
	require.NoError(t, rr.Add(ctx, domain.FilterRule{
		ID: "r1", AccountID: "acc", Action: domain.RuleActionIgnore,
		Type: domain.RuleTypeDomain, Value: "shop.com", Enabled: true,
	}))

	require.NoError(t, h.Handle(ctx, makeUpdate("q1", "prom_rmrule:r1", 60)))

	rules, _ := rr.List(ctx, "acc")
	assert.Empty(t, rules, "the blamed rule is removed")
}

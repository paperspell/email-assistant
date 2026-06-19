package telegram

import (
	"context"
	"errors"
	"sync"
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

// --- mock BotClient ---

type mockBotClient struct {
	mu          sync.Mutex
	answered    []string
	removedKeys []int64
	followUps   []string
}

func (m *mockBotClient) AnswerCallback(queryID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.answered = append(m.answered, queryID)
	return nil
}

func (m *mockBotClient) RemoveKeyboard(msgID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedKeys = append(m.removedKeys, msgID)
	return nil
}

func (m *mockBotClient) SendFollowUp(_ context.Context, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.followUps = append(m.followUps, text)
	return nil
}

// --- fake Mailbox ---

type fakeMailbox struct {
	mu         sync.Mutex
	markedUIDs []uint32
	markErr    error
	body       string
	bodyErr    error
}

func (f *fakeMailbox) MarkRead(_ context.Context, uid uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	f.markedUIDs = append(f.markedUIDs, uid)
	return nil
}

func (f *fakeMailbox) FetchBody(_ context.Context, _ uint32) (string, error) {
	if f.bodyErr != nil {
		return "", f.bodyErr
	}
	return f.body, nil
}

// --- helpers ---

func newTestHandler(
	t *testing.T,
) (*Handler, *repo.EmailRepo, *repo.SenderRepo, *repo.ClassificationRepo, *mockBotClient) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	senderRepo := repo.NewSenderRepo(sqlDB)
	classRepo := repo.NewClassificationRepo(sqlDB)
	mockBot := &mockBotClient{}

	h := &Handler{
		Bot:                mockBot,
		EmailRepo:          emailRepo,
		SenderRepo:         senderRepo,
		ClassificationRepo: classRepo,
		Logger:             log.Noop{},
	}
	return h, emailRepo, senderRepo, classRepo, mockBot
}

func insertTestEmail(t *testing.T, r *repo.EmailRepo, id, fromEmail string) domain.Email {
	t.Helper()
	e := domain.Email{
		ID:         id,
		AccountID:  "acc",
		MessageUID: 1,
		Subject:    "Test subject",
		FromEmail:  fromEmail,
		FromName:   "Test Sender",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNotified,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(context.Background(), e))
	return e
}

func insertTestClassification(t *testing.T, r *repo.ClassificationRepo, emailID string) domain.Classification {
	t.Helper()
	c := domain.Classification{
		ID:           "class-01",
		EmailID:      emailID,
		Level:        domain.LevelImportant,
		Category:     domain.CategoryWork,
		Score:        75,
		Reason:       []string{"baseline: +40", "urgent keyword: +25", "unknown sender: -10"},
		ClassifiedAt: time.Now().UTC().Truncate(time.Second),
		Source:       domain.SourceRuleBased,
	}
	require.NoError(t, r.Save(context.Background(), c))
	return c
}

func insertLLMClassification(t *testing.T, r *repo.ClassificationRepo, emailID string) domain.Classification {
	t.Helper()
	c := domain.Classification{
		ID:           "class-llm-01",
		EmailID:      emailID,
		Level:        domain.LevelImportant,
		Category:     domain.CategoryWork,
		Score:        82,
		Reason:       []string{"llm signal"},
		Summary:      "Important work email requiring action.",
		ClassifiedAt: time.Now().UTC().Truncate(time.Second),
		Source:       domain.SourceLLM + ":mock",
	}
	require.NoError(t, r.Save(context.Background(), c))
	return c
}

func makeUpdate(queryID, data string, msgID int64) gotgbot.Update {
	return gotgbot.Update{
		UpdateId: 1,
		CallbackQuery: &gotgbot.CallbackQuery{
			Id:   queryID,
			Data: data,
			Message: gotgbot.Message{
				MessageId: msgID,
				Chat:      gotgbot.Chat{Id: 99999},
			},
		},
	}
}

// --- tests ---

func TestHandler_NonCallbackUpdate_IsIgnored(t *testing.T) {
	h, _, _, _, mockBot := newTestHandler(t)
	update := gotgbot.Update{UpdateId: 1, Message: &gotgbot.Message{Text: "hello"}}
	require.NoError(t, h.Handle(context.Background(), update))
	assert.Empty(t, mockBot.answered)
	assert.Empty(t, mockBot.removedKeys)
}

func TestHandler_MalformedCallbackData_IsIgnored(t *testing.T) {
	h, _, _, _, mockBot := newTestHandler(t)
	update := makeUpdate("cq-01", "no-colon-here", 100)
	require.NoError(t, h.Handle(context.Background(), update))
	assert.Empty(t, mockBot.answered)
}

func TestHandler_UnknownEmailID_RemovesKeyboard(t *testing.T) {
	h, _, _, _, mockBot := newTestHandler(t)
	update := makeUpdate("cq-01", "handled:nonexistent-id", 100)
	require.NoError(t, h.Handle(context.Background(), update))
	assert.Contains(t, mockBot.answered, "cq-01")
	assert.Contains(t, mockBot.removedKeys, int64(100))
}

func TestHandler_Handled_UpdatesStatusAndSenderScore(t *testing.T) {
	h, emailRepo, senderRepo, _, mockBot := newTestHandler(t)
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com")
	update := makeUpdate("cq-01", "handled:"+e.ID, 100)
	require.NoError(t, h.Handle(ctx, update))

	got, err := emailRepo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusHandled, got.Status)

	sender, err := senderRepo.Get(ctx, "boss@work.com")
	require.NoError(t, err)
	require.NotNil(t, sender)
	assert.Equal(t, feedbackPositiveDelta, sender.ImportanceScore)

	assert.Contains(t, mockBot.answered, "cq-01")
	assert.Contains(t, mockBot.removedKeys, int64(100))
}

func TestHandler_Ignore_UpdatesStatusAndSenderScore(t *testing.T) {
	h, emailRepo, senderRepo, _, mockBot := newTestHandler(t)
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "news@shop.com")
	update := makeUpdate("cq-01", "ignore:"+e.ID, 200)
	require.NoError(t, h.Handle(ctx, update))

	got, err := emailRepo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusIgnored, got.Status)

	sender, err := senderRepo.Get(ctx, "news@shop.com")
	require.NoError(t, err)
	require.NotNil(t, sender)
	// score was 0, delta is -25, clamp to 0
	assert.Equal(t, 0, sender.ImportanceScore)

	assert.Contains(t, mockBot.removedKeys, int64(200))
}

func TestHandler_Ignore_DecreasesExistingSenderScore(t *testing.T) {
	h, emailRepo, senderRepo, _, _ := newTestHandler(t)
	ctx := context.Background()

	// Pre-seed sender with a high score
	require.NoError(t, senderRepo.Upsert(ctx, domain.Sender{
		ID:              "s-01",
		Email:           "news@shop.com",
		ImportanceScore: 60,
		SeenCount:       3,
		UpdatedAt:       time.Now(),
	}))

	e := insertTestEmail(t, emailRepo, "email-01", "news@shop.com")
	require.NoError(t, h.Handle(ctx, makeUpdate("cq-01", "ignore:"+e.ID, 1)))

	sender, err := senderRepo.Get(ctx, "news@shop.com")
	require.NoError(t, err)
	assert.Equal(t, 60-feedbackNegativeDelta, sender.ImportanceScore)
}

func TestHandler_Details_RuleBasedOnly(t *testing.T) {
	h, emailRepo, _, classRepo, mockBot := newTestHandler(t)
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com")
	insertTestClassification(t, classRepo, e.ID)

	update := makeUpdate("cq-01", "details:"+e.ID, 300)
	require.NoError(t, h.Handle(ctx, update))

	require.Len(t, mockBot.followUps, 1)
	msg := mockBot.followUps[0]
	assert.Contains(t, msg, "Email details")
	assert.Contains(t, msg, "boss@work.com")
	assert.Contains(t, msg, "Rule-based classification")
	assert.Contains(t, msg, "baseline: +40")
	// keyboard must NOT be removed for details
	assert.Empty(t, mockBot.removedKeys)
}

func TestHandler_Details_ShowsBothClassifications(t *testing.T) {
	h, emailRepo, _, classRepo, mockBot := newTestHandler(t)
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com")
	insertTestClassification(t, classRepo, e.ID)
	insertLLMClassification(t, classRepo, e.ID)

	update := makeUpdate("cq-01", "details:"+e.ID, 300)
	require.NoError(t, h.Handle(ctx, update))

	require.Len(t, mockBot.followUps, 1)
	msg := mockBot.followUps[0]
	assert.Contains(t, msg, "LLM classification")
	assert.Contains(t, msg, "Important work email requiring action.")
	assert.Contains(t, msg, "Rule-based classification")
	assert.Contains(t, msg, "baseline: +40")
}

func TestHandler_UnknownAction_IsIgnored(t *testing.T) {
	h, emailRepo, _, _, mockBot := newTestHandler(t)
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "x@y.com")
	require.NoError(t, h.Handle(ctx, makeUpdate("cq-01", "snooze:"+e.ID, 100)))

	assert.Contains(t, mockBot.answered, "cq-01")
	assert.Empty(t, mockBot.removedKeys)
	assert.Empty(t, mockBot.followUps)
}

// --- mailbox action tests (Stage 007-02) ---

func TestHandler_Handled_MarksRead(t *testing.T) {
	h, emailRepo, _, _, _ := newTestHandler(t)
	fm := &fakeMailbox{}
	h.Mailboxes = map[string]Mailbox{"acc": fm}

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com") // AccountID "acc", UID 1
	require.NoError(t, h.Handle(context.Background(), makeUpdate("cq-01", "handled:"+e.ID, 100)))

	assert.Equal(t, []uint32{e.MessageUID}, fm.markedUIDs)
}

func TestHandler_Ignore_MarksRead(t *testing.T) {
	h, emailRepo, _, _, _ := newTestHandler(t)
	fm := &fakeMailbox{}
	h.Mailboxes = map[string]Mailbox{"acc": fm}

	e := insertTestEmail(t, emailRepo, "email-01", "spam@x.com")
	require.NoError(t, h.Handle(context.Background(), makeUpdate("cq-01", "ignore:"+e.ID, 100)))

	assert.Equal(t, []uint32{e.MessageUID}, fm.markedUIDs)
}

func TestHandler_MarkReadError_DoesNotFailCallback(t *testing.T) {
	h, emailRepo, _, _, mockBot := newTestHandler(t)
	h.Mailboxes = map[string]Mailbox{"acc": &fakeMailbox{markErr: errors.New("imap down")}}
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com")
	require.NoError(t, h.Handle(ctx, makeUpdate("cq-01", "handled:"+e.ID, 100)))

	got, err := emailRepo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusHandled, got.Status, "status still updated despite mark-read failure")
	assert.Contains(t, mockBot.removedKeys, int64(100), "keyboard still removed")
}

func TestHandler_Details_IncludesBody(t *testing.T) {
	h, emailRepo, _, classRepo, mockBot := newTestHandler(t)
	h.Mailboxes = map[string]Mailbox{"acc": &fakeMailbox{body: "Hello, this is the email body."}}
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com")
	insertTestClassification(t, classRepo, e.ID)
	require.NoError(t, h.Handle(ctx, makeUpdate("cq-01", "details:"+e.ID, 100)))

	require.Len(t, mockBot.followUps, 1)
	assert.Contains(t, mockBot.followUps[0], "Hello, this is the email body.")
	assert.NotContains(t, mockBot.followUps[0], "(body unavailable)")
}

func TestHandler_Details_BodyUnavailable(t *testing.T) {
	h, emailRepo, _, classRepo, mockBot := newTestHandler(t)
	// No mailbox registered for the account.
	ctx := context.Background()

	e := insertTestEmail(t, emailRepo, "email-01", "boss@work.com")
	insertTestClassification(t, classRepo, e.ID)
	require.NoError(t, h.Handle(ctx, makeUpdate("cq-01", "details:"+e.ID, 100)))

	require.Len(t, mockBot.followUps, 1)
	assert.Contains(t, mockBot.followUps[0], "(body unavailable)")
}

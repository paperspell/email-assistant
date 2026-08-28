package telegram

import (
	"context"
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

// mockNotifier records re-sent (promoted) notifications.
type mockNotifier struct {
	mu   sync.Mutex
	sent []domain.Email
}

func (m *mockNotifier) SendNewEmail(
	_ context.Context, e domain.Email, _ domain.Classification, _, _ string,
) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, e)
	return int64(1000 + len(m.sent)), nil
}

func newDigestHandler(t *testing.T) (
	*Handler, *repo.EmailRepo, *repo.DigestRepo, *fakeMailbox, *mockNotifier, *mockBotClient,
) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	emailRepo := repo.NewEmailRepo(sqlDB)
	digestRepo := repo.NewDigestRepo(sqlDB)
	mb := &fakeMailbox{}
	notifier := &mockNotifier{}
	bot := &mockBotClient{}

	h := &Handler{
		Bot:                bot,
		Notifier:           notifier,
		EmailRepo:          emailRepo,
		SenderRepo:         repo.NewSenderRepo(sqlDB),
		ClassificationRepo: repo.NewClassificationRepo(sqlDB),
		DigestRepo:         digestRepo,
		Mailboxes:          map[string]Mailbox{"acc": mb},
		Accounts:           map[string]AccountInfo{"acc": {Name: "Acc", Email: "a@x.com"}},
		Logger:             log.Noop{},
	}
	return h, emailRepo, digestRepo, mb, notifier, bot
}

func ignoredEmail(t *testing.T, er *repo.EmailRepo, id string, uid uint32) {
	t.Helper()
	e := domain.Email{
		ID: id, AccountID: "acc", MessageUID: uid, Subject: "S" + id,
		FromEmail: id + "@s.com", FromName: "N", Date: time.Now(),
		Status: domain.StatusIgnored, ReceivedAt: time.Now(),
	}
	require.NoError(t, er.Upsert(context.Background(), &e))
	require.NoError(t, er.UpdateStatusDecidedBy(context.Background(), id, domain.StatusIgnored, "llm:low"))
}

func testDigest() domain.Digest {
	return domain.Digest{
		ID: "d1", AccountID: "acc", Date: "2026-06-26", TGMessageID: 100, SentAt: time.Now(),
	}
}

func messageUpdate(text string, replyTo int64) gotgbot.Update {
	u := gotgbot.Update{UpdateId: 1, Message: &gotgbot.Message{Text: text, Chat: gotgbot.Chat{Id: 1}}}
	if replyTo != 0 {
		u.Message.ReplyToMessage = &gotgbot.Message{MessageId: replyTo}
	}
	return u
}

func TestHandle_Promote_ReSendsAndMarksPromoted(t *testing.T) {
	h, er, dr, _, notifier, _ := newDigestHandler(t)
	ctx := context.Background()
	ignoredEmail(t, er, "e1", 11)
	ignoredEmail(t, er, "e2", 12)
	require.NoError(t, dr.Save(ctx, testDigest(),
		[]domain.DigestItem{{DigestID: "d1", SeqNo: 1, EmailID: "e1"}, {DigestID: "d1", SeqNo: 2, EmailID: "e2"}}))

	require.NoError(t, h.Handle(ctx, messageUpdate("/important 2", 100)))

	require.Len(t, notifier.sent, 1)
	assert.Equal(t, "e2", notifier.sent[0].ID)

	got, err := er.GetByID(ctx, "e2")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusNotified, got.Status)
	assert.Empty(t, got.DecidedBy, "promote clears provenance")

	items, err := dr.Items(ctx, "d1")
	require.NoError(t, err)
	assert.False(t, items[0].Promoted, "e1 untouched")
	assert.True(t, items[1].Promoted, "e2 promoted")
}

func TestHandle_Promote_NonReplyRejected(t *testing.T) {
	h, er, dr, _, notifier, bot := newDigestHandler(t)
	ctx := context.Background()
	ignoredEmail(t, er, "e1", 11)
	require.NoError(t, dr.Save(ctx, testDigest(),
		[]domain.DigestItem{{DigestID: "d1", SeqNo: 1, EmailID: "e1"}}))

	require.NoError(t, h.Handle(ctx, messageUpdate("/important 1", 0)))

	assert.Empty(t, notifier.sent, "no promote without a reply")
	require.NotEmpty(t, bot.followUps)
	assert.Contains(t, bot.followUps[0], "Reply to a digest")
}

func TestHandle_DigestRemove_OnlyNonPromoted(t *testing.T) {
	h, er, dr, mb, _, _ := newDigestHandler(t)
	ctx := context.Background()
	ignoredEmail(t, er, "e1", 11)
	ignoredEmail(t, er, "e2", 12)
	require.NoError(t, dr.Save(ctx, testDigest(),
		[]domain.DigestItem{{DigestID: "d1", SeqNo: 1, EmailID: "e1"}, {DigestID: "d1", SeqNo: 2, EmailID: "e2"}}))
	require.NoError(t, dr.MarkPromoted(ctx, "d1", 1)) // e1 already promoted

	require.NoError(t, h.Handle(ctx, makeUpdate("cq-1", "digest_remove:d1", 100)))

	assert.Equal(t, []uint32{12}, mb.trashedUIDs, "only the non-promoted item is trashed")
}

func TestHandle_DigestRead_MarksRemainderRead(t *testing.T) {
	h, er, dr, mb, _, _ := newDigestHandler(t)
	ctx := context.Background()
	ignoredEmail(t, er, "e1", 11)
	ignoredEmail(t, er, "e2", 12)
	require.NoError(t, dr.Save(ctx, testDigest(),
		[]domain.DigestItem{{DigestID: "d1", SeqNo: 1, EmailID: "e1"}, {DigestID: "d1", SeqNo: 2, EmailID: "e2"}}))

	require.NoError(t, h.Handle(ctx, makeUpdate("cq-1", "digest_read:d1", 100)))

	assert.ElementsMatch(t, []uint32{11, 12}, mb.markedUIDs)
}

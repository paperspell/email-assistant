package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func TestDigestRepo_SaveAndLookup(t *testing.T) {
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()

	d := domain.Digest{
		ID: "dig-1", AccountID: "a@x.com", Date: "2026-06-26",
		TGMessageID: 555, SentAt: time.Now().UTC().Truncate(time.Second),
	}
	items := []domain.DigestItem{
		{DigestID: "dig-1", SeqNo: 1, EmailID: "e1"},
		{DigestID: "dig-1", SeqNo: 2, EmailID: "e2"},
	}
	require.NoError(t, r.Save(ctx, d, items))

	byMsg, err := r.GetByTGMessageID(ctx, 555)
	require.NoError(t, err)
	require.NotNil(t, byMsg)
	assert.Equal(t, "dig-1", byMsg.ID)
	assert.Equal(t, "2026-06-26", byMsg.Date)

	byDate, err := r.GetByAccountAndDate(ctx, "a@x.com", "2026-06-26")
	require.NoError(t, err)
	require.NotNil(t, byDate)
	assert.Equal(t, "dig-1", byDate.ID)

	got, err := r.Items(ctx, "dig-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "e1", got[0].EmailID)
	assert.False(t, got[0].Promoted)
}

func TestDigestRepo_MarkPromoted(t *testing.T) {
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Save(ctx, domain.Digest{ID: "d1", AccountID: "a", Date: "2026-06-26", SentAt: time.Now()},
		[]domain.DigestItem{{DigestID: "d1", SeqNo: 1, EmailID: "e1"}, {DigestID: "d1", SeqNo: 2, EmailID: "e2"}}))

	require.NoError(t, r.MarkPromoted(ctx, "d1", 2))

	items, err := r.Items(ctx, "d1")
	require.NoError(t, err)
	assert.False(t, items[0].Promoted)
	assert.True(t, items[1].Promoted)
}

func TestDigestRepo_LookupMissing(t *testing.T) {
	r := NewDigestRepo(openTestDB(t))
	got, err := r.GetByTGMessageID(context.Background(), 999)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDigestRepo_UniquePerAccountDate(t *testing.T) {
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()
	d := domain.Digest{ID: "d1", AccountID: "a", Date: "2026-06-26", SentAt: time.Now()}
	require.NoError(t, r.Save(ctx, d, nil))
	d2 := domain.Digest{ID: "d2", AccountID: "a", Date: "2026-06-26", SentAt: time.Now()}
	assert.Error(t, r.Save(ctx, d2, nil), "a second digest for the same account/date is rejected")
}

func TestDigestRepo_SplitDigestResolvesFromAnyPart(t *testing.T) {
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()

	d := domain.Digest{
		ID: "dig-split", AccountID: "a@x.com", Date: "2026-09-11",
		TGMessageID:  903, // the part with the buttons
		TGMessageIDs: []int64{901, 902, 903},
		SentAt:       time.Now().UTC(),
	}
	require.NoError(t, r.Save(ctx, d, []domain.DigestItem{{DigestID: "dig-split", SeqNo: 1, EmailID: "e1"}}))

	// A user replies /important to whichever part the item they mean is in.
	for _, msgID := range []int64{901, 902, 903} {
		got, err := r.GetByTGMessageID(ctx, msgID)
		require.NoError(t, err)
		require.NotNil(t, got, "part %d must resolve to the digest", msgID)
		assert.Equal(t, "dig-split", got.ID)
		// Whichever part was replied to, the keyboard to remove is on the last.
		assert.Equal(t, int64(903), got.TGMessageID)
	}
}

func TestDigestRepo_SingleMessageDigestNeedsNoPartList(t *testing.T) {
	// Callers that predate splitting set only TGMessageID; that message must
	// still be registered as the sole part, or replies to it would stop resolving.
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()

	require.NoError(t, r.Save(ctx, domain.Digest{
		ID: "dig-one", AccountID: "a@x.com", Date: "2026-09-11", TGMessageID: 77, SentAt: time.Now(),
	}, nil))

	got, err := r.GetByTGMessageID(ctx, 77)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "dig-one", got.ID)
}

func TestDigestRepo_SameMessageIDInTwoChatsAreTwoDigests(t *testing.T) {
	// Telegram message ids are per chat: two people's chats will both reach
	// message 1234. Before chats were part of the key this was a UNIQUE
	// violation on the second save — the second friend's digest never recorded.
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Save(ctx, domain.Digest{
		ID: "d-owner", AccountID: "me@x.com", Date: "2026-09-26",
		TGMessageID: 1234, TGMessageIDs: []int64{1234}, TGChatID: 1001, SentAt: time.Now(),
	}, nil))
	require.NoError(t, r.Save(ctx, domain.Digest{
		ID: "d-friend", AccountID: "friend@x.com", Date: "2026-09-26",
		TGMessageID: 1234, TGMessageIDs: []int64{1234}, TGChatID: 2002, SentAt: time.Now(),
	}, nil))

	mine, err := r.GetByTGMessage(ctx, 1001, 1234)
	require.NoError(t, err)
	require.NotNil(t, mine)
	assert.Equal(t, "d-owner", mine.ID)

	theirs, err := r.GetByTGMessage(ctx, 2002, 1234)
	require.NoError(t, err)
	require.NotNil(t, theirs)
	assert.Equal(t, "d-friend", theirs.ID)

	// A chat that received neither resolves nothing.
	none, err := r.GetByTGMessage(ctx, 3003, 1234)
	require.NoError(t, err)
	assert.Nil(t, none)
}

func TestDigestRepo_LegacyDigestResolvesFromAnyChat(t *testing.T) {
	// Digests recorded before accounts had chats carry chat 0. They still
	// resolve — the handler's ownership check is what keeps another chat from
	// acting on them.
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Save(ctx, domain.Digest{
		ID: "d-old", AccountID: "me@x.com", Date: "2026-09-01",
		TGMessageID: 77, SentAt: time.Now(), // TGChatID 0
	}, nil))

	got, err := r.GetByTGMessage(ctx, 1001, 77)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "d-old", got.ID)
	assert.Zero(t, got.TGChatID)
}

func TestDigestRepo_GetByID(t *testing.T) {
	r := NewDigestRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Save(ctx, domain.Digest{
		ID: "d-1", AccountID: "a@x.com", Date: "2026-09-26", TGMessageID: 5, TGChatID: 9, SentAt: time.Now(),
	}, nil))

	got, err := r.GetByID(ctx, "d-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "a@x.com", got.AccountID)
	assert.Equal(t, int64(9), got.TGChatID)

	missing, err := r.GetByID(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

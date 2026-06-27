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

package importance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/domain"
	"github.com/paperspell/email-assistant/internal/email"
)

func newClassifyFilter(t *testing.T) (*Filter, *repo.SenderRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	senderRepo := repo.NewSenderRepo(sqlDB)
	domainRepo := repo.NewDomainRepo(sqlDB)
	return NewFilter(senderRepo, domainRepo), senderRepo
}

func TestFilter_Classify_UnknownSender(t *testing.T) {
	f, senderRepo := newClassifyFilter(t)
	ctx := context.Background()

	c, err := f.Classify(ctx, "email-1", email.Message{
		UID: 1, FromEmail: "stranger@example.com", Subject: "hi", Date: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, domain.SourceRuleBased, c.Source)
	assert.Equal(t, "email-1", c.EmailID)
	assert.NotEmpty(t, c.Reason)
	assert.Contains(t, strings.Join(c.Reason, ";"), "unknown sender")

	// The sender's seen_count is created and incremented to 1.
	s, err := senderRepo.Get(ctx, "stranger@example.com")
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, 1, s.SeenCount)
}

func TestFilter_Classify_IncrementsSeenCountAcrossCalls(t *testing.T) {
	f, senderRepo := newClassifyFilter(t)
	ctx := context.Background()
	msg := email.Message{UID: 1, FromEmail: "repeat@example.com", Subject: "hi", Date: time.Now()}

	_, err := f.Classify(ctx, "e1", msg)
	require.NoError(t, err)
	_, err = f.Classify(ctx, "e2", msg)
	require.NoError(t, err)

	s, err := senderRepo.Get(ctx, "repeat@example.com")
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, 2, s.SeenCount)
}

func TestFilter_Classify_AppliesSenderBonus(t *testing.T) {
	f, senderRepo := newClassifyFilter(t)
	ctx := context.Background()

	// Pre-seed a known, previously-important sender.
	require.NoError(t, senderRepo.Upsert(ctx, domain.Sender{
		ID: "s-1", Email: "boss@example.com", ImportanceScore: 50, SeenCount: 5, UpdatedAt: time.Now(),
	}))

	c, err := f.Classify(ctx, "e1", email.Message{
		UID: 1, FromEmail: "boss@example.com", Subject: "plain", Date: time.Now(),
	})
	require.NoError(t, err)
	// 50/5 = +10 bonus reason is present, and the sender's importance is preserved.
	assert.Contains(t, strings.Join(c.Reason, ";"), "sender previously important")

	s, err := senderRepo.Get(ctx, "boss@example.com")
	require.NoError(t, err)
	assert.Equal(t, 50, s.ImportanceScore, "Classify preserves the sender score")
	assert.Equal(t, 6, s.SeenCount)
}

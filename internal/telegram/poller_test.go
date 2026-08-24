package telegram

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/config"
	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/db/repo"
	"github.com/paperspell/email-assistant/internal/pkg/log"
)

func newOffsetPoller(t *testing.T) (*Poller, *repo.SettingsRepo) {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))

	sr := repo.NewSettingsRepo(sqlDB)
	return &Poller{SettingsRepo: sr, Logger: log.Noop{}}, sr
}

func TestPoller_Offset_RoundTrip(t *testing.T) {
	p, _ := newOffsetPoller(t)
	ctx := context.Background()

	assert.Equal(t, int64(0), p.loadOffset(ctx), "unset offset defaults to 0")

	p.saveOffset(ctx, 123456)
	assert.Equal(t, int64(123456), p.loadOffset(ctx))
}

func TestPoller_LoadOffset_MalformedValue(t *testing.T) {
	p, sr := newOffsetPoller(t)
	ctx := context.Background()
	require.NoError(t, sr.Set(ctx, config.KeyTelegramUpdateOffset, "not-a-number"))

	assert.Equal(t, int64(0), p.loadOffset(ctx), "a malformed stored offset falls back to 0")
}

package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/domain"
)

func setupAccountRepo(t *testing.T) *AccountRepo {
	t.Helper()
	sqlDB, err := db.Open(":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))
	return NewAccountRepo(sqlDB)
}

func sampleAccount(email string) domain.Account {
	return domain.Account{
		ID:           email,
		Name:         "Test",
		Email:        email,
		Host:         "imap.example.com",
		Port:         993,
		Username:     email,
		Password:     "secret",
		TLS:          true,
		PollInterval: 90 * time.Second,
		AuthType:     domain.AuthPassword,
		Enabled:      true,
	}
}

func TestAccountRepo_UpsertGetRoundTrip(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	in := sampleAccount("a@example.com")
	require.NoError(t, r.Upsert(ctx, in))

	got, err := r.Get(ctx, "a@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, in, *got)
}

func TestAccountRepo_GetByName(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, sampleAccount("a@example.com")))

	got, err := r.Get(ctx, "Test")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "a@example.com", got.Email)
}

func TestAccountRepo_GetMissingReturnsNil(t *testing.T) {
	r := setupAccountRepo(t)
	got, err := r.Get(context.Background(), "nobody@example.com")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestAccountRepo_UpsertUpdates(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, sampleAccount("a@example.com")))

	updated := sampleAccount("a@example.com")
	updated.Host = "imap.changed.com"
	updated.Port = 143
	require.NoError(t, r.Upsert(ctx, updated))

	got, err := r.Get(ctx, "a@example.com")
	require.NoError(t, err)
	assert.Equal(t, "imap.changed.com", got.Host)
	assert.Equal(t, 143, got.Port)

	all, err := r.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1, "upsert must not duplicate")
}

func TestAccountRepo_ListEnabled(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, sampleAccount("on@example.com")))
	off := sampleAccount("off@example.com")
	off.Enabled = false
	require.NoError(t, r.Upsert(ctx, off))

	all, err := r.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	enabled, err := r.ListEnabled(ctx)
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	assert.Equal(t, "on@example.com", enabled[0].Email)
}

func TestAccountRepo_SetEnabled(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, sampleAccount("a@example.com")))

	require.NoError(t, r.SetEnabled(ctx, "a@example.com", false))
	got, err := r.Get(ctx, "a@example.com")
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	require.NoError(t, r.SetEnabled(ctx, "a@example.com", true))
	got, err = r.Get(ctx, "a@example.com")
	require.NoError(t, err)
	assert.True(t, got.Enabled)
}

func TestAccountRepo_Delete(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, sampleAccount("a@example.com")))
	require.NoError(t, r.Delete(ctx, "a@example.com"))

	got, err := r.Get(ctx, "a@example.com")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestAccountRepo_UpsertDefaultsAuthType(t *testing.T) {
	r := setupAccountRepo(t)
	ctx := context.Background()
	acc := sampleAccount("a@example.com")
	acc.AuthType = "" // unset
	require.NoError(t, r.Upsert(ctx, acc))

	got, err := r.Get(ctx, "a@example.com")
	require.NoError(t, err)
	assert.Equal(t, domain.AuthPassword, got.AuthType)
}

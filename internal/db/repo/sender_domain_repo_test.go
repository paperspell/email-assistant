package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

const testAcct = "acc-1"

func TestSenderRepo_GetNotFound(t *testing.T) {
	r := NewSenderRepo(openTestDB(t))
	got, err := r.Get(context.Background(), testAcct, "nobody@example.com")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSenderRepo_UpsertAndGet(t *testing.T) {
	r := NewSenderRepo(openTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, r.Upsert(ctx, domain.Sender{
		ID: "s-1", AccountID: testAcct, Email: "a@example.com", ImportanceScore: 40, SeenCount: 2, UpdatedAt: now,
	}))

	got, err := r.Get(ctx, testAcct, "a@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "a@example.com", got.Email)
	assert.Equal(t, 40, got.ImportanceScore)
	assert.Equal(t, 2, got.SeenCount)
	assert.True(t, now.Equal(got.UpdatedAt))
}

func TestSenderRepo_UpsertUpdatesExisting(t *testing.T) {
	r := NewSenderRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, domain.Sender{
		ID: "s-1", AccountID: testAcct, Email: "a@example.com", ImportanceScore: 40, SeenCount: 1, UpdatedAt: time.Now(),
	}))
	require.NoError(t, r.Upsert(ctx, domain.Sender{
		ID: "s-1", AccountID: testAcct, Email: "a@example.com", ImportanceScore: 65, SeenCount: 3, UpdatedAt: time.Now(),
	}))

	got, err := r.Get(ctx, testAcct, "a@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 65, got.ImportanceScore)
	assert.Equal(t, 3, got.SeenCount)
}

func TestSenderRepo_PerAccountIsolation(t *testing.T) {
	r := NewSenderRepo(openTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, r.Upsert(ctx, domain.Sender{
		ID: "s-1", AccountID: "acc-a", Email: "x@example.com", ImportanceScore: 50, SeenCount: 1, UpdatedAt: now,
	}))
	require.NoError(t, r.Upsert(ctx, domain.Sender{
		ID: "s-2", AccountID: "acc-b", Email: "x@example.com", ImportanceScore: 90, SeenCount: 1, UpdatedAt: now,
	}))

	a, err := r.Get(ctx, "acc-a", "x@example.com")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, 50, a.ImportanceScore)

	b, err := r.Get(ctx, "acc-b", "x@example.com")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, 90, b.ImportanceScore, "same address scores independently per account")
}

func TestDomainRepo_GetNotFound(t *testing.T) {
	r := NewDomainRepo(openTestDB(t))
	got, err := r.Get(context.Background(), testAcct, "missing.com")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDomainRepo_UpsertAndGet(t *testing.T) {
	r := NewDomainRepo(openTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, r.Upsert(ctx, domain.Record{
		ID: "d-1", AccountID: testAcct, Domain: "acme.com", ImportanceScore: 55, UpdatedAt: now,
	}))

	got, err := r.Get(ctx, testAcct, "acme.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "acme.com", got.Domain)
	assert.Equal(t, 55, got.ImportanceScore)
	assert.True(t, now.Equal(got.UpdatedAt))
}

func TestDomainRepo_UpsertUpdatesExisting(t *testing.T) {
	r := NewDomainRepo(openTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, domain.Record{
		ID: "d-1", AccountID: testAcct, Domain: "acme.com", ImportanceScore: 20, UpdatedAt: time.Now(),
	}))
	require.NoError(t, r.Upsert(ctx, domain.Record{
		ID: "d-1", AccountID: testAcct, Domain: "acme.com", ImportanceScore: 80, UpdatedAt: time.Now(),
	}))

	got, err := r.Get(ctx, testAcct, "acme.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 80, got.ImportanceScore)
}

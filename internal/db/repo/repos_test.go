package repo

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/db"
	"github.com/paperspell/email-assistant/internal/domain"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(context.Background(), sqlDB))
	return sqlDB
}

// EmailRepo tests

func TestEmailRepo_Upsert_New(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	ctx := context.Background()

	e := domain.Email{
		ID:         "01ABC",
		AccountID:  "acc1",
		MessageUID: 42,
		Subject:    "Hello",
		FromEmail:  "sender@example.com",
		FromName:   "Sender",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}

	require.NoError(t, r.Upsert(ctx, e))

	got, err := r.GetByAccountAndUID(ctx, "acc1", 42)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.Subject, got.Subject)
	assert.Equal(t, domain.StatusNew, got.Status)
}

func TestEmailRepo_Upsert_Duplicate(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	ctx := context.Background()

	base := domain.Email{
		ID:         "01ABC",
		AccountID:  "acc1",
		MessageUID: 10,
		Subject:    "Original",
		FromEmail:  "a@b.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(ctx, base))

	updated := base
	updated.Subject = "Updated"
	require.NoError(t, r.Upsert(ctx, updated))

	got, err := r.GetByAccountAndUID(ctx, "acc1", 10)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated", got.Subject)
}

func TestEmailRepo_GetByAccountAndUID_NotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)

	got, err := r.GetByAccountAndUID(context.Background(), "acc1", 99)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestEmailRepo_UpdateStatus(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	ctx := context.Background()

	e := domain.Email{
		ID:         "01XYZ",
		AccountID:  "acc1",
		MessageUID: 5,
		Subject:    "Test",
		FromEmail:  "x@y.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(ctx, e))
	require.NoError(t, r.UpdateStatus(ctx, e.ID, domain.StatusNotified))

	got, err := r.GetByAccountAndUID(ctx, "acc1", 5)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.StatusNotified, got.Status)
}

func TestEmailRepo_UpdateStatus_NotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	err := r.UpdateStatus(context.Background(), "nonexistent", domain.StatusNotified)
	require.Error(t, err)
}

// SyncStateRepo tests

func TestSyncStateRepo_GetNotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewSyncStateRepo(d)

	got, err := r.Get(context.Background(), "unknown-account")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSyncStateRepo_Upsert_And_Get(t *testing.T) {
	d := openTestDB(t)
	r := NewSyncStateRepo(d)
	ctx := context.Background()

	s := domain.SyncState{
		AccountID: "acc1",
		LastUID:   100,
		SyncedAt:  time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(ctx, s))

	got, err := r.Get(ctx, "acc1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, uint32(100), got.LastUID)
	assert.Equal(t, s.SyncedAt, got.SyncedAt)
}

func TestSyncStateRepo_Upsert_Update(t *testing.T) {
	d := openTestDB(t)
	r := NewSyncStateRepo(d)
	ctx := context.Background()

	s := domain.SyncState{AccountID: "acc1", LastUID: 10, SyncedAt: time.Now().UTC().Truncate(time.Second)}
	require.NoError(t, r.Upsert(ctx, s))

	s.LastUID = 50
	require.NoError(t, r.Upsert(ctx, s))

	got, err := r.Get(ctx, "acc1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, uint32(50), got.LastUID)
}

func TestEmailRepo_AccountIsolation(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	ctx := context.Background()

	base := domain.Email{
		MessageUID: 1,
		Subject:    "Same UID",
		FromEmail:  "x@y.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}

	e1 := base
	e1.ID = "id-acc1"
	e1.AccountID = "acc1"
	require.NoError(t, r.Upsert(ctx, e1))

	e2 := base
	e2.ID = "id-acc2"
	e2.AccountID = "acc2"
	e2.Subject = "Different subject"
	require.NoError(t, r.Upsert(ctx, e2))

	got1, err := r.GetByAccountAndUID(ctx, "acc1", 1)
	require.NoError(t, err)
	require.NotNil(t, got1)
	assert.Equal(t, "Same UID", got1.Subject)

	got2, err := r.GetByAccountAndUID(ctx, "acc2", 1)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, "Different subject", got2.Subject)
}

func TestEmailRepo_MultipleEmails(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	ctx := context.Background()

	for i := range 5 {
		e := domain.Email{
			ID:         fmt.Sprintf("id-%d", i),
			AccountID:  "acc1",
			MessageUID: uint32(i + 1),
			Subject:    fmt.Sprintf("Email %d", i),
			FromEmail:  "x@y.com",
			Date:       time.Now().UTC().Truncate(time.Second),
			Status:     domain.StatusNew,
			ReceivedAt: time.Now().UTC().Truncate(time.Second),
		}
		require.NoError(t, r.Upsert(ctx, e))
	}

	for i := range 5 {
		got, err := r.GetByAccountAndUID(ctx, "acc1", uint32(i+1))
		require.NoError(t, err)
		require.NotNil(t, got, "expected email with uid %d", i+1)
		assert.Equal(t, fmt.Sprintf("Email %d", i), got.Subject)
	}
}

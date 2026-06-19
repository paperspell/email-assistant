package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func TestEmailRepo_GetByID(t *testing.T) {
	r := NewEmailRepo(openTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	e := domain.Email{
		ID: "e-1", AccountID: "acc", MessageUID: 7, Subject: "Hello",
		FromEmail: "a@b.com", FromName: "A", Date: now,
		Status: domain.StatusNotified, ReceivedAt: now,
	}
	require.NoError(t, r.Upsert(ctx, e))

	got, err := r.GetByID(ctx, "e-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "a@b.com", got.FromEmail)
	assert.Equal(t, uint32(7), got.MessageUID)
	assert.Equal(t, "acc", got.AccountID)
}

func TestEmailRepo_GetByID_NotFound(t *testing.T) {
	r := NewEmailRepo(openTestDB(t))
	got, err := r.GetByID(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, got)
}

package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func TestPendingRepo_SingleRowPerChat(t *testing.T) {
	r := NewPendingRepo(openTestDB(t))
	ctx := context.Background()

	require.NoError(t, r.Set(ctx, domain.PendingAction{
		ChatID: 7, Kind: domain.PendingClause, EmailID: "e1", AccountID: "acc",
	}))
	// A second Set for the same chat overwrites.
	require.NoError(t, r.Set(ctx, domain.PendingAction{
		ChatID: 7, Kind: domain.PendingSubjectConfirm, EmailID: "e1", AccountID: "acc", Payload: "flash sale",
	}))

	got, err := r.Get(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.PendingSubjectConfirm, got.Kind)
	assert.Equal(t, "flash sale", got.Payload)

	require.NoError(t, r.Delete(ctx, 7))
	gone, err := r.Get(ctx, 7)
	require.NoError(t, err)
	assert.Nil(t, gone)
}

func TestPendingRepo_GetMissing(t *testing.T) {
	r := NewPendingRepo(openTestDB(t))
	got, err := r.Get(context.Background(), 123)
	require.NoError(t, err)
	assert.Nil(t, got)
}

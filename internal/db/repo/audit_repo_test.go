package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func seedEmail(t *testing.T, r *EmailRepo, id string) {
	t.Helper()
	require.NoError(t, r.Upsert(context.Background(), domain.Email{
		ID:         id,
		AccountID:  "acc1",
		MessageUID: 1,
		Subject:    "Test",
		FromEmail:  "a@b.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}))
}

func TestAuditRepo_List_Empty(t *testing.T) {
	d := openTestDB(t)
	r := NewAuditRepo(d)

	entries, err := r.List(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAuditRepo_List_ReturnsNewestFirst(t *testing.T) {
	d := openTestDB(t)
	emailRepo := NewEmailRepo(d)
	r := NewAuditRepo(d)
	ctx := context.Background()

	// One email, two audit entries at different timestamps
	seedEmail(t, emailRepo, "email-list")

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 2 {
		require.NoError(t, r.Save(ctx, AuditEntry{
			ID:          fmt.Sprintf("audit-%02d", i),
			EmailID:     "email-list",
			Provider:    "anthropic",
			Model:       "llm:anthropic",
			ContentMode: "headers_only",
			BytesSent:   100 + i,
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		}))
	}

	entries, err := r.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.True(t, entries[0].CreatedAt.After(entries[1].CreatedAt), "newest entry must be first")
}

func TestAuditRepo_List_RespectsLimit(t *testing.T) {
	d := openTestDB(t)
	emailRepo := NewEmailRepo(d)
	r := NewAuditRepo(d)
	ctx := context.Background()

	seedEmail(t, emailRepo, "email-limit")

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		require.NoError(t, r.Save(ctx, AuditEntry{
			ID:          fmt.Sprintf("audit-lim-%02d", i),
			EmailID:     "email-limit",
			Provider:    "openai",
			Model:       "llm:openai",
			ContentMode: "full_body",
			BytesSent:   200,
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		}))
	}

	entries, err := r.List(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
}

func TestAuditRepo_Save_RoundTrip(t *testing.T) {
	d := openTestDB(t)
	emailRepo := NewEmailRepo(d)
	auditRepo := NewAuditRepo(d)
	ctx := context.Background()

	seedEmail(t, emailRepo, "email-01")

	entry := AuditEntry{
		ID:          "audit-01",
		EmailID:     "email-01",
		Provider:    "anthropic",
		Model:       "llm:anthropic",
		ContentMode: "redacted_body",
		BytesSent:   512,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, auditRepo.Save(ctx, entry))

	var got AuditEntry
	var createdAtStr string
	row := d.QueryRowContext(ctx,
		`SELECT id, email_id, provider, model, content_mode, bytes_sent, created_at
		 FROM llm_audit_log WHERE id = ?`, entry.ID)
	require.NoError(t, row.Scan(
		&got.ID, &got.EmailID, &got.Provider, &got.Model,
		&got.ContentMode, &got.BytesSent, &createdAtStr,
	))
	got.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)

	assert.Equal(t, entry.ID, got.ID)
	assert.Equal(t, entry.EmailID, got.EmailID)
	assert.Equal(t, entry.Provider, got.Provider)
	assert.Equal(t, entry.Model, got.Model)
	assert.Equal(t, entry.ContentMode, got.ContentMode)
	assert.Equal(t, entry.BytesSent, got.BytesSent)
	assert.Equal(t, entry.CreatedAt, got.CreatedAt)
}

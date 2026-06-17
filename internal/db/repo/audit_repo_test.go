package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func seedEmail(t *testing.T, r *EmailRepo, ctx context.Context, id string) {
	t.Helper()
	require.NoError(t, r.Upsert(ctx, domain.Email{
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

func TestAuditRepo_Save_RoundTrip(t *testing.T) {
	d := openTestDB(t)
	emailRepo := NewEmailRepo(d)
	auditRepo := NewAuditRepo(d)
	ctx := context.Background()

	seedEmail(t, emailRepo, ctx, "email-01")

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

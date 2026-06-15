package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paperspell/email-assistant/internal/domain"
)

func TestClassificationRepo_Save_And_Get(t *testing.T) {
	d := openTestDB(t)
	r := NewClassificationRepo(d)
	ctx := context.Background()

	// need an email to reference
	emailRepo := NewEmailRepo(d)
	e := domain.Email{
		ID:         "email-01",
		AccountID:  "acc1",
		MessageUID: 1,
		Subject:    "Test",
		FromEmail:  "a@b.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, emailRepo.Upsert(ctx, e))

	c := domain.Classification{
		ID:           "cls-01",
		EmailID:      "email-01",
		Level:        domain.LevelImportant,
		Category:     domain.CategoryWork,
		Score:        75,
		Reason:       []string{"meeting keyword", "active conversation"},
		ClassifiedAt: time.Now().UTC().Truncate(time.Second),
		Source:       domain.SourceRuleBased,
		Summary:      "",
	}
	require.NoError(t, r.Save(ctx, c))

	got, err := r.GetByEmailID(ctx, "email-01")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.LevelImportant, got.Level)
	assert.Equal(t, domain.CategoryWork, got.Category)
	assert.Equal(t, 75, got.Score)
	assert.Equal(t, c.Reason, got.Reason)
	assert.Equal(t, domain.SourceRuleBased, got.Source)
}

func TestClassificationRepo_GetByEmailIDAndSource(t *testing.T) {
	d := openTestDB(t)
	r := NewClassificationRepo(d)
	ctx := context.Background()

	emailRepo := NewEmailRepo(d)
	e := domain.Email{
		ID: "email-02", AccountID: "acc1", MessageUID: 2,
		Subject: "Test", FromEmail: "a@b.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNew,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, emailRepo.Upsert(ctx, e))

	rule := domain.Classification{
		ID: "cls-rule", EmailID: "email-02",
		Level: domain.LevelMaybe, Score: 55,
		Reason: []string{"baseline: +40"}, ClassifiedAt: time.Now().UTC().Truncate(time.Second),
		Source: domain.SourceRuleBased,
	}
	llmC := domain.Classification{
		ID: "cls-llm", EmailID: "email-02",
		Level: domain.LevelImportant, Score: 80,
		Reason: []string{"llm signal"}, Summary: "Work email needing action.",
		ClassifiedAt: time.Now().UTC().Truncate(time.Second),
		Source:       domain.SourceLLM + ":anthropic",
	}
	require.NoError(t, r.Save(ctx, rule))
	require.NoError(t, r.Save(ctx, llmC))

	// GetByEmailID should prefer rule_based
	got, err := r.GetByEmailID(ctx, "email-02")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.SourceRuleBased, got.Source)

	// GetByEmailIDAndSource returns the specific one
	gotLLM, err := r.GetByEmailIDAndSource(ctx, "email-02", domain.SourceLLM+":anthropic")
	require.NoError(t, err)
	require.NotNil(t, gotLLM)
	assert.Equal(t, "Work email needing action.", gotLLM.Summary)
	assert.Equal(t, domain.LevelImportant, gotLLM.Level)

	// GetAllByEmailID returns both, rule_based first
	all, err := r.GetAllByEmailID(ctx, "email-02")
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, domain.SourceRuleBased, all[0].Source)
}

func TestClassificationRepo_GetByEmailID_NotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewClassificationRepo(d)
	got, err := r.GetByEmailID(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSenderRepo_Upsert_And_Get(t *testing.T) {
	d := openTestDB(t)
	r := NewSenderRepo(d)
	ctx := context.Background()

	s := domain.Sender{
		ID:              "s-01",
		Email:           "alice@example.com",
		ImportanceScore: 20,
		SeenCount:       5,
		UpdatedAt:       time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(ctx, s))

	got, err := r.Get(ctx, "alice@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 20, got.ImportanceScore)
	assert.Equal(t, 5, got.SeenCount)
}

func TestSenderRepo_Get_NotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewSenderRepo(d)
	got, err := r.Get(context.Background(), "nobody@example.com")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSenderRepo_Upsert_Increments(t *testing.T) {
	d := openTestDB(t)
	r := NewSenderRepo(d)
	ctx := context.Background()

	s := domain.Sender{ID: "s-01", Email: "a@b.com", SeenCount: 1, UpdatedAt: time.Now().UTC()}
	require.NoError(t, r.Upsert(ctx, s))

	s.SeenCount = 2
	require.NoError(t, r.Upsert(ctx, s))

	got, err := r.Get(ctx, "a@b.com")
	require.NoError(t, err)
	assert.Equal(t, 2, got.SeenCount)
}

func TestDomainRepo_Upsert_And_Get(t *testing.T) {
	d := openTestDB(t)
	r := NewDomainRepo(d)
	ctx := context.Background()

	rec := domain.Record{
		ID:              "d-01",
		Domain:          "school.pl",
		ImportanceScore: 30,
		UpdatedAt:       time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(ctx, rec))

	got, err := r.Get(ctx, "school.pl")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 30, got.ImportanceScore)
}

func TestDomainRepo_Get_NotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewDomainRepo(d)
	got, err := r.Get(context.Background(), "unknown.com")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestEmailRepo_SetTelegramMessageID(t *testing.T) {
	d := openTestDB(t)
	r := NewEmailRepo(d)
	ctx := context.Background()

	e := domain.Email{
		ID:         "email-tg-01",
		AccountID:  "acc",
		MessageUID: 99,
		Subject:    "Test",
		FromEmail:  "a@b.com",
		Date:       time.Now().UTC().Truncate(time.Second),
		Status:     domain.StatusNotified,
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, r.Upsert(ctx, e))
	require.NoError(t, r.SetTelegramMessageID(ctx, e.ID, 42001))

	got, err := r.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(42001), got.TelegramMessageID)
}

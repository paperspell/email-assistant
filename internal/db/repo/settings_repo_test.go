package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsRepo_Get_NotFound(t *testing.T) {
	d := openTestDB(t)
	r := NewSettingsRepo(d)

	val, err := r.Get(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, val)
}

func TestSettingsRepo_Set_Get_RoundTrip(t *testing.T) {
	d := openTestDB(t)
	r := NewSettingsRepo(d)
	ctx := context.Background()

	require.NoError(t, r.Set(ctx, "log.level", "debug"))

	val, err := r.Get(ctx, "log.level")
	require.NoError(t, err)
	assert.Equal(t, "debug", val)
}

func TestSettingsRepo_Set_Overwrite(t *testing.T) {
	d := openTestDB(t)
	r := NewSettingsRepo(d)
	ctx := context.Background()

	require.NoError(t, r.Set(ctx, "log.level", "info"))
	require.NoError(t, r.Set(ctx, "log.level", "warn"))

	val, err := r.Get(ctx, "log.level")
	require.NoError(t, err)
	assert.Equal(t, "warn", val)
}

func TestSettingsRepo_GetAll(t *testing.T) {
	d := openTestDB(t)
	r := NewSettingsRepo(d)
	ctx := context.Background()

	require.NoError(t, r.Set(ctx, "k1", "v1"))
	require.NoError(t, r.Set(ctx, "k2", "v2"))

	all, err := r.GetAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, "v1", all["k1"])
	assert.Equal(t, "v2", all["k2"])
}

func TestSettingsRepo_GetAll_Empty(t *testing.T) {
	d := openTestDB(t)
	r := NewSettingsRepo(d)

	all, err := r.GetAll(context.Background())
	require.NoError(t, err)
	assert.Empty(t, all)
}

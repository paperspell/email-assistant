package errx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrap_Nil(t *testing.T) {
	err := Wrap(context.Background(), nil, "some message")
	assert.NoError(t, err)
}

func TestWrap_NonNil(t *testing.T) {
	inner := New(context.Background(), "inner error")
	err := Wrap(context.Background(), inner, "outer message")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outer message")
	assert.Contains(t, err.Error(), "inner error")
}

func TestNew_Message(t *testing.T) {
	err := New(context.Background(), "something went wrong")
	require.Error(t, err)
	assert.Equal(t, "something went wrong", err.Error())
}

func TestNew_KeyValues(t *testing.T) {
	err := New(context.Background(), "oops", "key", "value", "count", 42)
	require.Error(t, err)
	kvHolder, ok := err.(KeysValuesHolder)
	require.True(t, ok)
	kvs := kvHolder.KeysValues()
	assert.Equal(t, []any{"key", "value", "count", 42}, kvs)
}

func TestWrap_KeyValuesPropagation(t *testing.T) {
	inner := New(context.Background(), "inner", "k1", "v1")
	outer := Wrap(context.Background(), inner, "outer", "k2", "v2")

	kvHolder, ok := outer.(KeysValuesHolder)
	require.True(t, ok)
	kvs := kvHolder.KeysValues()
	assert.Contains(t, kvs, "k1")
	assert.Contains(t, kvs, "v1")
	assert.Contains(t, kvs, "k2")
	assert.Contains(t, kvs, "v2")
}

func TestNew_StacktraceCapture(t *testing.T) {
	err := New(context.Background(), "trace test")
	traceHolder, ok := err.(TraceSource)
	require.True(t, ok)
	assert.NotEmpty(t, traceHolder.Trace())
}

func TestNormaliseKVs_OddLength(t *testing.T) {
	kvs := normaliseKVs([]any{"key"})
	assert.Len(t, kvs, 2)
	assert.Equal(t, "", kvs[1])
}

func TestNormaliseKVs_EvenLength(t *testing.T) {
	kvs := normaliseKVs([]any{"key", "val"})
	assert.Len(t, kvs, 2)
}

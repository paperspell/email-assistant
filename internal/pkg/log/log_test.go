package log

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoop_AllMethodsNoPanic(t *testing.T) {
	n := Noop{}
	assert.NotPanics(t, func() {
		n.Debug("msg")
		n.Info("msg")
		n.Warn(errors.New("err"))
		n.Error(errors.New("err"))
		_ = n.With("k", "v")
	})
}

func TestNoop_EnabledReturnsFalse(t *testing.T) {
	n := Noop{}
	assert.False(t, n.InfoEnabled())
	assert.False(t, n.ErrorEnabled())
	assert.False(t, n.WarnEnabled())
	assert.False(t, n.DebugEnabled())
}

func TestNoop_WithReturnsSelf(t *testing.T) {
	n := Noop{}
	assert.IsType(t, Noop{}, n.With("k", "v"))
}

func TestIntoContext_FromContext_RoundTrip(t *testing.T) {
	logger := Noop{}
	ctx := IntoContext(context.Background(), logger)
	got := FromContext(ctx)
	assert.Equal(t, logger, got)
}

func TestFromContext_Missing_ReturnsNoop(t *testing.T) {
	got := FromContext(context.Background())
	assert.IsType(t, Noop{}, got)
}

func TestNewLogger_InfoLevel_FiltersDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(LoggerConfig{Dev: false, Level: LevelInfo, Output: buf})
	logger.Debug("should not appear")
	assert.Empty(t, buf.String())
}

func TestNewLogger_DebugLevel_ShowsDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(LoggerConfig{Dev: false, Level: LevelDebug, Output: buf})
	logger.Debug("debug message")
	assert.Contains(t, buf.String(), "debug message")
}

func TestNewLogger_DevMode_ColorizedOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(LoggerConfig{Dev: true, Level: LevelInfo, Output: buf})
	logger.Info("hello dev")
	assert.Contains(t, buf.String(), "hello dev")
}

func TestNewLogger_With_AttachesFields(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(LoggerConfig{Dev: false, Level: LevelInfo, Output: buf})
	child := logger.With("component", "test")
	require.NotNil(t, child)
	child.Info("message")
	assert.Contains(t, buf.String(), "component")
	assert.Contains(t, buf.String(), "test")
}

func TestNewLogger_Error_NilNoPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(LoggerConfig{Dev: false, Level: LevelError, Output: buf})
	assert.NotPanics(t, func() { logger.Error(nil) })
	assert.Empty(t, buf.String())
}

func TestNewLogger_Warn_NilNoPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(LoggerConfig{Dev: false, Level: LevelWarn, Output: buf})
	assert.NotPanics(t, func() { logger.Warn(nil) })
	assert.Empty(t, buf.String())
}

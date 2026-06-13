package ioutil

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockCloser struct {
	err error
}

func (m *mockCloser) Close() error { return m.err }

type mockLogger struct {
	logged []error
}

func (m *mockLogger) Error(err error, _ ...any) { m.logged = append(m.logged, err) }

func TestCloseWithLog_Success(t *testing.T) {
	logger := &mockLogger{}
	CloseWithLog(&mockCloser{err: nil}, logger)
	assert.Empty(t, logger.logged)
}

func TestCloseWithLog_Error(t *testing.T) {
	logger := &mockLogger{}
	CloseWithLog(&mockCloser{err: errors.New("disk full")}, logger)
	assert.Len(t, logger.logged, 1)
	assert.Contains(t, logger.logged[0].Error(), "disk full")
}

func TestCloseWithLog_NilCloser(t *testing.T) {
	logger := &mockLogger{}
	assert.NotPanics(t, func() { CloseWithLog(nil, logger) })
	assert.Empty(t, logger.logged)
}

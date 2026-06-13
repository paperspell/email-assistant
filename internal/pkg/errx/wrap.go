package errx

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

var _ TraceSource = (*wrapped)(nil)      //nolint:errcheck
var _ ContextHolder = (*wrapped)(nil)    //nolint:errcheck
var _ KeysValuesHolder = (*wrapped)(nil) //nolint:errcheck
var _ Unwrapped = (*wrapped)(nil)        //nolint:errcheck

// TraceSource is an interface that contains a stacktrace.
type TraceSource interface {
	Trace() []TraceEntry
}

// ContextHolder is an interface that contains a context.
type ContextHolder interface {
	Contexts() []context.Context
}

// KeysValuesHolder is an interface that contains structured key-value pairs.
type KeysValuesHolder interface {
	KeysValues() []any
}

// Unwrapped is an interface that exposes the wrapped error.
type Unwrapped interface {
	Unwrap() error
}

type wrapped struct {
	initial error
	message string
	trace   []TraceEntry
	ctx     context.Context
	kvs     []any
}

// Contexts returns all contexts accumulated in the error chain.
func (e *wrapped) Contexts() (out []context.Context) {
	if e.ctx != nil {
		out = append(out, e.ctx)
	}
	var wrappedErr = e.initial
	for wrappedErr != nil {
		if ctxHolder, ok := wrappedErr.(ContextHolder); ok {
			out = append(out, ctxHolder.Contexts()...)
			break
		}
		wrappedErr = errors.Unwrap(wrappedErr)
	}
	return slices.Compact(out)
}

// KeysValues returns all key-value pairs accumulated in the error chain.
func (e *wrapped) KeysValues() (kvs []any) {
	kvs = e.kvs
	var wrappedErr = e.initial
	for wrappedErr != nil {
		if kvHolder, ok := wrappedErr.(KeysValuesHolder); ok {
			kvs = append(kvs, kvHolder.KeysValues()...)
			break
		}
		wrappedErr = errors.Unwrap(wrappedErr)
	}
	return kvs
}

// Trace returns the deepest stacktrace in the error chain.
func (e *wrapped) Trace() (trace []TraceEntry) {
	trace = e.trace
	var wrappedErr = e.initial
	for wrappedErr != nil {
		if traceHolder, ok := wrappedErr.(TraceSource); ok && len(traceHolder.Trace()) > 0 {
			trace = traceHolder.Trace()
			break
		}
		wrappedErr = errors.Unwrap(wrappedErr)
	}
	return
}

func (e *wrapped) Error() string {
	if e.initial == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %s", e.message, e.initial.Error())
}

func (e *wrapped) Unwrap() error {
	return e.initial
}

// Wrap wraps err with a message, context, and optional key-value pairs.
// Returns nil if err is nil.
func Wrap(ctx context.Context, err error, message string, kvs ...any) error {
	if err == nil {
		return nil
	}
	return &wrapped{
		ctx:     ctx,
		initial: err,
		message: message,
		kvs:     normaliseKVs(kvs),
		trace:   Stacktrace(ignoreStacktraceLevels, ignoredPrefix),
	}
}

// New creates a new error with a message, context, and optional key-value pairs.
func New(ctx context.Context, message string, kvs ...any) error {
	return &wrapped{
		ctx:     ctx,
		message: message,
		kvs:     normaliseKVs(kvs),
		trace:   Stacktrace(ignoreStacktraceLevels, ignoredPrefix),
	}
}

// normaliseKVs ensures the key-value slice has an even length.
func normaliseKVs(kvs []any) []any {
	if len(kvs)%2 == 0 {
		return kvs
	}
	return append(kvs, "")
}

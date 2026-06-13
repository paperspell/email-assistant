package log

import "context"

// Logger describes the common interface for structured logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(err error, args ...any)
	Error(err error, args ...any)
	With(args ...any) Logger
	InfoEnabled() bool
	ErrorEnabled() bool
	WarnEnabled() bool
	DebugEnabled() bool
}

// Level names for configuration.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

type contextKey struct{}

// IntoContext injects logger into ctx.
func IntoContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext extracts the Logger from ctx, or returns Noop if none was set.
func FromContext(ctx context.Context) Logger {
	logger, ok := ctx.Value(contextKey{}).(Logger)
	if !ok {
		return Noop{}
	}
	return logger
}

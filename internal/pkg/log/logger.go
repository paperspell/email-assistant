package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/lmittmann/tint"

	"github.com/paperspell/email-assistant/internal/pkg/errx"
)

// LoggerConfig configures a Slogger.
type LoggerConfig struct {
	Dev    bool
	Level  string
	Output io.Writer
}

// Slogger is a Logger implementation backed by log/slog.
type Slogger struct {
	logger *slog.Logger
}

// NewLogger creates a new Slogger. In dev mode it uses a colorized tint handler;
// otherwise it uses a JSON handler suitable for production.
func NewLogger(c LoggerConfig, kvs ...any) *Slogger {
	if c.Output == nil {
		c.Output = os.Stdout
	}

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(c.Level)); err != nil {
		fmt.Println("log: unmarshal level:", err)
	}

	var handler slog.Handler
	if c.Dev {
		handler = tint.NewHandler(c.Output, &tint.Options{AddSource: true, Level: lvl})
	} else {
		handler = slog.NewJSONHandler(c.Output, &slog.HandlerOptions{AddSource: true, Level: lvl})
	}

	return &Slogger{
		logger: slog.New(handler).With(kvs...),
	}
}

// Debug logs at LevelDebug.
func (l *Slogger) Debug(msg string, args ...any) {
	if !l.logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	l.log(slog.LevelDebug, msg, args...)
}

// Info logs at LevelInfo.
func (l *Slogger) Info(msg string, args ...any) {
	if !l.logger.Enabled(context.Background(), slog.LevelInfo) {
		return
	}
	l.log(slog.LevelInfo, msg, args...)
}

// Warn logs at LevelWarn. Does nothing if err is nil.
func (l *Slogger) Warn(err error, args ...any) {
	if err == nil || !l.logger.Enabled(context.Background(), slog.LevelWarn) {
		return
	}
	if holder, ok := err.(errx.KeysValuesHolder); ok {
		args = append(args, holder.KeysValues()...)
	}
	l.log(slog.LevelWarn, err.Error(), args...)
}

// Error logs at LevelError. Does nothing if err is nil.
func (l *Slogger) Error(err error, args ...any) {
	if err == nil || !l.logger.Enabled(context.Background(), slog.LevelError) {
		return
	}
	args = append(args, argsFromErr(err)...)
	l.log(slog.LevelError, err.Error(), args...)
}

// With returns a new Logger that includes the given key-value attributes.
func (l *Slogger) With(args ...any) Logger {
	return &Slogger{logger: l.logger.With(args...)}
}

// InfoEnabled reports whether the logger records Info-level messages.
func (l *Slogger) InfoEnabled() bool {
	return l.logger.Enabled(context.Background(), slog.LevelInfo)
}

// ErrorEnabled reports whether the logger records Error-level messages.
func (l *Slogger) ErrorEnabled() bool {
	return l.logger.Enabled(context.Background(), slog.LevelError)
}

// WarnEnabled reports whether the logger records Warn-level messages.
func (l *Slogger) WarnEnabled() bool {
	return l.logger.Enabled(context.Background(), slog.LevelWarn)
}

// DebugEnabled reports whether the logger records Debug-level messages.
func (l *Slogger) DebugEnabled() bool {
	return l.logger.Enabled(context.Background(), slog.LevelDebug)
}

func (l *Slogger) log(level slog.Level, msg string, args ...any) {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	if err := l.logger.Handler().Handle(context.Background(), r); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
}

func argsFromErr(err error) (args []any) {
	if holder, ok := err.(errx.KeysValuesHolder); ok {
		args = append(args, holder.KeysValues()...)
	}
	if tracer, ok := err.(errx.TraceSource); ok {
		args = append(args, "stacktrace", formatTrace(tracer.Trace()))
	}
	return args
}

func formatTrace(traces []errx.TraceEntry) []any {
	ss := make([]any, 0, len(traces))
	for _, trace := range traces {
		s := trace.FuncName + " " + trace.File + ":" + strconv.Itoa(trace.Line)
		ss = append(ss, s)
	}
	return ss
}

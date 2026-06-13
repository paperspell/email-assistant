package errx

import (
	"runtime"
	"slices"
	"strings"
)

const (
	depth                  = 32
	ignoreStacktraceLevels = 3
	ignoredPrefix          = "runtime"
)

// TraceEntry represents a single frame in a stacktrace.
type TraceEntry struct {
	FuncName string
	File     string
	Line     int
}

// Stacktrace captures the current goroutine stacktrace, skipping ignoreLevels frames
// and omitting frames whose function name starts with any of ignorePrefixes.
func Stacktrace(ignoreLevels int, ignorePrefixes ...string) (stacktrace []TraceEntry) {
	var pc [depth]uintptr
	n := runtime.Callers(ignoreLevels, pc[:])

	for _, entry := range pc[:n] {
		fn := runtime.FuncForPC(entry)
		file, line := fn.FileLine(entry)
		if !slices.ContainsFunc(ignorePrefixes, func(prefix string) bool {
			return strings.HasPrefix(fn.Name(), prefix)
		}) {
			stacktrace = append(stacktrace, TraceEntry{
				FuncName: fn.Name(),
				File:     file,
				Line:     line,
			})
		}
	}

	return stacktrace
}

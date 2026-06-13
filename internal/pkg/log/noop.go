//nolint:revive
package log

// Noop is a Logger that discards all output. Safe for use in tests.
type Noop struct{}

func (Noop) Debug(string, ...any) {}
func (Noop) Info(string, ...any)  {}
func (Noop) Warn(error, ...any)   {}
func (Noop) Error(error, ...any)  {}
func (Noop) InfoEnabled() bool    { return false }
func (Noop) ErrorEnabled() bool   { return false }
func (Noop) WarnEnabled() bool    { return false }
func (Noop) DebugEnabled() bool   { return false }
func (n Noop) With(...any) Logger { return n }

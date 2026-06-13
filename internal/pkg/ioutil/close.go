package ioutil

import (
	"fmt"
	"io"
)

// Closer is an alias for io.Closer.
type Closer = io.Closer

// Logger is the minimal logging interface required for CloseWithLog.
type Logger interface {
	Error(err error, attrs ...any)
}

// CloseWithLog calls Close on the provided Closer and logs any error.
func CloseWithLog(closer Closer, logger Logger) {
	if closer != nil {
		if err := closer.Close(); err != nil {
			logger.Error(fmt.Errorf("close: %w", err))
		}
	}
}

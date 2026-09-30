// Package logger provides a Zap-backed implementation of port.Logger for consumer services.
package logger

import (
	"os"
	"sync"

	internallog "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
)

// NewLogger builds a port.Logger: development Zap config when env is "dev", production otherwise.
func NewLogger(env string) (port.Logger, error) {
	return internallog.NewLogger(env)
}

var (
	defaultOnce   sync.Once
	defaultLogger port.Logger
	defaultErr    error
)

// Default returns a process-wide logger for code paths that were never handed
// one — an early startup failure, a component constructed without a logger —
// so they still log through gincommon instead of a second logging stack. It
// is built once, for APP_ENV (then ENVIRONMENT; default "production"). It
// returns an error only if Zap itself could not be constructed.
func Default() (port.Logger, error) {
	defaultOnce.Do(func() {
		env := os.Getenv("APP_ENV")
		if env == "" {
			env = os.Getenv("ENVIRONMENT")
		}
		if env == "" {
			env = "production"
		}
		defaultLogger, defaultErr = NewLogger(env)
	})
	return defaultLogger, defaultErr
}

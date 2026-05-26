// Package logger provides a Zap-backed implementation of port.Logger for consumer services.
package logger

import (
	internallog "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
)

// NewLogger builds a port.Logger: development Zap config when env is "dev", production otherwise.
func NewLogger(env string) (port.Logger, error) {
	return internallog.NewLogger(env)
}

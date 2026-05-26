package logger

import (
	"sync"

	"go.uber.org/zap"
)

const (
	zapFieldPoolInitCap = 16
	zapFieldPoolMaxCap  = 64
)

// zapFieldPool reuses []zap.Field backing arrays when converting log fields.
var zapFieldPool = sync.Pool{
	New: func() any {
		s := make([]zap.Field, 0, zapFieldPoolInitCap)
		return &s
	},
}

package logger_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
)

func TestNewLogger_DevAndProd(t *testing.T) {
	dev, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("dev logger: %v", err)
	}
	dev.Debug("dev_debug", nil)
	dev.Info("dev_test", map[string]interface{}{"k": "v"})
	dev.Warn("dev_warn", nil)
	dev.Error("dev_error", nil)

	prod, err := logger.NewLogger("prod")
	if err != nil {
		t.Fatalf("prod logger: %v", err)
	}
	prod.Debug("prod_debug", nil)
	prod.Info("prod_test", map[string]interface{}{"n": 1})
	prod.Warn("prod_warn", nil)
	prod.Error("prod_error", nil)
}

func TestNewLogger_TypedFields(t *testing.T) {
	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	log.Info("http_request", map[string]interface{}{
		"trace_id":   "trace-1",
		"request_id": "req-1",
		"tenant_id":  "tenant-1",
		"user_id":    "user-1",
		"path":       "/api/v1/items",
		"status":     201,
		"duration":   "12ms",
		"ok":         true,
		"err":        errors.New("ignored in smoke"),
		"tags":       []string{"a", "b"},
	})
}

func TestZapLogger_ManyFields_TrimsPool(t *testing.T) {
	log, err := logger.NewLogger("prod")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	// More than zapFieldPoolMaxCap (64) fields forces releaseZapFields to shrink the pool slice.
	fields := make(map[string]interface{}, 65)
	for i := 0; i < 65; i++ {
		fields[fmt.Sprintf("k%d", i)] = i
	}
	log.Info("many-fields-first", fields)
	// Second call reuses the pool entry; if cap was reset, this exercises the shrink path again.
	log.Info("many-fields-second", fields)
}

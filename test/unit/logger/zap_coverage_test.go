package logger_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
)

// TestNewLogger_NoLogFile_UsesConfig exercises the newLoggerFromConfig path
// (LOG_FILE unset) for both dev and prod configurations.
func TestNewLogger_NoLogFile_UsesConfig(t *testing.T) {
	t.Setenv("LOG_FILE", "") // force the non-file branch

	dev, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("dev logger: %v", err)
	}
	dev.Info("dev_no_file", map[string]interface{}{"k": "v"})
	_ = dev.Sync() // stderr sync error is platform-dependent; exercise the path only

	prod, err := logger.NewLogger("prod")
	if err != nil {
		t.Fatalf("prod logger: %v", err)
	}
	prod.Info("prod_no_file", map[string]interface{}{"n": 1})
	_ = prod.Sync()
}

// TestNewLogger_BadLogFilePath_ReturnsError exercises the newFileCore error path:
// a LOG_FILE whose parent directory cannot be created (it lives under a regular file)
// makes os.MkdirAll fail, so NewLogger returns an error.
func TestNewLogger_BadLogFilePath_ReturnsError(t *testing.T) {
	// Create a regular file, then point LOG_FILE at a path *under* it.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	badPath := filepath.Join(blocker, "sub", "app.log") // parent is a file → MkdirAll fails
	t.Setenv("LOG_FILE", badPath)

	if _, err := logger.NewLogger("dev"); err == nil {
		t.Fatal("expected error for un-creatable LOG_FILE path, got nil")
	}
}

// errSyncer is a zapcore.WriteSyncer whose Sync always fails with a
// non-suppressed error, exercising the final `return err` branch of ZapLogger.Sync.
type errSyncer struct{}

func (errSyncer) Write(p []byte) (int, error) { return len(p), nil }
func (errSyncer) Sync() error                 { return errors.New("disk full") }

func TestSync_RealErrorPropagates(t *testing.T) {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		errSyncer{},
		zapcore.DebugLevel,
	)
	log := logger.NewLoggerWithCore(core)

	if err := log.Sync(); err == nil {
		t.Fatal("expected Sync to propagate a non-suppressed error, got nil")
	}
}

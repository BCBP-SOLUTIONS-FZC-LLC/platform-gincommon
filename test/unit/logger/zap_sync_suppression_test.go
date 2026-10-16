package logger_test

import (
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
)

// msgSyncer fails Sync with a fixed message.
type msgSyncer struct{ msg string }

func (msgSyncer) Write(p []byte) (int, error) { return len(p), nil }
func (s msgSyncer) Sync() error               { return errors.New(s.msg) }

// Syncing stderr fails with EBADF / ENOTTY on non-TTY outputs (CI, containers, tests).
// Those errors are not flush failures, so Sync (and gincommon.Shutdown) must not
// report them.
func TestSync_StderrSyncErrorsSuppressed(t *testing.T) {
	for _, msg := range []string{
		"sync /dev/stderr: bad file descriptor",
		"sync /dev/stderr: inappropriate ioctl for device",
	} {
		core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), msgSyncer{msg}, zapcore.InfoLevel)
		if err := logger.NewLoggerWithCore(core).Sync(); err != nil {
			t.Errorf("Sync with %q = %v, want nil", msg, err)
		}
	}
}

// A LOG_FILE that names an existing directory cannot be opened for append.
func TestNewLogger_LogFileIsDirectory_ReturnsError(t *testing.T) {
	t.Setenv("LOG_FILE", t.TempDir())
	if _, err := logger.NewLogger("prod"); err == nil {
		t.Fatal("expected an error when LOG_FILE is a directory")
	}
}

type okSyncer struct{}

func (okSyncer) Write(p []byte) (int, error) { return len(p), nil }
func (okSyncer) Sync() error                 { return nil }

func TestSync_Success(t *testing.T) {
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), okSyncer{}, zapcore.InfoLevel)
	if err := logger.NewLoggerWithCore(core).Sync(); err != nil {
		t.Fatalf("Sync = %v, want nil", err)
	}
}

package gincommon_test

import (
	"context"
	"errors"
	"testing"

	gootel "go.opentelemetry.io/otel"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

// syncLogger implements port.Logger plus an optional Sync() error used by Shutdown.
type syncLogger struct {
	synced  bool
	syncErr error
}

func (l *syncLogger) Debug(string, map[string]interface{}) {}
func (l *syncLogger) Info(string, map[string]interface{})  {}
func (l *syncLogger) Warn(string, map[string]interface{})  {}
func (l *syncLogger) Error(string, map[string]interface{}) {}
func (l *syncLogger) Sync() error {
	l.synced = true
	return l.syncErr
}

func TestShutdown_NilLogger_FlushesTracingOnly(t *testing.T) {
	gincommon.InitTracing("shutdown-nil-logger", "test", "")

	if err := gincommon.Shutdown(nil); err != nil {
		t.Fatalf("Shutdown(nil) = %v, want nil", err)
	}
	// After Shutdown the global provider stays installed but records nothing,
	// so a provider captured now is still live after a later Init.
	captured := gootel.GetTracerProvider()
	if _, span := captured.Tracer("t").Start(context.Background(), "after-shutdown"); span.IsRecording() {
		t.Fatal("span recording after Shutdown, want a non-recording no-op span")
	}
}

func TestShutdown_FlushesLoggerWhenSyncable(t *testing.T) {
	gincommon.InitTracing("shutdown-sync-logger", "test", "")

	log := &syncLogger{}
	if err := gincommon.Shutdown(log); err != nil {
		t.Fatalf("Shutdown(log) = %v, want nil", err)
	}
	if !log.synced {
		t.Fatal("Shutdown did not call logger.Sync()")
	}
}

func TestShutdown_PropagatesSyncError(t *testing.T) {
	gincommon.InitTracing("shutdown-sync-err", "test", "")

	wantErr := errors.New("sync failed")
	log := &syncLogger{syncErr: wantErr}
	if err := gincommon.Shutdown(log); !errors.Is(err, wantErr) {
		t.Fatalf("Shutdown error = %v, want %v", err, wantErr)
	}
}

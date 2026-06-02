package gincommon_test

import (
	"errors"
	"testing"

	gootel "go.opentelemetry.io/otel"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

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
	// After Shutdown the provider must be reset to the noop implementation.
	if _, ok := gootel.GetTracerProvider().(nooptrace.TracerProvider); !ok {
		t.Fatalf("tracer provider type = %T, want noop after Shutdown", gootel.GetTracerProvider())
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

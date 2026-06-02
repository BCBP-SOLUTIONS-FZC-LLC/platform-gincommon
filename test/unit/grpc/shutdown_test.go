package grpc_test

import (
	"errors"
	"testing"

	gootel "go.opentelemetry.io/otel"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	otelsetup "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
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
	otelsetup.Init("grpc-shutdown-nil", "test", "")

	if err := grpccommon.Shutdown(nil); err != nil {
		t.Fatalf("Shutdown(nil) = %v, want nil", err)
	}
	if _, ok := gootel.GetTracerProvider().(nooptrace.TracerProvider); !ok {
		t.Fatalf("tracer provider type = %T, want noop after Shutdown", gootel.GetTracerProvider())
	}
}

func TestShutdown_FlushesLoggerWhenSyncable(t *testing.T) {
	otelsetup.Init("grpc-shutdown-sync", "test", "")

	log := &syncLogger{}
	if err := grpccommon.Shutdown(log); err != nil {
		t.Fatalf("Shutdown(log) = %v, want nil", err)
	}
	if !log.synced {
		t.Fatal("Shutdown did not call logger.Sync()")
	}
}

func TestShutdown_PropagatesSyncError(t *testing.T) {
	otelsetup.Init("grpc-shutdown-err", "test", "")

	wantErr := errors.New("sync failed")
	log := &syncLogger{syncErr: wantErr}
	if err := grpccommon.Shutdown(log); !errors.Is(err, wantErr) {
		t.Fatalf("Shutdown error = %v, want %v", err, wantErr)
	}
}

package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

func TestInit_RegistersGRPCMetrics(t *testing.T) {
	if metrics.GrpcRequestsTotal == nil ||
		metrics.GrpcRequestDuration == nil ||
		metrics.GrpcRequestSizeBytes == nil ||
		metrics.GrpcResponseSizeBytes == nil ||
		metrics.GrpcInflight == nil ||
		metrics.GrpcRateLimitedRequestsTotal == nil ||
		metrics.GrpcPanicTotal == nil {
		t.Fatal("expected gRPC metric vectors to be initialized")
	}

	metrics.RecordGRPC("/pkg.Service/Method", 0.05, "OK", "2xx", "success", 128, 256)
}

func TestRecordGRPCRateLimited_NilSafe(t *testing.T) {
	saved := metrics.GrpcRateLimitedRequestsTotal
	metrics.GrpcRateLimitedRequestsTotal = nil
	defer func() { metrics.GrpcRateLimitedRequestsTotal = saved }()
	metrics.RecordGRPCRateLimited("/test.Service/Method") // must not panic when nil
}

func TestRecordGRPCPanic_NilSafe(t *testing.T) {
	saved := metrics.GrpcPanicTotal
	metrics.GrpcPanicTotal = nil
	defer func() { metrics.GrpcPanicTotal = saved }()
	metrics.RecordGRPCPanic("/test.Service/Method") // must not panic when nil
}

func TestRecordGRPC_NilSafe(t *testing.T) {
	saved := metrics.GrpcRequestsTotal
	metrics.GrpcRequestsTotal = nil
	defer func() { metrics.GrpcRequestsTotal = saved }()
	metrics.RecordGRPC("/test.Service/Method", 0.01, "OK", "2xx", "success", 0, 0) // must not panic when nil
}

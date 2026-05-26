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
		metrics.GrpcRateLimitedRequestsTotal == nil {
		t.Fatal("expected gRPC metric vectors to be initialized")
	}

	metrics.RecordGRPC("/pkg.Service/Method", 0.05, "OK", "2xx", "success", 128, 256)
}

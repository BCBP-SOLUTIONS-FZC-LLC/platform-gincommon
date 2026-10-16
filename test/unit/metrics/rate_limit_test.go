package metrics_test

import (
	"net/http"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordHTTPRateLimited(t *testing.T) {
	metrics.RecordHTTPRateLimited(http.MethodGet, "/api/v1/token")

	counter, err := metrics.RateLimitedRequestsTotal.GetMetricWithLabelValues(http.MethodGet, "/api/v1/token")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("rate_limited_requests_total = %v, want 1", got)
	}
}

func TestInit_RegistersRateLimitedCounter(t *testing.T) {
	if metrics.RateLimitedRequestsTotal == nil || metrics.GrpcRateLimitedRequestsTotal == nil {
		t.Fatal("expected rate limit counters to be initialized")
	}
}

func TestRecordHTTPRateLimited_NilSafe(t *testing.T) {
	saved := metrics.RateLimitedRequestsTotal
	metrics.RateLimitedRequestsTotal = nil
	defer func() { metrics.RateLimitedRequestsTotal = saved }()
	metrics.RecordHTTPRateLimited("GET", "/limited") // must not panic when counter is nil
}

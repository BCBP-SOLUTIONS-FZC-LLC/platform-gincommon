package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const (
	testServiceName  = "metrics-test"
	testBuildVersion = "1.0.0-test"
)

func TestInit_RegistersCollectors(t *testing.T) {
	if metrics.HttpRequestsTotal == nil ||
		metrics.HttpRequestDuration == nil ||
		metrics.HttpRequestSizeBytes == nil ||
		metrics.HttpResponseSizeBytes == nil ||
		metrics.HttpActiveRequests == nil ||
		metrics.HttpPanicTotal == nil ||
		metrics.RateLimitedRequestsTotal == nil ||
		metrics.BuildInfo == nil ||
		metrics.GrpcRequestsTotal == nil ||
		metrics.GrpcRequestDuration == nil ||
		metrics.GrpcRequestSizeBytes == nil ||
		metrics.GrpcResponseSizeBytes == nil ||
		metrics.GrpcInflight == nil ||
		metrics.GrpcRateLimitedRequestsTotal == nil {
		t.Fatal("expected metric vectors to be initialized")
	}

	// Init is once-only; a second call must not replace collectors.
	metrics.Init("metrics-test-again", "ignored")
	if metrics.HttpRequestsTotal == nil {
		t.Fatal("expected collectors to remain after second Init")
	}
}

func TestInit_ServiceConstLabelOnMetrics(t *testing.T) {
	metrics.HttpRequestsTotal.WithLabelValues("GET", "/health", "2xx", "success").Inc()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != "http_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			got := constLabelsMap(m)
			if got["service"] == testServiceName && got["version"] == testBuildVersion {
				return
			}
		}
	}
	t.Fatalf(`expected service=%q version=%q on http_requests_total`, testServiceName, testBuildVersion)
}

func constLabelsMap(m *dto.Metric) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string)
	for _, l := range m.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

func TestRequestBodySize(t *testing.T) {
	if got := metrics.RequestBodySize(512); got != 512 {
		t.Fatalf("got %v want 512", got)
	}
	if got := metrics.RequestBodySize(0); got != 0 {
		t.Fatalf("got %v want 0", got)
	}
	if got := metrics.RequestBodySize(-1); got != 0 {
		t.Fatalf("unknown length: got %v want 0", got)
	}
}

func TestHTTPPayloadSizeBuckets(t *testing.T) {
	b := metrics.HTTPPayloadSizeBuckets
	if len(b) < 8 {
		t.Fatalf("expected at least 8 buckets, got %d", len(b))
	}
	for i := 1; i < len(b); i++ {
		if b[i] <= b[i-1] {
			t.Fatalf("buckets must be strictly increasing: %v", b)
		}
	}
}

func TestInit_RegistersSizeHistograms(t *testing.T) {
	if metrics.HttpRequestSizeBytes == nil || metrics.HttpResponseSizeBytes == nil {
		t.Fatal("expected size histograms to be initialized")
	}

	metrics.HttpRequestSizeBytes.WithLabelValues("POST", "/upload", "2xx", "success").Observe(1024)
	metrics.HttpResponseSizeBytes.WithLabelValues("POST", "/upload", "2xx", "success").Observe(256)
}

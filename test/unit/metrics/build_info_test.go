package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBuildInfo_ExposesServiceAndVersion(t *testing.T) {
	if metrics.BuildInfo == nil {
		t.Fatal("BuildInfo not initialized")
	}

	if got := testutil.ToFloat64(metrics.BuildInfo.WithLabelValues(testBuildVersion)); got != 1 {
		t.Fatalf("platform_build_info = %v, want 1", got)
	}

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != "platform_build_info" {
			continue
		}
		if len(mf.GetMetric()) != 1 {
			t.Fatalf("metric count = %d, want 1", len(mf.GetMetric()))
		}
		labels := constLabelsMap(mf.GetMetric()[0])
		if labels["service"] != testServiceName || labels["version"] != testBuildVersion || labels["domain"] != "iam" {
			t.Fatalf("labels = %v, want service=%q version=%q", labels, testServiceName, testBuildVersion)
		}
		return
	}
	t.Fatal("platform_build_info not found in gather output")
}

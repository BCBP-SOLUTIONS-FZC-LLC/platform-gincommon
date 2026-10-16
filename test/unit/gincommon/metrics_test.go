package gincommon_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

func TestMetricsRegisterer_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("gincommon-metrics-registerer", "v1.0", reg)

	if got := gincommon.MetricsRegisterer(); got != reg {
		t.Fatalf("MetricsRegisterer() = %v, want %v", got, reg)
	}
}

func TestMetricsConstLabels_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("gincommon-metrics-labels", "v2.0", reg)

	got := gincommon.MetricsConstLabels()
	if got["service"] != "gincommon-metrics-labels" || got["domain"] != "iam" || got["environment"] != "test" || got["version"] != "" {
		t.Fatalf("MetricsConstLabels() = %v, want domain=iam environment=test service=gincommon-metrics-labels and no version", got)
	}
}

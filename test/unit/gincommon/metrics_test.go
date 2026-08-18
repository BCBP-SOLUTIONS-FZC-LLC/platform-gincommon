package gincommon_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestMetricsRegisterer_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("gincommon-metrics-registerer", "v1.0", reg)

	if got := gincommon.MetricsRegisterer(); got != reg {
		t.Fatalf("MetricsRegisterer() = %v, want %v", got, reg)
	}
}

func TestMetricsConstLabels_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("gincommon-metrics-labels", "v2.0", reg)

	got := gincommon.MetricsConstLabels()
	if got["service"] != "gincommon-metrics-labels" || got["version"] != "v2.0" {
		t.Fatalf("MetricsConstLabels() = %v, want service=gincommon-metrics-labels version=v2.0", got)
	}
}

package grpc_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

func TestMetricsRegisterer_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("grpccommon-metrics-registerer", "v1.0", reg)

	if got := grpccommon.MetricsRegisterer(); got != reg {
		t.Fatalf("MetricsRegisterer() = %v, want %v", got, reg)
	}
}

func TestMetricsConstLabels_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("grpccommon-metrics-labels", "v2.0", reg)

	got := grpccommon.MetricsConstLabels()
	if got["service"] != "grpccommon-metrics-labels" || got["domain"] != "iam" || got["environment"] != "test" || got["version"] != "" {
		t.Fatalf("MetricsConstLabels() = %v, want domain=iam environment=test service=grpccommon-metrics-labels and no version", got)
	}
}

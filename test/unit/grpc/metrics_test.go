package grpc_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
)

func TestMetricsRegisterer_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("grpccommon-metrics-registerer", "v1.0", reg)

	if got := grpccommon.MetricsRegisterer(); got != reg {
		t.Fatalf("MetricsRegisterer() = %v, want %v", got, reg)
	}
}

func TestMetricsConstLabels_DelegatesToInternalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("grpccommon-metrics-labels", "v2.0", reg)

	got := grpccommon.MetricsConstLabels()
	if got["service"] != "grpccommon-metrics-labels" || got["version"] != "v2.0" {
		t.Fatalf("MetricsConstLabels() = %v, want service=grpccommon-metrics-labels version=v2.0", got)
	}
}

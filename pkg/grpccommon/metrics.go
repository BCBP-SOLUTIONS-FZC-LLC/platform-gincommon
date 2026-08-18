package grpccommon

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

// MetricsRegisterer returns the prometheus.Registerer gincommon's own gRPC metrics are
// registered to. Register custom application collectors here (not
// prometheus.DefaultRegisterer directly) so they stay consistent with tests that use an
// isolated registry via metrics.InitWithRegisterer. Call after
// ObservabilityUnaryInterceptors/ObservabilityStreamInterceptors/Full* has run.
//
// Metrics initialization is shared process-wide with pkg/gincommon: whichever package's
// Config first triggers Init wins the registerer and const labels for the process.
func MetricsRegisterer() prometheus.Registerer {
	return metrics.Registerer()
}

// MetricsConstLabels returns a copy of the {service, version} const labels applied to
// gincommon's own metrics, so custom collectors can carry matching labels. Returns an
// empty map if metrics have not been initialized yet.
func MetricsConstLabels() prometheus.Labels {
	return metrics.ConstLabels()
}

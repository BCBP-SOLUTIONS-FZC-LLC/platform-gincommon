package gincommon

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

// MetricsRegisterer returns the prometheus.Registerer gincommon's own HTTP metrics are
// registered to. Register custom application collectors here (not
// prometheus.DefaultRegisterer directly) so they stay consistent with tests that use an
// isolated registry via metrics.InitWithRegisterer. Call after ObservabilityMiddlewares
// or DefaultMiddlewares has run.
func MetricsRegisterer() prometheus.Registerer {
	return metrics.Registerer()
}

// MetricsConstLabels returns a copy of the {service, version} const labels applied to
// gincommon's own metrics, so custom collectors can carry matching labels. Returns an
// empty map if ObservabilityMiddlewares/DefaultMiddlewares has not run yet.
func MetricsConstLabels() prometheus.Labels {
	return metrics.ConstLabels()
}

package fixtures

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

// Test identity used by InitMetrics / InitMetricsWithRegisterer.
const (
	TestDomain      = "iam"
	TestEnvironment = "test"
)

// InitMetrics initializes the process-wide metrics on the default registerer
// (first caller wins) with the test identity. It panics on an invalid service
// name so a broken fixture fails loudly.
func InitMetrics(service, version string) {
	if err := metrics.InitWithOptions(testOptions(service, version)); err != nil {
		panic(err)
	}
}

// InitMetricsWithRegisterer initializes metrics on an isolated registry with
// the test identity, replacing any previous collectors.
func InitMetricsWithRegisterer(service, version string, reg prometheus.Registerer) {
	if err := metrics.InitWithRegistererOptions(testOptions(service, version), reg); err != nil {
		panic(err)
	}
}

func testOptions(service, version string) metrics.Options {
	return metrics.Options{ServiceName: service, BuildVersion: version, Domain: TestDomain, Environment: TestEnvironment}
}

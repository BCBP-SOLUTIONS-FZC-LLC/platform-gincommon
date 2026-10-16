// Command metricslint enforces the Enterprise Platform Observability Standard
// in CI: it validates a service's /metrics scrape against the Platform
// Observability Registry and scans dashboards, alert and recording rules,
// SLOs and HPA manifests for legacy or unregistered metric names.
//
//	go run github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/cmd/metricslint@<version> check -url http://localhost:8080/metrics
package main

import (
	"os"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/obsregistry"
)

func main() {
	os.Exit(obsregistry.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

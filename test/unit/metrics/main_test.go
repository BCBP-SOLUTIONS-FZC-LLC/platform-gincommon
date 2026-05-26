package metrics_test

import (
	"os"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

func TestMain(m *testing.M) {
	metrics.Init("metrics-test", "1.0.0-test")
	os.Exit(m.Run())
}

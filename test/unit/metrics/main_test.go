package metrics_test

import (
	"os"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

func TestMain(m *testing.M) {
	fixtures.InitMetrics("metrics-test", "1.0.0-test")
	os.Exit(m.Run())
}

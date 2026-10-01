// Package uninit_test runs in its own process, before anything initializes
// metrics, to pin the behaviour of the metrics API when the identity labels
// are not known yet.
package uninit_test

import (
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestConstructorsFailBeforeInit(t *testing.T) {
	if metrics.Initialized() {
		t.Fatal("this package must run before any metrics initialization")
	}
	for name, build := range map[string]func() error{
		"NewCounterVec":       func() error { _, err := gincommon.NewCounterVec(nil, "iam_x_y_total", "h"); return err },
		"NewCounter":          func() error { _, err := gincommon.NewCounter(nil, "iam_x_y_total", "h"); return err },
		"NewGaugeVec":         func() error { _, err := gincommon.NewGaugeVec(nil, "iam_x_y", "h"); return err },
		"NewGauge":            func() error { _, err := gincommon.NewGauge(nil, "iam_x_y", "h"); return err },
		"NewHistogramVec":     func() error { _, err := gincommon.NewHistogramVec(nil, "iam_x_y_seconds", "h", nil); return err },
		"NewSharedCounterVec": func() error { _, err := gincommon.NewSharedCounterVec(nil, "platform_retry_total"); return err },
	} {
		if err := build(); err == nil || !strings.Contains(err.Error(), "not initialized") {
			t.Errorf("%s before init: err = %v, want a not-initialized error", name, err)
		}
	}
	if d, s := metrics.Identity(); d != "" || s != "" {
		t.Errorf("Identity before init = %q, %q", d, s)
	}
	if len(metrics.ConstLabels()) != 0 {
		t.Error("ConstLabels before init must be empty")
	}
	// Recorders are no-ops before init.
	metrics.RecordHTTP("GET", "/", "2xx", "success", 0, 0, 0)
	metrics.RecordGRPC("/a.B/C", 0, "OK", "2xx", "success", 0, 0)
	metrics.RecordHTTPPanic("GET", "/")
	metrics.RecordHTTPRateLimited("GET", "/")
	metrics.RecordHTTPTimeout("GET", "/")
	metrics.RecordGRPCPanic("/a.B/C")
	metrics.RecordGRPCRateLimited("/a.B/C")
	metrics.GRPCInflightInc("/a.B/C")
	metrics.GRPCInflightDec("/a.B/C")
	if metrics.HTTPEnabled() || metrics.GRPCEnabled() {
		t.Error("nothing is enabled before init")
	}
}

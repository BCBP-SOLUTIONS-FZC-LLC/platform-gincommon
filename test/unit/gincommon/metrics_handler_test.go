package gincommon_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
)

// brokenCollector is an unchecked collector whose Collect fails.
type brokenCollector struct{}

func (brokenCollector) Describe(chan<- *prometheus.Desc) {}

func (brokenCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.NewInvalidMetric(prometheus.NewDesc("iam_broken_x", "h", nil, nil), errors.New("collect failed"))
}

func TestMetricsHandler_ContinuesOnCollectorError(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-handler-err", "v1", reg)
	reg.MustRegister(brokenCollector{})

	rec := httptest.NewRecorder()
	gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the metrics that were gathered", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `platform_build_info{domain="iam",environment="test",service="svc-handler-err",version="v1"} 1`) {
		t.Errorf("gincommon's own metrics must still be served:\n%s", rec.Body.String())
	}
}

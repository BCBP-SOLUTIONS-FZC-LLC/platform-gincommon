// Package metricshandler_test checks MetricsHandler's error counter on an
// isolated registry and that the scrape stays compliant with the
// observability standard.
package metricshandler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/obsregistry"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

// brokenCollector is an unchecked collector whose Collect fails until fixed.
type brokenCollector struct{ fixed atomic.Bool }

func (*brokenCollector) Describe(chan<- *prometheus.Desc) {}

func (b *brokenCollector) Collect(ch chan<- prometheus.Metric) {
	if !b.fixed.Load() {
		ch <- prometheus.NewInvalidMetric(prometheus.NewDesc("iam_broken_x", "h", nil, nil), errors.New("collect failed"))
	}
}

func TestMetricsHandler_ExposesErrorCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-handler-cnt", "v1", reg)
	broken := &brokenCollector{}
	reg.MustRegister(broken)

	scrape := func() string {
		rec := httptest.NewRecorder()
		// A new handler per scrape: promhttp must reuse the registered counter.
		gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		return rec.Body.String()
	}
	scrape()
	scrape()
	body := scrape()
	if !strings.Contains(body, `promhttp_metric_handler_errors_total{cause="gathering"} 2`) {
		t.Errorf("want the gather-error counter exposed and counting every failed scrape:\n%s", body)
	}

	// promhttp_* is exempt from the standard: the counter adds no findings.
	broken.fixed.Store(true)
	rep, err := obsregistry.Validate(reg, obsregistry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Errors() {
		t.Error(f)
	}
}

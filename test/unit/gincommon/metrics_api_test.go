package gincommon_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestMetricConstructors_RegisterWithConstLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("svc-metrics-api", "v9", reg)

	cv, err := gincommon.NewCounterVec(nil, "app_writes_total", "writes", "result")
	if err != nil {
		t.Fatal(err)
	}
	cv.WithLabelValues("ok").Inc()
	c, err := gincommon.NewCounter(nil, "app_fails_total", "fails")
	if err != nil {
		t.Fatal(err)
	}
	c.Inc()
	gv, err := gincommon.NewGaugeVec(nil, "app_ratio", "ratio", "key")
	if err != nil {
		t.Fatal(err)
	}
	gv.WithLabelValues("k").Set(0.5)
	g, err := gincommon.NewGauge(nil, "app_up", "up")
	if err != nil {
		t.Fatal(err)
	}
	g.Set(1)

	rec := httptest.NewRecorder()
	gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body := rec.Body.String()
	for _, want := range []string{
		`app_writes_total{result="ok",service="svc-metrics-api",version="v9"} 1`,
		`app_fails_total{service="svc-metrics-api",version="v9"} 1`,
		`app_ratio{key="k",service="svc-metrics-api",version="v9"} 0.5`,
		`app_up{service="svc-metrics-api",version="v9"} 1`,
		`build_info{service="svc-metrics-api",version="v9"} 1`, // gincommon's own collectors on the same registry
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q", want)
		}
	}
}

func TestMetricConstructors_ExplicitRegistererAndDuplicates(t *testing.T) {
	metrics.InitWithRegisterer("svc-dup", "v1", prometheus.NewRegistry())
	own := prometheus.NewRegistry()
	if _, err := gincommon.NewCounterVec(own, "dup_total", "d", "l"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewCounterVec(own, "dup_total", "d", "l"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
	if _, err := gincommon.NewCounter(own, "dup_c", "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewCounter(own, "dup_c", "d"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
	if _, err := gincommon.NewGaugeVec(own, "dup_gv", "d", "l"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewGaugeVec(own, "dup_gv", "d", "l"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
	if _, err := gincommon.NewGauge(own, "dup_g", "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewGauge(own, "dup_g", "d"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
}

// nonGatherer is a Registerer that cannot be scraped.
type nonGatherer struct{ prometheus.Registerer }

func TestMetricsHandler_FallsBackToDefaultGatherer(t *testing.T) {
	metrics.InitWithRegisterer("svc-nongatherer", "v1", nonGatherer{prometheus.NewRegistry()})
	rec := httptest.NewRecorder()
	gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

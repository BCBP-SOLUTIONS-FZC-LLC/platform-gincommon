package gincommon_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

func TestMetricConstructors_RegisterWithConstLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-metrics-api", "v9", reg)

	cv, err := gincommon.NewCounterVec(nil, "iam_svc_metrics_api_writes_total", "writes", "result")
	if err != nil {
		t.Fatal(err)
	}
	cv.WithLabelValues("ok").Inc()
	c, err := gincommon.NewCounter(nil, "iam_svc_metrics_api_fails_total", "fails")
	if err != nil {
		t.Fatal(err)
	}
	c.Inc()
	gv, err := gincommon.NewGaugeVec(nil, "iam_svc_metrics_api_ratio", "ratio", "key")
	if err != nil {
		t.Fatal(err)
	}
	gv.WithLabelValues("k").Set(0.5)
	g, err := gincommon.NewGauge(nil, "iam_svc_metrics_api_up", "up")
	if err != nil {
		t.Fatal(err)
	}
	g.Set(1)

	rec := httptest.NewRecorder()
	gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body := rec.Body.String()
	for _, want := range []string{
		`iam_svc_metrics_api_writes_total{domain="iam",environment="test",result="ok",service="svc-metrics-api"} 1`,
		`iam_svc_metrics_api_fails_total{domain="iam",environment="test",service="svc-metrics-api"} 1`,
		`iam_svc_metrics_api_ratio{domain="iam",environment="test",key="k",service="svc-metrics-api"} 0.5`,
		`iam_svc_metrics_api_up{domain="iam",environment="test",service="svc-metrics-api"} 1`,
		`platform_build_info{domain="iam",environment="test",service="svc-metrics-api",version="v9"} 1`, // gincommon's own collectors on the same registry
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q", want)
		}
	}
}

func TestMetricConstructors_ExplicitRegistererAndDuplicates(t *testing.T) {
	fixtures.InitMetricsWithRegisterer("svc-dup", "v1", prometheus.NewRegistry())
	own := prometheus.NewRegistry()
	if _, err := gincommon.NewCounterVec(own, "iam_svc_dup_total", "d", "l"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewCounterVec(own, "iam_svc_dup_total", "d", "l"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
	if _, err := gincommon.NewCounter(own, "iam_svc_dup_c_total", "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewCounter(own, "iam_svc_dup_c_total", "d"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
	if _, err := gincommon.NewGaugeVec(own, "iam_svc_dup_gv", "d", "l"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewGaugeVec(own, "iam_svc_dup_gv", "d", "l"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
	if _, err := gincommon.NewGauge(own, "iam_svc_dup_g", "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewGauge(own, "iam_svc_dup_g", "d"); err == nil {
		t.Fatal("expected a duplicate-registration error")
	}
}

// nonGatherer is a Registerer that cannot be scraped.
type nonGatherer struct{ prometheus.Registerer }

func TestMetricsHandler_FallsBackToDefaultGatherer(t *testing.T) {
	fixtures.InitMetricsWithRegisterer("svc-nongatherer", "v1", nonGatherer{prometheus.NewRegistry()})
	rec := httptest.NewRecorder()
	gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

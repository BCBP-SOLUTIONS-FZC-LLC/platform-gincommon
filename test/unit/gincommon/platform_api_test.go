package gincommon_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/obsregistry"
)

const iamLabels = `domain="iam",environment="prod",service="event-consumer"`

func initIAM(t *testing.T) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	err := metrics.InitWithRegistererOptions(metrics.Options{
		ServiceName: "event-consumer", BuildVersion: "v1", Domain: "iam", Environment: "prod",
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func scrapeBody(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	gincommon.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	return rec.Body.String()
}

func TestSharedConstructors_UseRegistryContract(t *testing.T) {
	initIAM(t)
	processed, err := gincommon.NewSharedCounterVec(nil, "platform_messages_processed_total")
	if err != nil {
		t.Fatal(err)
	}
	processed.WithLabelValues("iam.events", "user.created").Inc()
	auth, err := gincommon.NewSharedCounterVec(nil, "iam_auth_events_total")
	if err != nil {
		t.Fatal(err)
	}
	auth.WithLabelValues("login", "success").Inc()
	depth, err := gincommon.NewSharedGaugeVec(nil, "platform_queue_depth")
	if err != nil {
		t.Fatal(err)
	}
	depth.WithLabelValues("iam.events").Set(3)
	dep, err := gincommon.NewSharedHistogramVec(nil, "platform_dependency_request_seconds")
	if err != nil {
		t.Fatal(err)
	}
	dep.WithLabelValues("postgres", "query", "success").Observe(0.004)

	body := scrapeBody(t)
	for _, want := range []string{
		`platform_messages_processed_total{domain="iam",environment="prod",event_type="user.created",queue="iam.events",service="event-consumer"} 1`,
		`iam_auth_events_total{domain="iam",environment="prod",event_type="login",outcome="success",service="event-consumer"} 1`,
		`platform_queue_depth{domain="iam",environment="prod",queue="iam.events",service="event-consumer"} 3`,
		`platform_dependency_request_seconds_bucket{dependency="postgres",domain="iam",environment="prod",operation="query",outcome="success",service="event-consumer",le="0.005"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %s", want)
		}
	}

	rep, err := obsregistry.Validate(gincommon.MetricsGatherer(), obsregistry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Errors() {
		t.Error(f)
	}
}

func TestSharedConstructors_Errors(t *testing.T) {
	initIAM(t)
	for _, tc := range []struct {
		build func() error
		want  string
	}{
		{func() error { _, err := gincommon.NewSharedCounterVec(nil, "platform_queue_depth"); return err }, "is a gauge"},
		{func() error { _, err := gincommon.NewSharedGaugeVec(nil, "platform_retry_total"); return err }, "is a counter"},
		{func() error { _, err := gincommon.NewSharedHistogramVec(nil, "platform_retry_total"); return err }, "is a counter"},
		{func() error { _, err := gincommon.NewSharedCounterVec(nil, "platform_made_up_total"); return err }, "not in the Platform Observability Registry"},
	} {
		if err := tc.build(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
	// Duplicate registration surfaces the registerer's error.
	if _, err := gincommon.NewSharedGaugeVec(nil, "platform_dlq_depth"); err != nil {
		t.Fatal(err)
	}
	if g, err := gincommon.NewSharedGaugeVec(nil, "platform_dlq_depth"); err == nil || g != nil {
		t.Errorf("duplicate: %v, %v", g, err)
	}
	if _, err := gincommon.NewSharedCounterVec(nil, "platform_retry_total"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewSharedCounterVec(nil, "platform_retry_total"); err == nil {
		t.Error("want duplicate-registration error")
	}
	if _, err := gincommon.NewSharedHistogramVec(nil, "platform_event_propagation_seconds"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewSharedHistogramVec(nil, "platform_event_propagation_seconds"); err == nil {
		t.Error("want duplicate-registration error")
	}
}

func TestCustomConstructors_EnforceServiceNamespace(t *testing.T) {
	initIAM(t)
	if _, err := gincommon.NewCounterVec(nil, "iam_event_consumer_hmac_failures_total", "HMAC failures", "reason"); err != nil {
		t.Fatal(err)
	}
	if _, err := gincommon.NewGauge(nil, "iam_event_consumer_cache_entries", "entries"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		build func() error
		want  string
	}{
		{func() error { _, err := gincommon.NewCounterVec(nil, "app_writes_total", "w"); return err }, "iam_event_consumer_<metric>"},
		{func() error { _, err := gincommon.NewCounter(nil, "platform_retry_total", "r"); return err }, "NewShared"},
		{func() error { _, err := gincommon.NewGaugeVec(nil, "iam_event_consumer_x_total", "g"); return err }, "gauges must not"},
		{func() error { _, err := gincommon.NewGauge(nil, "Bad", "g"); return err }, "snake_case"},
		{func() error {
			_, err := gincommon.NewCounterVec(nil, "iam_event_consumer_logins_total", "l", "user_id")
			return err
		}, "prohibited"},
		{func() error {
			_, err := gincommon.NewHistogramVec(nil, "iam_event_consumer_lookup_ms", "l", nil)
			return err
		}, "_seconds"},
	} {
		if err := tc.build(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}

func TestNewHistogramVec(t *testing.T) {
	reg := initIAM(t)
	h, err := gincommon.NewHistogramVec(nil, "iam_event_consumer_lookup_seconds", "lookup latency", nil, "cache")
	if err != nil {
		t.Fatal(err)
	}
	h.WithLabelValues("hit").Observe(0.0015)
	custom, err := gincommon.NewHistogramVec(nil, "iam_event_consumer_batch_seconds", "batch", []float64{1, 10})
	if err != nil {
		t.Fatal(err)
	}
	custom.WithLabelValues().Observe(3)
	if _, err := gincommon.NewHistogramVec(nil, "iam_event_consumer_batch_seconds", "batch", nil); err == nil {
		t.Error("want duplicate-registration error")
	}

	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	buckets := map[string]int{}
	for _, f := range fams {
		if f.GetType() == dto.MetricType_HISTOGRAM {
			buckets[f.GetName()] = len(f.GetMetric()[0].GetHistogram().GetBucket())
		}
	}
	if want := len(metrics.RequestDurationBuckets); buckets["iam_event_consumer_lookup_seconds"] != want {
		t.Errorf("default buckets = %d, want request_duration preset (%d)", buckets["iam_event_consumer_lookup_seconds"], want)
	}
	if buckets["iam_event_consumer_batch_seconds"] != 2 {
		t.Errorf("custom buckets = %d", buckets["iam_event_consumer_batch_seconds"])
	}
}

func TestLabeledRegisterer_AddsStandardLabels(t *testing.T) {
	initIAM(t)
	lib := prometheus.NewCounter(prometheus.CounterOpts{Name: "iam_event_consumer_lib_total", Help: "l"})
	if err := gincommon.LabeledRegisterer(nil).Register(lib); err != nil {
		t.Fatal(err)
	}
	lib.Inc()
	if want := `iam_event_consumer_lib_total{` + iamLabels + `} 1`; !strings.Contains(scrapeBody(t), want) {
		t.Errorf("scrape missing %s", want)
	}
}

func TestObservabilityMiddlewares_PanicsOnInvalidIdentity(t *testing.T) {
	t.Setenv("OBSERVABILITY_DOMAIN", "") // the missing-domain case must not be rescued by testenv's default
	for _, cfg := range []gincommon.Config{
		{ServiceName: "event-consumer", Domain: "hr", Environment: "prod"},
		{ServiceName: "event-consumer", Domain: "iam", Environment: "qa"},
		{ServiceName: "event-consumer", Domain: "", Environment: "prod"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ObservabilityMiddlewares(%+v): want panic", cfg)
				}
			}()
			gincommon.ObservabilityMiddlewares(cfg)
		}()
	}
}

func TestObsregistryFacade(t *testing.T) {
	r := obsregistry.Default()
	if _, ok := r.Lookup("platform_messages_processed_total"); !ok {
		t.Fatal("registry lookup failed")
	}
	if _, err := obsregistry.Parse([]byte("{}")); err == nil {
		t.Error("Parse({}): want error")
	}
	if got := obsregistry.ServicePrefix("iam", "event-consumer"); got != "iam_event_consumer_" {
		t.Errorf("ServicePrefix = %q", got)
	}
	rep, err := obsregistry.ValidateText(strings.NewReader("# TYPE orders_total counter\norders_total 1\n"), obsregistry.Options{})
	if err != nil || rep.OK() {
		t.Errorf("ValidateText: %v, %v", rep, err)
	}
	failing := prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) { return nil, errors.New("gather failed") })
	if _, err := obsregistry.Validate(failing, obsregistry.Options{}); err == nil {
		t.Error("want gather error")
	}
}

// LabeledRegisterer resolves the identity labels when a collector registers,
// so a registerer built before metrics initialization still gets them.
func TestLabeledRegisterer_CreatedBeforeInit(t *testing.T) {
	early := gincommon.LabeledRegisterer(nil)
	initIAM(t)
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "iam_event_consumer_early_total", Help: "e"})
	early.MustRegister(c)
	c.Inc()
	if want := `iam_event_consumer_early_total{` + iamLabels + `} 1`; !strings.Contains(scrapeBody(t), want) {
		t.Errorf("scrape missing %s", want)
	}
	if !early.Unregister(c) {
		t.Error("Unregister through the labeled registerer must find the collector")
	}
}

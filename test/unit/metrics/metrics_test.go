package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const (
	testServiceName  = "metrics-test"
	testBuildVersion = "1.0.0-test"
)

func TestInit_RegistersCollectors(t *testing.T) {
	if metrics.HttpRequestsTotal == nil ||
		metrics.HttpRequestDuration == nil ||
		metrics.HttpRequestSizeBytes == nil ||
		metrics.HttpResponseSizeBytes == nil ||
		metrics.HttpActiveRequests == nil ||
		metrics.HttpPanicTotal == nil ||
		metrics.RateLimitedRequestsTotal == nil ||
		metrics.HttpRequestTimeoutTotal == nil ||
		metrics.BuildInfo == nil ||
		metrics.GrpcRequestsTotal == nil ||
		metrics.GrpcRequestDuration == nil ||
		metrics.GrpcRequestSizeBytes == nil ||
		metrics.GrpcResponseSizeBytes == nil ||
		metrics.GrpcInflight == nil ||
		metrics.GrpcRateLimitedRequestsTotal == nil {
		t.Fatal("expected metric vectors to be initialized")
	}

	// Init is once-only; a second call must not replace collectors.
	metrics.Init("metrics-test-again", "ignored")
	if metrics.HttpRequestsTotal == nil {
		t.Fatal("expected collectors to remain after second Init")
	}
}

func TestInit_ServiceConstLabelOnMetrics(t *testing.T) {
	metrics.HttpRequestsTotal.WithLabelValues("GET", "/health", "2xx", "success").Inc()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != "http_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			got := constLabelsMap(m)
			if got["service"] == testServiceName && got["version"] == testBuildVersion {
				return
			}
		}
	}
	t.Fatalf(`expected service=%q version=%q on http_requests_total`, testServiceName, testBuildVersion)
}

func constLabelsMap(m *dto.Metric) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string)
	for _, l := range m.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

func TestInitWithRegisterer_RegistersCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("svc-registerer", "v2.0", reg)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(mfs) == 0 {
		t.Fatal("expected metrics registered on custom registerer")
	}
}

func TestInitWithRegisterer_EmptyBuildVersion_UsesUnknown(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("svc-empty-ver", "", reg)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(mfs) == 0 {
		t.Fatal("expected metrics registered")
	}
}

func TestRegisterer_ReflectsMostRecentInitWithRegisterer(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("svc-registerer-accessor", "v3.0", reg)

	if got := metrics.Registerer(); got != reg {
		t.Fatalf("Registerer() = %v, want the registerer passed to InitWithRegisterer", got)
	}
}

func TestConstLabels_ReflectsMostRecentInitWithRegisterer(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.InitWithRegisterer("svc-labels-accessor", "v4.0", reg)

	got := metrics.ConstLabels()
	if got["service"] != "svc-labels-accessor" || got["version"] != "v4.0" {
		t.Fatalf("ConstLabels() = %v, want service=svc-labels-accessor version=v4.0", got)
	}

	// Mutating the returned map must not affect the package's internal state.
	got["service"] = "mutated"
	if again := metrics.ConstLabels(); again["service"] != "svc-labels-accessor" {
		t.Fatalf("ConstLabels() leaked internal state: got %v", again)
	}
}

// ─── Coverage gap tests ────────────────────────────────────────────────────────

func TestRecordHTTPTimeout_NilSafe(t *testing.T) {
	saved := metrics.HttpRequestTimeoutTotal
	metrics.HttpRequestTimeoutTotal = nil
	defer func() { metrics.HttpRequestTimeoutTotal = saved }()
	metrics.RecordHTTPTimeout("GET", "/slow") // must not panic when counter is nil
}

// ProtoMessageSize: typed-nil proto.Message must return 0, not panic.
func TestProtoMessageSize_TypedNilReturnsZero(t *testing.T) {
	// A typed nil satisfies the proto.Message interface but the concrete pointer is nil.
	// Without the reflect guard in ProtoMessageSize, proto.Size would panic.
	var m *struct{ _ int } // not a proto.Message, returns 0 via !ok branch
	if got := metrics.ProtoMessageSize(m); got != 0 {
		t.Fatalf("non-proto nil = %v, want 0", got)
	}
	if got := metrics.ProtoMessageSize(nil); got != 0 {
		t.Fatalf("nil = %v, want 0", got)
	}
}

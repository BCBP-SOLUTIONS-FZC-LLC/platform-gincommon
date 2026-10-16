// Package uninit_test runs in its own process, before anything initializes
// metrics, to pin the behaviour of the metrics API when the identity labels
// are not known yet.
package uninit_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
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

// TestFailedInitRollsBackAndRetries runs after TestConstructorsFailBeforeInit
// (source order) while nothing is initialized: a registration failure on the
// default registerer must leave nothing registered and metrics uninitialized,
// and a corrected call must then succeed.
func TestFailedInitRollsBackAndRetries(t *testing.T) {
	if metrics.Registerer() != prometheus.DefaultRegisterer {
		t.Error("Registerer before init must be the default registerer")
	}
	opts := metrics.Options{ServiceName: "uninit-svc", BuildVersion: "v1.0.0", Domain: "iam", Environment: "test"}
	// An identical collector (AlreadyRegisteredError) under the name
	// initialization registers last. Identical, because a registry remembers a
	// name's help and labels even after Unregister.
	conflict := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "platform_grpc_panics_total", Help: "gRPC handler panics recovered by the interceptor stack.",
		ConstLabels: prometheus.Labels{"domain": "iam", "environment": "test", "service": "uninit-svc"},
	}, []string{"grpc_method"})
	prometheus.MustRegister(conflict)
	err := metrics.InitWithOptions(opts)
	if err == nil || !strings.Contains(err.Error(), "register collectors") {
		t.Fatalf("InitWithOptions with a conflicting collector = %v, want a registration error", err)
	}
	if metrics.Initialized() || metrics.BuildInfo != nil {
		t.Fatal("a failed init must not publish collectors")
	}
	fams, _ := prometheus.DefaultGatherer.Gather()
	for _, f := range fams {
		if strings.HasPrefix(f.GetName(), "platform_") && f.GetName() != "platform_grpc_panics_total" {
			t.Errorf("%s left registered after a failed init", f.GetName())
		}
	}
	prometheus.Unregister(conflict)

	if err := metrics.InitWithOptions(opts); err != nil {
		t.Fatalf("retry after fixing the conflict: %v", err)
	}
	if !metrics.Initialized() {
		t.Fatal("want initialized after retry")
	}
	// Same identity again: no-op. Different identity: ErrIdentityMismatch naming the field.
	if err := metrics.InitWithOptions(opts); err != nil {
		t.Fatalf("same identity: %v", err)
	}
	for field, o := range map[string]metrics.Options{
		"service":     {ServiceName: "other", BuildVersion: "v1.0.0", Domain: "iam", Environment: "test"},
		"domain":      {ServiceName: "uninit-svc", BuildVersion: "v1.0.0", Domain: "billing", Environment: "test"},
		"environment": {ServiceName: "uninit-svc", BuildVersion: "v1.0.0", Domain: "iam", Environment: "prod"},
		"version":     {ServiceName: "uninit-svc", BuildVersion: "v2.0.0", Domain: "iam", Environment: "test"},
	} {
		err := metrics.InitWithOptions(o)
		if !errors.Is(err, metrics.ErrIdentityMismatch) || !strings.Contains(err.Error(), field) {
			t.Errorf("%s mismatch: err = %v", field, err)
		}
	}
	// A later call without a version ("unknown") inherits the established one,
	// and a version that sanitises to the established one is the same identity.
	for _, v := range []string{"", "unknown", " refs/tags/v1.0.0 "} {
		o := opts
		o.BuildVersion = v
		if err := metrics.InitWithOptions(o); err != nil {
			t.Errorf("BuildVersion %q after v1.0.0: %v, want inherited (no mismatch)", v, err)
		}
	}
	func() {
		defer func() {
			if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "already initialized") {
				t.Errorf("MustInitFromConfig with a different identity: recovered %v", r)
			}
		}()
		metrics.MustInitFromConfig(metrics.Options{ServiceName: "other", BuildVersion: "v1.0.0", Domain: "iam", Environment: "test"})
	}()
	// The testing escape hatch keeps the first identity silently.
	restore := metrics.AllowIdentityChangeForTesting(true)
	if err := metrics.InitWithOptions(metrics.Options{ServiceName: "other", Domain: "iam", Environment: "test"}); err != nil {
		t.Errorf("with AllowIdentityChangeForTesting: %v", err)
	}
	restore()
	if _, s := metrics.Identity(); s != "uninit-svc" {
		t.Errorf("Identity service = %q, want the first identity", s)
	}
}

// TestUnknownFirstThenRealVersionMismatches pins the one-directional version
// leniency: once "unknown" is established, a later real version is still a
// mismatch (platform_build_info would otherwise silently keep "unknown").
func TestUnknownFirstThenRealVersionUpgrades(t *testing.T) {
	unknown := metrics.Options{ServiceName: "uninit-unknown", Domain: "iam", Environment: "test"}
	reg := prometheus.NewRegistry()
	if err := metrics.InitWithRegistererOptions(unknown, reg); err != nil {
		t.Fatal(err)
	}
	if err := metrics.InitWithOptions(unknown); err != nil {
		t.Errorf("unknown after unknown: %v", err)
	}
	versioned := unknown
	versioned.BuildVersion = "v1.4.0"
	if err := metrics.InitWithOptions(versioned); err != nil {
		t.Fatalf("real version after unknown: err = %v, want an upgrade (nil)", err)
	}
	if v := testutil.ToFloat64(metrics.BuildInfo.WithLabelValues("v1.4.0")); v != 1 {
		t.Errorf("platform_build_info{version=v1.4.0} = %v, want 1", v)
	}
	if n := testutil.CollectAndCount(metrics.BuildInfo); n != 1 {
		t.Errorf("platform_build_info series = %d, want 1 (unknown series dropped)", n)
	}
	// Once a real version is established, a different real version mismatches.
	other := unknown
	other.BuildVersion = "v2.0.0"
	if err := metrics.InitWithOptions(other); !errors.Is(err, metrics.ErrIdentityMismatch) || !strings.Contains(err.Error(), "version") {
		t.Errorf("second real version: err = %v, want a version ErrIdentityMismatch", err)
	}
	if err := metrics.InitWithOptions(unknown); err != nil {
		t.Errorf("unknown after real version: %v", err)
	}
}

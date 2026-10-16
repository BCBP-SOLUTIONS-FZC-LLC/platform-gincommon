// Package identity_test pins the one-identity-per-process contract through the
// public constructors. It deliberately does NOT import test/testenv (which
// enables metrics.AllowIdentityChangeForTesting for every other test
// package), so the real ErrIdentityMismatch panic is exercised; it sets the
// identity environment itself.
package identity_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
)

// recovered runs fn and returns the recovered panic value (nil if none).
func recovered(fn func()) (r any) {
	defer func() { r = recover() }()
	fn()
	return nil
}

func TestOneIdentityPerProcess(t *testing.T) {
	t.Setenv("OBSERVABILITY_DOMAIN", "iam")
	t.Setenv("ENVIRONMENT", "test")
	t.Setenv("APP_ENV", "")
	t.Setenv("BUILD_VERSION", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	// Metrics identity is process-wide. Under -count>1 a previous iteration has
	// already established this test's own identity, which the assertions below
	// tolerate; any other identity means another test initialized metrics first.
	if metrics.Initialized() {
		if d, s := metrics.Identity(); d != "iam" || s != "identity-svc" {
			t.Fatalf("metrics already initialized as %s/%s; this package must run before any other metrics initialization", d, s)
		}
	}

	first := gincommon.Config{ServiceName: "identity-svc", BuildVersion: "1.4.0"}
	if r := recovered(func() { gincommon.ObservabilityMiddlewares(first) }); r != nil {
		t.Fatalf("first stack: %v", r)
	}

	// Same identity over gRPC, with no BuildVersion and BUILD_VERSION unset
	// ("unknown"): inherits 1.4.0, no panic.
	if r := recovered(func() { grpccommon.FullUnaryInterceptors(grpccommon.Config{ServiceName: "identity-svc"}) }); r != nil {
		t.Errorf("same identity, empty version: %v", r)
	}
	if r := recovered(func() { gincommon.ObservabilityMiddlewares(first) }); r != nil {
		t.Errorf("same identity again: %v", r)
	}

	// A different service name in the same process panics with ErrIdentityMismatch.
	r := recovered(func() {
		grpccommon.FullUnaryInterceptors(grpccommon.Config{ServiceName: "other-svc", BuildVersion: "1.4.0"})
	})
	if r == nil || !strings.Contains(fmt.Sprint(r), metrics.ErrIdentityMismatch.Error()) || !strings.Contains(fmt.Sprint(r), "service") {
		t.Errorf("different service: recovered %v, want an ErrIdentityMismatch panic naming the service", r)
	}
	// A different real version is still a mismatch.
	r = recovered(func() {
		gincommon.ObservabilityMiddlewares(gincommon.Config{ServiceName: "identity-svc", BuildVersion: "2.0.0"})
	})
	if r == nil || !strings.Contains(fmt.Sprint(r), metrics.ErrIdentityMismatch.Error()) {
		t.Errorf("different version: recovered %v, want an ErrIdentityMismatch panic", r)
	}
	if _, s := metrics.Identity(); s != "identity-svc" {
		t.Errorf("Identity service = %q, want the first identity kept", s)
	}
}

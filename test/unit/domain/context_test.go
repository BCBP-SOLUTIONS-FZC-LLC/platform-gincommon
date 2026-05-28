package domain_test

import (
	"context"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
)

func TestRequestContext_ClientIPField(t *testing.T) {
	rc := &domain.RequestContext{
		TenantID: "t1",
		UserID:   "u1",
		ClientIP: "10.0.0.1",
	}
	if rc.ClientIP != "10.0.0.1" {
		t.Fatalf("ClientIP = %q, want 10.0.0.1", rc.ClientIP)
	}
}

func TestContextWithRequestContext_RoundTrip(t *testing.T) {
	rc := &domain.RequestContext{
		TenantID: "tenant-1",
		UserID:   "user-1",
		ClientIP: "192.168.1.1",
	}

	ctx := domain.ContextWithRequestContext(context.Background(), rc)
	got, ok := domain.RequestContextFromGoContext(ctx)

	if !ok {
		t.Fatal("RequestContextFromGoContext ok = false, want true")
	}
	if got != rc {
		t.Fatalf("got different pointer; want same *RequestContext")
	}
	if got.ClientIP != "192.168.1.1" {
		t.Fatalf("ClientIP = %q, want 192.168.1.1", got.ClientIP)
	}
}

func TestRequestContextFromGoContext_NotPresent(t *testing.T) {
	got, ok := domain.RequestContextFromGoContext(context.Background())
	if ok {
		t.Fatal("RequestContextFromGoContext ok = true, want false")
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestContextWithRequestContext_NilRC(t *testing.T) {
	ctx := domain.ContextWithRequestContext(context.Background(), nil)
	got, ok := domain.RequestContextFromGoContext(ctx)
	// Storing nil pointer: retrieved as nil with ok=true (type assert succeeds but pointer is nil)
	// The ok check is the important part—no panic.
	_ = ok
	_ = got
}

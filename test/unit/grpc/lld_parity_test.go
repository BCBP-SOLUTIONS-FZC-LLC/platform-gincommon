package grpc_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/grpccommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
)

// The gRPC authz audit entries carry tenant_id, matching the HTTP AuthZMiddleware.
func TestAuthZ_DenialLogIncludesTenantID(t *testing.T) {
	log := &fixtures.MockLogger{}
	ic := grpcmw.UnaryAuthZInterceptor(grpccommon.NewDemoRBAC(), "write", "items", log)
	ctx := domain.ContextWithRequestContext(context.Background(),
		&domain.RequestContext{TenantID: "tenant-1", UserID: "user-1", Roles: []string{"user"}})

	_, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/svc.A/Write"}, okUnary)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("err = %v, want PermissionDenied", err)
	}
	if log.Msg != "grpc authz: access denied" || log.Fields["tenant_id"] != "tenant-1" {
		t.Fatalf("log = %q %v, want access denied with tenant_id", log.Msg, log.Fields)
	}
}

// A panic value recorded on the gRPC span is sanitized, matching the HTTP tracing middleware.
func TestTracing_PanicValueSanitizedOnSpan(t *testing.T) {
	sr := newRecorder(t)
	ic := grpcmw.UnaryTracingInterceptor("sanitize")
	func() {
		defer func() { _ = recover() }()
		_, _ = ic(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc.A/P"},
			func(context.Context, any) (any, error) { panic("line1\nforged log line" + strings.Repeat("x", 600)) })
	}()
	s := sr.Ended()[0]
	var msg string
	for _, ev := range s.Events() {
		for _, a := range ev.Attributes {
			if a.Key == "exception.message" {
				msg = a.Value.AsString()
			}
		}
	}
	if msg == "" || strings.Contains(msg, "\n") || len(msg) > 256 {
		t.Fatalf("exception.message not sanitized: %q (len %d)", msg, len(msg))
	}
}

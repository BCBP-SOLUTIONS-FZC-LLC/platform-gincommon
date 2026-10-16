package grpc_test

import (
	"context"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRequirePermission_Allowed(t *testing.T) {
	auth := service.NewRBACService()
	ic := grpccommon.RequirePermission("read", "doc", auth)

	rc := &domain.RequestContext{UserID: "u1", TenantID: "t1", Roles: []string{"user"}}
	ctx := domain.ContextWithRequestContext(context.Background(), rc)

	_, err := ic(
		ctx, &emptypb.Empty{},
		&grpc.UnaryServerInfo{},
		func(ctx context.Context, req any) (any, error) { return &emptypb.Empty{}, nil },
	)

	if err != nil {
		t.Fatalf("expected OK, got err = %v", err)
	}
}

func TestRequirePermission_Forbidden(t *testing.T) {
	auth := service.NewRBACService()
	ic := grpccommon.RequirePermission("delete", "doc", auth)

	rc := &domain.RequestContext{UserID: "u1", TenantID: "t1", Roles: []string{"user"}}
	ctx := domain.ContextWithRequestContext(context.Background(), rc)

	_, err := ic(
		ctx, &emptypb.Empty{},
		&grpc.UnaryServerInfo{},
		func(ctx context.Context, req any) (any, error) { return &emptypb.Empty{}, nil },
	)

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
}

func TestRequirePermissionWithLogger_LogsOnForbidden(t *testing.T) {
	auth := service.NewRBACService()
	log := &fixtures.MockLogger{}
	ic := grpccommon.RequirePermissionWithLogger("delete", "doc", auth, log)

	rc := &domain.RequestContext{UserID: "u1", TenantID: "t1", Roles: []string{"user"}}
	ctx := domain.ContextWithRequestContext(context.Background(), rc)

	_, err := ic(
		ctx, &emptypb.Empty{},
		&grpc.UnaryServerInfo{},
		func(ctx context.Context, req any) (any, error) { return &emptypb.Empty{}, nil },
	)

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
	if log.Level != "warn" {
		t.Fatalf("log level = %q, want warn for denied", log.Level)
	}
}

func TestNewDemoRBAC_NotNil(t *testing.T) {
	if grpccommon.NewDemoRBAC() == nil {
		t.Fatal("grpccommon.NewDemoRBAC() returned nil")
	}
}

func TestObservabilityStreamInterceptors_PanicsOnEmptyServiceName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty ServiceName")
		}
	}()
	grpccommon.ObservabilityStreamInterceptors(grpccommon.Config{})
}

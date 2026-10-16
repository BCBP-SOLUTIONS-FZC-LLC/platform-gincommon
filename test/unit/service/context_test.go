package service_test

import (
	"errors"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
)

func TestContextService_BuildContext(t *testing.T) {
	svc := service.NewContextService()

	tests := []struct {
		name    string
		tid     string
		uid     string
		wantErr error
	}{
		{name: "ok", tid: "tenant-1", uid: "user-1", wantErr: nil},
		{name: "missing tenant", tid: "", uid: "user-1", wantErr: domain.ErrMissingTenant},
		{name: "missing user", tid: "tenant-1", uid: "", wantErr: domain.ErrMissingUser},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := svc.BuildContext(tt.tid, tt.uid, []string{"admin"}, "trace-1", "")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("BuildContext() err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildContext() err = %v", err)
			}
			if ctx.TenantID != tt.tid || ctx.UserID != tt.uid || ctx.TraceID != "trace-1" {
				t.Fatalf("BuildContext() = %+v", ctx)
			}
		})
	}
}

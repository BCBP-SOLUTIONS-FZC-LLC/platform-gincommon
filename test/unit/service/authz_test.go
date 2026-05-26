package service_test

import (
	"errors"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
)

func TestRBACService_Authorize(t *testing.T) {
	svc := service.NewRBACService()

	tests := []struct {
		name     string
		roles    []string
		action   string
		resource string
		wantErr  error
	}{
		{name: "read allowed for user", roles: []string{"user"}, action: "read", resource: "doc", wantErr: nil},
		{name: "delete forbidden for user", roles: []string{"user"}, action: "delete", resource: "doc", wantErr: domain.ErrForbidden},
		{name: "delete allowed for admin", roles: []string{"admin"}, action: "delete", resource: "doc", wantErr: nil},
		{name: "delete allowed with admin among roles", roles: []string{"user", "admin"}, action: "delete", resource: "doc", wantErr: nil},
		{name: "unknown action denied", roles: []string{"admin"}, action: "patch", resource: "doc", wantErr: domain.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Authorize("user-1", tt.roles, tt.action, tt.resource)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("Authorize() err = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authorize() err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

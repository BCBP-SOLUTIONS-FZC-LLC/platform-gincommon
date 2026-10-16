package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
)

func TestRBACService_Authorize_NilRequestContextDenied(t *testing.T) {
	err := service.NewRBACService().Authorize(context.Background(), nil, "read", "doc")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden for nil rc", err)
	}
}

package domain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
)

func manyRoles(n int) string {
	roles := make([]string, n)
	for i := range roles {
		roles[i] = fmt.Sprintf("role%d", i)
	}
	return strings.Join(roles, ",")
}

func TestParseRolesStrict_AtLimitsAccepted(t *testing.T) {
	roles, err := domain.ParseRolesStrict(manyRoles(domain.MaxRoles))
	if err != nil || len(roles) != domain.MaxRoles {
		t.Fatalf("got %d roles, err %v; want %d, nil", len(roles), err, domain.MaxRoles)
	}
	// Duplicates and invalid tokens do not count towards MaxRoles.
	header := manyRoles(domain.MaxRoles) + ",role0,bad role,!!"
	if roles, err := domain.ParseRolesStrict(header); err != nil || len(roles) != domain.MaxRoles {
		t.Fatalf("dups/invalid counted: %d roles, err %v", len(roles), err)
	}
}

func TestParseRolesStrict_TooManyRoles(t *testing.T) {
	if _, err := domain.ParseRolesStrict(manyRoles(domain.MaxRoles + 1)); !errors.Is(err, domain.ErrInvalidRoles) {
		t.Fatalf("err = %v, want ErrInvalidRoles", err)
	}
}

func TestParseRolesStrict_HeaderTooLong(t *testing.T) {
	header := "admin," + strings.Repeat(",", domain.MaxRolesHeaderLen)
	if _, err := domain.ParseRolesStrict(header); !errors.Is(err, domain.ErrInvalidRoles) {
		t.Fatalf("err = %v, want ErrInvalidRoles", err)
	}
}

func TestParseRolesStrict_Empty(t *testing.T) {
	roles, err := domain.ParseRolesStrict("")
	if roles != nil || err != nil {
		t.Fatalf("got %v, %v; want nil, nil", roles, err)
	}
}

// ParseRoles fails closed (no roles) on over-limit input; gRPC uses it.
func TestParseRoles_OverLimitFailsClosed(t *testing.T) {
	if got := domain.ParseRoles(manyRoles(domain.MaxRoles + 1)); got != nil {
		t.Fatalf("ParseRoles = %v, want nil on too many roles", got)
	}
	if got := domain.ParseRoles(strings.Repeat("a", domain.MaxRolesHeaderLen+1)); got != nil {
		t.Fatalf("ParseRoles = %v, want nil on oversized header", got)
	}
	if got := domain.ParseRoles("admin,user"); len(got) != 2 {
		t.Fatalf("ParseRoles = %v", got)
	}
}

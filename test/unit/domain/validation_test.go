package domain_test

import (
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
)

func TestNormalizeHeaderValue(t *testing.T) {
	if _, ok := domain.NormalizeHeaderValue("  user-1  "); !ok {
		t.Fatal("expected valid trimmed value")
	}
	if _, ok := domain.NormalizeHeaderValue(""); ok {
		t.Fatal("expected empty invalid")
	}
	if _, ok := domain.NormalizeHeaderValue("bad\nvalue"); ok {
		t.Fatal("expected control char invalid")
	}
	if _, ok := domain.NormalizeHeaderValue(strings.Repeat("a", domain.MaxHeaderValueLen+1)); ok {
		t.Fatal("expected overlong invalid")
	}
}

func TestParseRoles(t *testing.T) {
	roles := domain.ParseRoles(" admin, user ,admin, bad role ")
	if len(roles) != 2 {
		t.Fatalf("roles = %v, want [admin user]", roles)
	}
	if got := domain.ParseRoles("!!!"); len(got) != 0 {
		t.Fatalf("invalid roles = %v", got)
	}
}

func TestNormalizeRequestID(t *testing.T) {
	if _, ok := domain.NormalizeRequestID("req-123"); !ok {
		t.Fatal("expected valid request id")
	}
	if _, ok := domain.NormalizeRequestID("bad id"); ok {
		t.Fatal("expected space invalid")
	}
}

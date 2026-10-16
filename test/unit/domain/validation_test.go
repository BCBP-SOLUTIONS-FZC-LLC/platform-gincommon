package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
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
	// Pin count AND order: first occurrence wins, input order preserved.
	want := []string{"admin", "user"}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	if got := domain.ParseRoles("!!!"); len(got) != 0 {
		t.Fatalf("invalid roles = %v", got)
	}
	// Empty/nil input.
	if got := domain.ParseRoles(""); got != nil {
		t.Fatalf("empty ParseRoles = %v, want nil", got)
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

package label_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
)

// registryGRPCMethodPattern mirrors the grpc_method label pattern in
// internal/obsregistry/registry.json; every emitted value must satisfy it.
var registryGRPCMethodPattern = regexp.MustCompile(`^(unknown|/?[A-Za-z0-9_.]+/[A-Za-z0-9_]+)$`)

func TestGRPCMethod_PatternAndLengthBound(t *testing.T) {
	cases := map[string]string{
		"/pkg.v1.Service/Method":    "/pkg.v1.Service/Method",
		"/svc/M_2":                  "/svc/M_2",
		"":                          label.Unknown,
		"pkg.Service/Method":        label.Unknown, // no leading slash
		"//pkg.Service/Method":      label.Unknown,
		"/pkg.Service/Method/extra": label.Unknown,
		"/pkg-Service/Method":       label.Unknown,
		"/pkg.Service/Me thod":      label.Unknown,
		"/pkg.Service/Method\n":     label.Unknown,
		"/" + strings.Repeat("a", 250) + "/Method": label.Unknown, // > 256 bytes
	}
	for in, want := range cases {
		got := label.GRPCMethod(in)
		if got != want {
			t.Errorf("GRPCMethod(%q) = %q, want %q", in, got, want)
		}
		if !registryGRPCMethodPattern.MatchString(got) {
			t.Errorf("GRPCMethod(%q) = %q violates the registry pattern", in, got)
		}
	}
}

func TestGRPCMethod_AllowList(t *testing.T) {
	t.Cleanup(label.ResetGRPCMethods)
	label.RegisterGRPCMethods("/pkg.Svc/A")
	label.RegisterGRPCMethods("/pkg.Svc/B") // additive
	for in, want := range map[string]string{
		"/pkg.Svc/A": "/pkg.Svc/A",
		"/pkg.Svc/B": "/pkg.Svc/B",
		"/pkg.Svc/C": label.Unknown,
	} {
		if got := label.GRPCMethod(in); got != want {
			t.Errorf("GRPCMethod(%q) = %q, want %q", in, got, want)
		}
	}
	label.ResetGRPCMethods()
	if got := label.GRPCMethod("/pkg.Svc/C"); got != "/pkg.Svc/C" {
		t.Fatalf("after reset = %q, want pattern-only mode", got)
	}
}

// An empty registration (e.g. RegisterKnownMethods on a server with no services yet)
// must not switch to allow-list mode and map every method to Unknown.
func TestRegisterGRPCMethods_EmptyKeepsPatternMode(t *testing.T) {
	label.ResetGRPCMethods()
	t.Cleanup(label.ResetGRPCMethods)
	label.RegisterGRPCMethods()
	if got := label.GRPCMethod("/pkg.Service/Method"); got != "/pkg.Service/Method" {
		t.Fatalf("GRPCMethod after empty registration = %q, want the method", got)
	}
}

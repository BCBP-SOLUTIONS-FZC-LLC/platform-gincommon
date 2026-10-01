package platformmetrics_test

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/obsregistry"
)

func TestSanitizeVersion(t *testing.T) {
	long := strings.Repeat("a", 70)
	for in, want := range map[string]string{
		"v1.4.0":                 "v1.4.0",
		"unknown":                "unknown",
		"1.2.3-rc.1+build.5":     "1.2.3-rc.1+build.5",
		" v1.0.0\n":              "v1.0.0",
		"feat/observability-api": "feat_observability-api",
		"refs/tags/v1.4.0":       "v1.4.0",
		"refs/heads/main":        "main",
		"registry:5000/img:v1":   "registry_5000_img_v1",
		"1.0 (dirty)":            "1.0__dirty_",
		"-v1":                    "v1",
		"/._v2":                  "v2",
		"v1.0-é":                 "v1.0-_",
		long:                     long[:64],
		"///":                    "",
		"refs/tags/":             "",
		"":                       "",
	} {
		got := metrics.SanitizeVersion(in)
		if got != want {
			t.Errorf("SanitizeVersion(%q) = %q, want %q", in, got, want)
		}
		if got != "" {
			if err := obsregistry.Default().CheckVersion(got); err != nil {
				t.Errorf("SanitizeVersion(%q) = %q fails the registry pattern: %v", in, got, err)
			}
		}
	}
}

// TestBranchStyleVersionInitializes: BUILD_VERSION values CI commonly sets
// (branch names, refs, image references) initialize with a sanitised label
// and a single stderr warning per original value instead of failing startup.
func TestBranchStyleVersionInitializes(t *testing.T) {
	out := captureStderr(t, func() {
		for range 2 {
			o := iamOpts
			o.BuildVersion = "refs/heads/feat/observability-api:x"
			reg := initPlatform(t, o)
			if s := scrape(t, reg); !strings.Contains(s, `version="feat_observability-api_x"`) {
				t.Errorf("platform_build_info must carry the sanitised version:\n%s", s)
			}
		}
	})
	if n := strings.Count(out, "is not a valid version label"); n != 1 {
		t.Errorf("want exactly one warning, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, `"refs/heads/feat/observability-api:x"`) || !strings.Contains(out, `"feat_observability-api_x"`) {
		t.Errorf("warning must name the original and sanitised value:\n%s", out)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	_ = w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

// TestSanitizedVersionWarningsBounded: the once-per-value memory is capped, so
// many distinct invalid versions cannot grow it (or stderr) without bound.
func TestSanitizedVersionWarningsBounded(t *testing.T) {
	out := captureStderr(t, func() {
		for i := range 100 {
			o := iamOpts
			o.BuildVersion = fmt.Sprintf("bounded/%d", i)
			initPlatform(t, o)
		}
	})
	if n := strings.Count(out, "is not a valid version label"); n == 0 || n > 64 {
		t.Errorf("warnings = %d, want between 1 and 64", n)
	}
}

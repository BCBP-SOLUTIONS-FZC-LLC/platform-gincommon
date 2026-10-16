package obsregistry_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/obsregistry"
)

func TestScanReferences_SizeCap(t *testing.T) {
	restore := obsregistry.SetMaxReferenceFileBytesForTesting(64)
	defer restore()
	r := obsregistry.Default()

	// At the cap: scanned normally.
	exact := "expr: rate(http_requests_total[5m])" + strings.Repeat(" ", 64-36) + "\n"
	if len(exact) != 64 {
		t.Fatalf("fixture is %d bytes", len(exact))
	}
	if refs, err := r.ScanReferences("ok", strings.NewReader(exact)); err != nil || len(refs) != 1 {
		t.Errorf("at cap: refs=%v err=%v", refs, err)
	}
	// Over the cap with no newline at all: an error, not an unbounded read;
	// references from lines before the cap are still returned.
	huge := "expr: rate(http_requests_total[5m])\n" + strings.Repeat("a", 1000)
	refs, err := r.ScanReferences("big", strings.NewReader(huge))
	if !errors.Is(err, obsregistry.ErrReferenceFileTooLarge) {
		t.Fatalf("over cap: err = %v, want ErrReferenceFileTooLarge", err)
	}
	if len(refs) != 1 {
		t.Errorf("refs before the cap must be kept: %v", refs)
	}
}

// TestRun_RefsIOErrorExitCode: an oversized file inside a walked directory
// and a missing root are error findings; every finding is printed and the
// exit code is 2 (I/O error), not 1.
func TestRun_RefsIOErrorExitCode(t *testing.T) {
	restore := obsregistry.SetMaxReferenceFileBytesForTesting(128)
	defer restore()
	dir := t.TempDir()
	writeFile(t, dir, "big.json", strings.Repeat("x", 200))
	writeFile(t, dir, "y.yml", "expr: rate(http_requests_total[5m])\n")
	missing := filepath.Join(dir, "missing")

	code, stdout, stderr := run(t, "", "refs", dir, missing)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	for _, want := range []string{"big.json:0: error: cannot read", "exceeds the reference scan size limit", "missing:0: error: cannot read", "y.yml:1: error"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "2 path(s) could not be read") {
		t.Errorf("stderr = %q", stderr)
	}
	// Findings only (no I/O error) stay exit 1.
	if code, _, _ := run(t, "", "refs", filepath.Join(dir, "y.yml")); code != 1 {
		t.Errorf("findings only: code %d, want 1", code)
	}
}

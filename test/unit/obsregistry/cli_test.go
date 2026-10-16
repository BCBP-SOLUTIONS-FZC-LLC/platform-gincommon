package obsregistry_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/obsregistry"
)

func run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := obsregistry.Run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodScrape = "# TYPE platform_retry_total counter\nplatform_retry_total{" + ids + `,operation="consume",event_type="user.created"} 1` + "\n"

func TestRun_Usage(t *testing.T) {
	if code, _, stderr := run(t, ""); code != 2 || !strings.Contains(stderr, "Usage") {
		t.Errorf("no args: code %d, %q", code, stderr)
	}
	if code, stdout, _ := run(t, "", "help"); code != 0 || !strings.Contains(stdout, "Usage") {
		t.Errorf("help: code %d", code)
	}
	if code, _, stderr := run(t, "", "bogus"); code != 2 || !strings.Contains(stderr, "unknown command") {
		t.Errorf("bogus: code %d, %q", code, stderr)
	}
	if code, _, _ := run(t, "", "check", "-nope"); code != 2 {
		t.Errorf("bad flag: code %d", code)
	}
}

func TestRun_CheckStdinFilesAndURL(t *testing.T) {
	if code, stdout, _ := run(t, goodScrape, "check"); code != 0 || !strings.Contains(stdout, "0 errors") {
		t.Errorf("stdin: code %d, %s", code, stdout)
	}
	dir := t.TempDir()
	good := writeFile(t, dir, "good.txt", goodScrape)
	bad := writeFile(t, dir, "bad.txt", "# TYPE orders_total counter\norders_total 1\n")
	if code, stdout, _ := run(t, "", "check", good, bad); code != 1 || !strings.Contains(stdout, "unclassified") {
		t.Errorf("files: code %d, %s", code, stdout)
	}
	if code, _, stderr := run(t, "", "check", filepath.Join(dir, "missing.txt")); code != 2 || stderr == "" {
		t.Errorf("missing file: code %d", code)
	}
	if code, _, _ := run(t, "{", "check"); code != 2 {
		t.Errorf("unparsable: code %d", code)
	}
	proposed := "# TYPE platform_queue_depth gauge\nplatform_queue_depth{" + ids + `,queue="q"} 1` + "\n"
	if code, _, _ := run(t, proposed, "check", "-strict"); code != 1 {
		t.Errorf("strict: code %d", code)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(goodScrape))
	}))
	defer srv.Close()
	if code, stdout, _ := run(t, "", "check", "-url", srv.URL+"/metrics"); code != 0 || !strings.Contains(stdout, "1 metrics checked") {
		t.Errorf("url: code %d, %s", code, stdout)
	}
	if code, _, stderr := run(t, "", "check", "-url", srv.URL+"/nope"); code != 2 || !strings.Contains(stderr, "404") {
		t.Errorf("url 404: code %d, %s", code, stderr)
	}
	if code, _, _ := run(t, "", "check", "-url", "http://127.0.0.1:1/metrics"); code != 2 {
		t.Errorf("url refused: code %d", code)
	}
	if code, _, _ := run(t, "", "check", "-url", "://bad"); code != 2 {
		t.Errorf("bad url: code %d", code)
	}
}

func TestRun_CustomRegistry(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "registry.json", string(obsregistry.Embedded()))
	bad := writeFile(t, dir, "bad.json", `{"schema_version": 2}`)
	for _, cmd := range []string{"check", "refs", "inventory"} {
		if code, _, _ := run(t, goodScrape, cmd, "-registry", bad, dir); code != 2 {
			t.Errorf("%s with bad registry: code %d", cmd, code)
		}
	}
	if code, stdout, _ := run(t, "", "registry", "-registry", good); code != 0 || !strings.Contains(stdout, "registry OK") {
		t.Errorf("registry good: code %d %s", code, stdout)
	}
	if code, _, stderr := run(t, "", "registry", "-registry", bad); code != 1 || !strings.Contains(stderr, "schema_version") {
		t.Errorf("registry bad: code %d %s", code, stderr)
	}
	// A missing file is an I/O error (2), unlike an invalid document (1).
	if code, _, _ := run(t, "", "registry", "-registry", filepath.Join(dir, "missing.json")); code != 2 {
		t.Errorf("registry missing: code %d", code)
	}
}

func TestRun_Refs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dash/board.json", `{"expr": "sum(rate(http_requests_total[5m]))"}`)
	writeFile(t, dir, "alerts/a.yml", "expr: histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))\n")
	writeFile(t, dir, "notes.md", "http_requests_total is skipped by extension\n")
	// Removed legacy names are errors.
	if code, stdout, _ := run(t, "", "refs", dir); code != 1 || !strings.Contains(stdout, "2 references: 2 errors, 0 warnings") ||
		!strings.Contains(stdout, "was removed and is no longer emitted; use platform_http_request_duration_seconds_bucket") {
		t.Errorf("refs removed: code %d\n%s", code, stdout)
	}
	// Deprecated ones (compatibility period) warn, unless -deny-deprecated.
	reg := writeFile(t, t.TempDir(), "registry.json", string(deprecatedRegistry(t)))
	board := filepath.Join(dir, "dash", "board.json")
	if code, stdout, _ := run(t, "", "refs", "-registry", reg, board); code != 0 || !strings.Contains(stdout, "1 references: 0 errors, 1 warnings") {
		t.Errorf("refs deprecated: code %d\n%s", code, stdout)
	}
	if code, _, _ := run(t, "", "refs", "-registry", reg, "-deny-deprecated", board); code != 1 {
		t.Errorf("deny-deprecated: code %d", code)
	}
	single := writeFile(t, dir, "hpa.yaml", "metricName: platform_unknown_total\n")
	if code, stdout, _ := run(t, "", "refs", single); code != 1 || !strings.Contains(stdout, "not in the Platform Observability Registry") {
		t.Errorf("unknown platform ref: code %d %s", code, stdout)
	}
	if code, _, stderr := run(t, "", "refs"); code != 2 || !strings.Contains(stderr, "path is required") {
		t.Errorf("no path: code %d", code)
	}
	if code, _, _ := run(t, "", "refs", filepath.Join(dir, "missing")); code != 2 {
		t.Errorf("missing path: code %d", code)
	}
}

func TestRun_RefsUnreadable(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "x.yml", "a\n")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p, 0o644) //nolint:errcheck
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("file still readable (running as root)")
	}
	// Inside a walked directory: an error finding, the walk continues, and
	// the exit code is 2 (I/O error) after every finding is printed.
	writeFile(t, dir, "y.yml", "expr: rate(http_requests_total[5m])\n")
	if code, stdout, _ := run(t, "", "refs", dir); code != 2 || !strings.Contains(stdout, "x.yml:0: error: cannot read") ||
		!strings.Contains(stdout, "y.yml:1: error") {
		t.Errorf("unreadable in dir: code %d\n%s", code, stdout)
	}
	if code, _, _ := run(t, "", "refs", p); code != 2 {
		t.Errorf("unreadable file: code %d", code)
	}
}

func TestRun_Inventory(t *testing.T) {
	code, stdout, _ := run(t, "", "inventory")
	if code != 0 || !strings.Contains(stdout, "| `platform_messages_processed_total` |") {
		t.Fatalf("inventory: code %d", code)
	}
	out := filepath.Join(t.TempDir(), "inv.md")
	if code, _, _ := run(t, "", "inventory", "-o", out); code != 0 {
		t.Fatalf("inventory -o: code %d", code)
	}
	if data, _ := os.ReadFile(out); string(data) != stdout {
		t.Error("inventory -o differs from stdout")
	}
}

// TestInventory_DocsUpToDate keeps the committed observability inventory in
// sync with the registry (regenerate with make metrics-inventory).
func TestInventory_DocsUpToDate(t *testing.T) {
	want, err := os.ReadFile("../../../docs/observability/metric-registry.md")
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := obsregistry.Default().WriteInventory(&got); err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Fatal("docs/observability/metric-registry.md is stale: run make metrics-inventory")
	}
	if err := obsregistry.Default().WriteInventory(failWriter{}); err == nil {
		t.Error("want write error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestScanReferences(t *testing.T) {
	r := obsregistry.Default()
	text := `sum(rate(http_requests_total[5m])) + rate(http_requests_total[1m])
service:platform_http_requests:rate5m platform_* prose platform_events platform_messages_received/processed
platform_http_requests_total platform_http_request_duration_seconds_count iam_event_consumer_x_total
platform_build_info_count
`
	refs, err := r.ScanReferences("f", strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("want 2 references (deduped per line, histogram suffix only for histograms), got %v", refs)
	}
	if refs[0].Line != 1 || refs[0].Replacement != "platform_http_requests_total" || !strings.HasPrefix(refs[0].String(), "f:1: error:") {
		t.Errorf("ref 0 = %+v", refs[0])
	}
	if refs[1].Metric != "platform_build_info_count" || refs[1].Severity != obsregistry.SeverityError {
		t.Errorf("ref 1 = %+v (platform_build_info is a gauge, so _count is not one of its series)", refs[1])
	}
	if _, err := r.ScanReferences("f", errReader{}); err == nil {
		t.Error("want read error")
	}
}

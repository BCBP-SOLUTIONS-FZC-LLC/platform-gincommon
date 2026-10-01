package obsregistry_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/obsregistry"
	pkgobs "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/obsregistry"
)

func TestRun_CheckRejectsURLWithFiles(t *testing.T) {
	f := writeFile(t, t.TempDir(), "scrape.txt", goodScrape)
	code, _, stderr := run(t, "", "check", "-url", "http://127.0.0.1:1/metrics", f)
	if code != 2 || !strings.Contains(stderr, "mutually exclusive") || !strings.Contains(stderr, "Usage") {
		t.Errorf("check -url with files: code %d, %s", code, stderr)
	}
}

func TestRun_CheckAllowProposedOwner(t *testing.T) {
	proposed := "# TYPE platform_build_info gauge\nplatform_build_info{" + ids + `,version="v1"} 1` + "\n"
	if code, _, _ := run(t, proposed, "check", "-strict"); code != 1 {
		t.Errorf("strict: code %d, want 1 (gincommon metrics are proposed)", code)
	}
	if code, stdout, _ := run(t, proposed, "check", "-strict", "-allow-proposed-owner", "platform-events, platform-gincommon"); code != 0 ||
		!strings.Contains(stdout, "warning [status]") {
		t.Errorf("strict with allowed owner: code %d\n%s", code, stdout)
	}
	// Ownerless proposed entries (services emit them) stay errors.
	queue := "# TYPE platform_queue_depth gauge\nplatform_queue_depth{" + ids + `,queue="q"} 1` + "\n"
	if code, _, _ := run(t, queue, "check", "-strict", "-allow-proposed-owner", "platform-gincommon"); code != 1 {
		t.Errorf("ownerless proposed: code %d", code)
	}
}

func TestRun_RefsSkipsVendoredDirs(t *testing.T) {
	dir := t.TempDir()
	for _, skip := range []string{".git", "node_modules", "vendor", "sub/vendor"} {
		writeFile(t, dir, filepath.Join(skip, "x.yml"), "expr: rate(http_requests_total[5m])\n")
	}
	writeFile(t, dir, "rules/a.yml", "expr: rate(http_requests_total[5m])\n")
	code, stdout, _ := run(t, "", "refs", dir)
	if code != 1 || !strings.Contains(stdout, "1 references: 1 errors") {
		t.Errorf("refs: code %d\n%s", code, stdout)
	}
	// An explicitly named vendor directory is still scanned.
	if code, stdout, _ := run(t, "", "refs", filepath.Join(dir, "vendor")); code != 1 || !strings.Contains(stdout, "1 references") {
		t.Errorf("explicit vendor root: code %d\n%s", code, stdout)
	}
}

func TestRun_RefsUnreadableSubdir(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	writeFile(t, locked, "x.yml", "a\n")
	writeFile(t, dir, "ok.yml", "expr: rate(http_requests_total[5m])\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755) //nolint:errcheck
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("directory still readable (running as root)")
	}
	code, stdout, _ := run(t, "", "refs", dir)
	if code != 2 || !strings.Contains(stdout, "locked:0: error: cannot read") || !strings.Contains(stdout, "ok.yml:1: error") {
		t.Errorf("unreadable subdir: code %d\n%s", code, stdout)
	}
}

func TestScanReferences_LongLines(t *testing.T) {
	// Minified dashboard JSON: one line well beyond bufio.Scanner's limits.
	line := `{"x":"` + strings.Repeat("a", 9<<20) + `","expr":"rate(http_requests_total[5m])"}`
	refs, err := obsregistry.Default().ScanReferences("dash.json", strings.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Line != 1 {
		t.Errorf("refs = %v", refs)
	}
}

const openMetricsScrape = `# HELP platform_retry_total Retries.
# TYPE platform_retry_total counter
platform_retry_total{` + ids + `,operation="consume",event_type="user.created"} 1 1.7e9 # {trace_id="a # b"} 1.0 1.7e9
platform_retry_created{` + ids + `,operation="consume",event_type="user.created"} 1.7e9
# HELP platform_build info.
# TYPE platform_build info
platform_build_info{` + ids + `,version="v1"} 1
# TYPE platform_dependency_request_seconds histogram
# UNIT platform_dependency_request_seconds seconds
platform_dependency_request_seconds_bucket{` + ids + `,dependency="db",operation="query",outcome="success",le="+Inf"} 1
platform_dependency_request_seconds_count{` + ids + `,dependency="db",operation="query",outcome="success"} 1
platform_dependency_request_seconds_sum{` + ids + `,dependency="db",operation="query",outcome="success"} 0.1
platform_dependency_request_seconds_created{` + ids + `,dependency="db",operation="query",outcome="success"} 1.7e9
# TYPE iam_event_consumer_queue_seconds gaugehistogram
iam_event_consumer_queue_seconds_bucket{` + ids + `,le="+Inf"} 1
iam_event_consumer_queue_seconds_gcount{` + ids + `} 1
iam_event_consumer_queue_seconds_gsum{` + ids + `} 2
# TYPE iam_event_consumer_mode stateset
iam_event_consumer_mode{` + ids + `,iam_event_consumer_mode="a"} 1
# TYPE iam_event_consumer_misc unknown
iam_event_consumer_misc{` + ids + `} 1
# EOF
`

func TestValidateText_OpenMetrics(t *testing.T) {
	rep := validate(t, openMetricsScrape, obsregistry.Options{})
	for name, tier := range map[string]obsregistry.Tier{
		"platform_retry_total":                obsregistry.TierPlatform,
		"platform_build_info":                 obsregistry.TierPlatform,
		"platform_dependency_request_seconds": obsregistry.TierPlatform,
		"iam_event_consumer_queue_seconds":    obsregistry.TierService,
		"iam_event_consumer_mode":             obsregistry.TierService,
	} {
		if rep.Tiers[name] != tier {
			t.Errorf("tier of %s = %q, want %q (tiers %v)", name, rep.Tiers[name], tier, rep.Tiers)
		}
	}
	for _, f := range rep.Errors() {
		// unknown maps to untyped, which the standard rejects; nothing else may fail.
		if f.Metric != "iam_event_consumer_misc" || f.Rule != obsregistry.RuleType {
			t.Errorf("unexpected finding %v", f)
		}
	}
	if len(rep.Errors()) != 1 {
		t.Errorf("want exactly the untyped finding, got %v", rep.Errors())
	}
	// Malformed OpenMetrics lines are left for the parser to report.
	for _, bad := range []string{"platform_x{a=\"1\" 1\n# EOF\n", "platform_x\n# EOF\n", "platform_x{} \n# EOF\n", "# TYPE x nonsense\n# EOF\n"} {
		if _, err := obsregistry.Default().ValidateText(strings.NewReader(bad), obsregistry.Options{}); err == nil {
			t.Errorf("want parse error for %q", bad)
		}
	}
	if _, err := obsregistry.Default().ValidateText(errReader{}, obsregistry.Options{}); err == nil {
		t.Error("want read error")
	}
}

func TestValidateText_CreatedCompanionsArePrecise(t *testing.T) {
	text := `# TYPE iam_event_consumer_jobs_total counter
iam_event_consumer_jobs_total{` + ids + `,kind="a"} 1
# TYPE iam_event_consumer_jobs_created gauge
iam_event_consumer_jobs_created{` + ids + `,kind="b"} 1.7e9
# TYPE iam_event_consumer_runs_total counter
iam_event_consumer_runs_total{` + ids + `} 1
# TYPE iam_event_consumer_runs_created counter
iam_event_consumer_runs_created{` + ids + `} 1
# TYPE iam_event_consumer_lat_seconds histogram
iam_event_consumer_lat_seconds_bucket{` + ids + `,le="+Inf"} 1
iam_event_consumer_lat_seconds_sum{` + ids + `} 1
iam_event_consumer_lat_seconds_count{` + ids + `} 1
# TYPE iam_event_consumer_lat_seconds_created gauge
iam_event_consumer_lat_seconds_created{` + ids + `} 1.7e9
# TYPE iam_event_consumer_up gauge
iam_event_consumer_up{` + ids + `} 1
# TYPE iam_event_consumer_up_created gauge
iam_event_consumer_up_created{` + ids + `} 1
`
	rep := validate(t, text, obsregistry.Options{})
	if rep.Tiers["iam_event_consumer_lat_seconds_created"] != obsregistry.TierExempt {
		t.Error("a histogram's matching _created gauge is a companion")
	}
	for _, name := range []string{"iam_event_consumer_jobs_created", "iam_event_consumer_runs_created", "iam_event_consumer_up_created"} {
		if rep.Tiers[name] == obsregistry.TierExempt {
			t.Errorf("%s must be validated (label sets differ, counter type, or gauge parent)", name)
		}
	}
	if !hasFinding(rep, obsregistry.SeverityError, obsregistry.RuleSuffix, "iam_event_consumer_runs_created", "_total") {
		t.Errorf("a counter named *_created is a metric of its own: %v", rep.Findings)
	}
}

func TestValidateText_Tier3LabelNames(t *testing.T) {
	text := "# TYPE iam_event_consumer_jobs_total counter\niam_event_consumer_jobs_total{" + ids + `,jobKind="a",ok_label="b"} 1` + "\n"
	rep := validate(t, text, obsregistry.Options{})
	if !hasFinding(rep, obsregistry.SeverityError, obsregistry.RuleLabelName, "iam_event_consumer_jobs_total", `"jobKind"`) {
		t.Errorf("want a label-name error for jobKind: %v", rep.Findings)
	}
	if len(rep.Errors()) != 1 {
		t.Errorf("only jobKind is invalid: %v", rep.Errors())
	}
}

func TestCheckCustom_LegacyNamePointsToReplacement(t *testing.T) {
	err := obsregistry.Default().CheckCustom(obsregistry.Counter, "http_requests_total", nil, "iam", "svc")
	if err == nil || !strings.Contains(err.Error(), "platform_http_requests_total") || strings.Contains(err.Error(), "NewShared") {
		t.Errorf("CheckCustom(legacy) = %v, want the replacement named", err)
	}
}

func TestCheckVersion(t *testing.T) {
	r := obsregistry.Default()
	for _, ok := range []string{"unknown", "dev", "v1.2.3", "1.2.3-rc.1+b5", "abc1234"} {
		if err := r.CheckVersion(ok); err != nil {
			t.Errorf("CheckVersion(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "v1 dirty", "-x", strings.Repeat("a", 65)} {
		if err := r.CheckVersion(bad); err == nil {
			t.Errorf("CheckVersion(%q): want error", bad)
		}
	}
}

// partialGatherer returns families together with an error, like a registry
// where one collector failed.
type partialGatherer struct{ fams []*dto.MetricFamily }

func (g partialGatherer) Gather() ([]*dto.MetricFamily, error) {
	return g.fams, errors.New("collector x failed")
}

func TestValidate_PartialGather(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "orders_total", Help: "h"})
	reg.MustRegister(c)
	c.Inc()
	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := pkgobs.Validate(partialGatherer{fams}, pkgobs.Options{})
	if err == nil || !strings.Contains(err.Error(), "collector x failed") {
		t.Fatalf("want the gather error, got %v", err)
	}
	if rep == nil || rep.Checked != 1 || rep.OK() {
		t.Fatalf("partial families must be validated and the report must fail: %+v", rep)
	}
	if !hasFinding(rep, pkgobs.SeverityError, pkgobs.RuleGather, "(gather)", "collector x failed") ||
		!hasFinding(rep, pkgobs.SeverityError, pkgobs.RuleNamespace, "orders_total", "unclassified") {
		t.Errorf("findings = %v", rep.Findings)
	}
	if rep, err := pkgobs.Validate(reg, pkgobs.Options{}); err != nil || rep.Checked != 1 {
		t.Errorf("clean gather: %v, %+v", err, rep)
	}
}

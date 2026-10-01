package obsregistry_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/obsregistry"
)

const ids = `domain="iam",environment="prod",service="event-consumer"`

func validate(t *testing.T, text string, opts obsregistry.Options) *obsregistry.Report {
	t.Helper()
	rep, err := obsregistry.Default().ValidateText(strings.NewReader(text), opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func hasFinding(rep *obsregistry.Report, sev obsregistry.Severity, rule, metric, contains string) bool {
	for _, f := range rep.Findings {
		if f.Severity == sev && f.Rule == rule && f.Metric == metric && strings.Contains(f.Message, contains) {
			return true
		}
	}
	return false
}

func TestValidateText_CompliantScrape(t *testing.T) {
	text := `# TYPE platform_messages_processed_total counter
platform_messages_processed_total{` + ids + `,queue="iam.events",event_type="user.created"} 3
# TYPE iam_event_consumer_hmac_failures_total counter
iam_event_consumer_hmac_failures_total{` + ids + `,key_kind="primary"} 1
# TYPE iam_event_consumer_cache_entries gauge
iam_event_consumer_cache_entries{` + ids + `} 12
# TYPE go_goroutines gauge
go_goroutines 7
`
	rep := validate(t, text, obsregistry.Options{})
	if !rep.OK() || len(rep.Warnings()) != 0 {
		t.Fatalf("want clean report, got %v", rep.Findings)
	}
	want := map[string]obsregistry.Tier{
		"platform_messages_processed_total":      obsregistry.TierPlatform,
		"iam_event_consumer_hmac_failures_total": obsregistry.TierService,
		"iam_event_consumer_cache_entries":       obsregistry.TierService,
		"go_goroutines":                          obsregistry.TierExempt,
	}
	for name, tier := range want {
		if rep.Tiers[name] != tier {
			t.Errorf("tier(%s) = %q, want %q", name, rep.Tiers[name], tier)
		}
	}
}

func TestValidateText_Findings(t *testing.T) {
	cases := []struct {
		name, text      string
		sev             obsregistry.Severity
		rule, metric, w string
	}{
		{"removed legacy", "# TYPE http_requests_total counter\nhttp_requests_total{service=\"s\"} 1\n",
			obsregistry.SeverityError, obsregistry.RuleRemoved, "http_requests_total", "emit platform_http_requests_total"},
		{"proposed", "# TYPE platform_queue_depth gauge\nplatform_queue_depth{" + ids + `,queue="q"} 1` + "\n",
			obsregistry.SeverityWarning, obsregistry.RuleStatus, "platform_queue_depth", "ratification"},
		{"name format", "# TYPE Bad_Name counter\nBad_Name 1\n",
			obsregistry.SeverityError, obsregistry.RuleNameFormat, "Bad_Name", "snake_case"},
		{"untyped", "iam_event_consumer_x 1\n",
			obsregistry.SeverityError, obsregistry.RuleType, "iam_event_consumer_x", "not covered"},
		{"registry type", "# TYPE platform_retry_total gauge\nplatform_retry_total{" + ids + `,operation="consume",event_type="e"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleType, "platform_retry_total", "defines a counter"},
		{"unregistered platform", "# TYPE platform_custom_total counter\nplatform_custom_total{" + ids + "} 1\n",
			obsregistry.SeverityError, obsregistry.RuleNamespace, "platform_custom_total", "ratification"},
		{"unclassified", "# TYPE orders_total counter\norders_total{" + ids + "} 1\n",
			obsregistry.SeverityError, obsregistry.RuleNamespace, "orders_total", "unclassified"},
		{"unregistered domain", "# TYPE iam_custom_total counter\niam_custom_total{" + ids + "} 1\n",
			obsregistry.SeverityError, obsregistry.RuleNamespace, "iam_custom_total", "iam_event_consumer_<metric>"},
		{"tier3 without service", "# TYPE iam_event_consumer_x_total counter\niam_event_consumer_x_total 1\n",
			obsregistry.SeverityError, obsregistry.RuleNamespace, "iam_event_consumer_x_total", "not in the domain registry"},
		{"tier3 wrong domain", "# TYPE iam_event_consumer_x_total counter\niam_event_consumer_x_total{domain=\"billing\",environment=\"prod\",service=\"event-consumer\"} 1\n",
			obsregistry.SeverityError, obsregistry.RuleNamespace, "iam_event_consumer_x_total", `domain="billing"`},
		{"domain metric wrong domain", "# TYPE iam_auth_events_total counter\niam_auth_events_total{domain=\"billing\",environment=\"prod\",service=\"s1\",event_type=\"login\",outcome=\"success\"} 1\n",
			obsregistry.SeverityError, obsregistry.RuleNamespace, "iam_auth_events_total", `emitted with domain="billing"`},
		{"counter suffix", "# TYPE iam_event_consumer_hits counter\niam_event_consumer_hits{" + ids + "} 1\n",
			obsregistry.SeverityError, obsregistry.RuleSuffix, "iam_event_consumer_hits", "_total"},
		{"histogram suffix", "# TYPE iam_event_consumer_lat histogram\niam_event_consumer_lat_bucket{" + ids + `,le="+Inf"} 1` + "\niam_event_consumer_lat_sum{" + ids + "} 1\niam_event_consumer_lat_count{" + ids + "} 1\n",
			obsregistry.SeverityError, obsregistry.RuleSuffix, "iam_event_consumer_lat", "_seconds"},
		{"required label", "# TYPE iam_event_consumer_x_total counter\niam_event_consumer_x_total{service=\"event-consumer\"} 1\n",
			obsregistry.SeverityError, obsregistry.RuleRequiredLabel, "iam_event_consumer_x_total", `"environment"`},
		{"prohibited label", "# TYPE iam_event_consumer_x_total counter\niam_event_consumer_x_total{" + ids + `,tenant_id="t1"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleProhibitedLabel, "iam_event_consumer_x_total", "tenant_id"},
		{"unapproved shared label", "# TYPE platform_retry_total counter\nplatform_retry_total{" + ids + `,operation="consume",event_type="e",colour="red"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleLabelVocabulary, "platform_retry_total", "colour"},
		{"missing registry label", "# TYPE platform_retry_total counter\nplatform_retry_total{" + ids + `,operation="consume"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleLabelVocabulary, "platform_retry_total", `"event_type" is missing`},
		{"metric value set", "# TYPE platform_outbox_errors_total counter\nplatform_outbox_errors_total{" + ids + `,operation="consume"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleLabelValue, "platform_outbox_errors_total", `operation="consume"`},
		{"version on shared metric", "# TYPE platform_queue_depth gauge\nplatform_queue_depth{" + ids + `,queue="q",version="v1"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleLabelVocabulary, "platform_queue_depth", `"version"`},
		{"label value", "# TYPE platform_retry_total counter\nplatform_retry_total{" + strings.Replace(ids, "prod", "qa", 1) + `,operation="consume",event_type="e"} 1` + "\n",
			obsregistry.SeverityError, obsregistry.RuleLabelValue, "platform_retry_total", `environment="qa"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := validate(t, tc.text, obsregistry.Options{})
			if !hasFinding(rep, tc.sev, tc.rule, tc.metric, tc.w) {
				t.Fatalf("want %s [%s] %s containing %q; got %v", tc.sev, tc.rule, tc.metric, tc.w, rep.Findings)
			}
		})
	}
}

func TestValidateText_PythonClientCompanions(t *testing.T) {
	text := `# TYPE python_gc_collections_total counter
python_gc_collections_total{generation="0"} 3
# TYPE http_requests_total counter
http_requests_total{service="s"} 1
# TYPE http_requests_created gauge
http_requests_created{service="s"} 1.7e9
# TYPE orphan_created gauge
orphan_created 1
`
	rep := validate(t, text, obsregistry.Options{})
	if rep.Tiers["python_gc_collections_total"] != obsregistry.TierExempt || rep.Tiers["http_requests_created"] != obsregistry.TierExempt {
		t.Errorf("tiers = %v", rep.Tiers)
	}
	if !hasFinding(rep, obsregistry.SeverityError, obsregistry.RuleNamespace, "orphan_created", "unclassified") {
		t.Errorf("a _created family without a parent must still be classified: %v", rep.Findings)
	}
}

func TestValidateText_DeprecatedLegacyWarns(t *testing.T) {
	r, err := obsregistry.Parse(deprecatedRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := r.ValidateText(strings.NewReader("# TYPE http_requests_total counter\nhttp_requests_total 1\n"), obsregistry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || !hasFinding(rep, obsregistry.SeverityWarning, obsregistry.RuleDeprecated, "http_requests_total", "platform_http_requests_total") {
		t.Fatalf("a deprecated legacy metric warns during its compatibility period: %v", rep.Findings)
	}
}

func TestValidateText_StrictFailsProposed(t *testing.T) {
	text := "# TYPE platform_queue_depth gauge\nplatform_queue_depth{" + ids + `,queue="q"} 1` + "\n"
	if rep := validate(t, text, obsregistry.Options{Strict: true}); rep.OK() {
		t.Fatal("strict mode must fail proposed metrics")
	}
}

func TestValidateText_Cardinality(t *testing.T) {
	var b strings.Builder
	b.WriteString("# TYPE platform_dlq_depth gauge\n")
	for i := range 101 {
		b.WriteString("platform_dlq_depth{" + ids + `,queue="q` + strings.Repeat("x", i) + `"} 1` + "\n")
	}
	rep := validate(t, b.String(), obsregistry.Options{})
	if !hasFinding(rep, obsregistry.SeverityError, obsregistry.RuleCardinality, "platform_dlq_depth", "101 distinct values (limit 100)") {
		t.Fatalf("want cardinality error, got %v", rep.Errors())
	}
}

func TestValidateText_ParseError(t *testing.T) {
	if _, err := obsregistry.Default().ValidateText(strings.NewReader("not a metric line {"), obsregistry.Options{}); err == nil {
		t.Fatal("want parse error")
	}
}

func TestValidateFamilies_UnsupportedTypeAndGaugeHistogram(t *testing.T) {
	summary := &dto.MetricFamily{Name: proto.String("iam_event_consumer_x_seconds"), Type: dto.MetricType_SUMMARY.Enum()}
	gh := &dto.MetricFamily{
		Name: proto.String("iam_event_consumer_y_seconds"), Type: dto.MetricType_GAUGE_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{Label: []*dto.LabelPair{
			{Name: proto.String("domain"), Value: proto.String("iam")},
			{Name: proto.String("environment"), Value: proto.String("prod")},
			{Name: proto.String("service"), Value: proto.String("event-consumer")},
		}}},
	}
	rep := obsregistry.Default().ValidateFamilies([]*dto.MetricFamily{summary, gh}, obsregistry.Options{})
	if !hasFinding(rep, obsregistry.SeverityError, obsregistry.RuleType, "iam_event_consumer_x_seconds", "SUMMARY") {
		t.Errorf("summary: %v", rep.Findings)
	}
	if rep.Tiers["iam_event_consumer_y_seconds"] != obsregistry.TierService || len(rep.Findings) != 1 {
		t.Errorf("gauge histogram: %v", rep.Findings)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestReport_WriteTo(t *testing.T) {
	rep := validate(t, "# TYPE http_requests_total counter\nhttp_requests_total 1\n# TYPE orders_total counter\norders_total 1\n", obsregistry.Options{})
	var buf bytes.Buffer
	if _, err := rep.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"error [removed] http_requests_total", "error [namespace] orders_total", "2 metrics checked: 2 errors, 0 warnings"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if _, err := rep.WriteTo(failWriter{}); err == nil {
		t.Error("want write error")
	}
}

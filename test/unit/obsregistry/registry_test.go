package obsregistry_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/obsregistry"
)

func TestDefault_EmbeddedRegistryIsValid(t *testing.T) {
	if _, err := obsregistry.Parse(obsregistry.Embedded()); err != nil {
		t.Fatalf("embedded registry invalid:\n%v", err)
	}
	if first, second := obsregistry.Default(), obsregistry.Default(); first != second {
		t.Fatal("Default must be memoized")
	}
}

func TestDefault_BucketPresetsMatchLibraryBuckets(t *testing.T) {
	r := obsregistry.Default()
	if !slices.Equal(r.Buckets("request_duration"), metrics.RequestDurationBuckets) {
		t.Error("request_duration preset drifted from metrics.RequestDurationBuckets")
	}
	if !slices.Equal(r.Buckets("payload_size"), metrics.PayloadSizeBuckets) {
		t.Error("payload_size preset drifted from metrics.PayloadSizeBuckets")
	}
}

func TestDefault_CanonicalMessagingMetricsAreApproved(t *testing.T) {
	r := obsregistry.Default()
	for _, name := range []string{
		"platform_messages_received_total", "platform_messages_processed_total",
		"platform_messages_failed_total", "platform_retry_total", "platform_dlq_messages_total",
	} {
		m, ok := r.Lookup(name)
		if !ok || m.Status != obsregistry.StatusApproved {
			t.Errorf("%s: want approved registry entry, got %+v", name, m)
		}
	}
	for _, name := range []string{
		"platform_duplicate_messages_total", "platform_queue_depth", "platform_dlq_depth",
		"platform_dependency_request_seconds", "platform_event_propagation_seconds",
	} {
		if m, ok := r.Lookup(name); !ok || m.Status != obsregistry.StatusProposed {
			t.Errorf("%s: want proposed registry entry (needs ratification), got %+v", name, m)
		}
	}
}

// mutate decodes the embedded registry, applies fn, and re-encodes it.
func mutate(t *testing.T, fn func(doc map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(obsregistry.Embedded(), &doc); err != nil {
		t.Fatal(err)
	}
	fn(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func metricsOf(doc map[string]any) []any { return doc["metrics"].([]any) }

func labelsOf(doc map[string]any) map[string]any { return doc["labels"].(map[string]any) }

func addMetric(doc map[string]any, m map[string]any) {
	doc["metrics"] = append(metricsOf(doc), m)
}

func shared(name, typ string) map[string]any {
	return map[string]any{
		"name": name, "tier": "platform", "type": typ, "status": "proposed",
		"help": "h", "labels": []any{}, "aggregation": "sum",
	}
}

func TestParse_RejectsInvalidRegistries(t *testing.T) {
	cases := []struct {
		name string
		fn   func(map[string]any)
		want string
	}{
		{"schema version", func(d map[string]any) { d["schema_version"] = 2 }, "schema_version"},
		{"label name", func(d map[string]any) {
			labelsOf(d)["Bad-Label"] = map[string]any{"description": "d", "cardinality": "c", "values": []any{"x"}}
		}, "not snake_case"},
		{"prohibited vocabulary label", func(d map[string]any) {
			labelsOf(d)["tenant_id"] = map[string]any{"description": "d", "cardinality": "c", "values": []any{"x"}}
		}, "prohibited"},
		{"label description", func(d map[string]any) {
			labelsOf(d)["thing"] = map[string]any{"values": []any{"x"}}
		}, "description and cardinality"},
		{"values and pattern", func(d map[string]any) {
			labelsOf(d)["thing"] = map[string]any{"description": "d", "cardinality": "c", "values": []any{"x"}, "pattern": "x"}
		}, "not both"},
		{"no constraint", func(d map[string]any) {
			labelsOf(d)["thing"] = map[string]any{"description": "d", "cardinality": "c"}
		}, "values or pattern is required"},
		{"bad pattern", func(d map[string]any) {
			labelsOf(d)["thing"] = map[string]any{"description": "d", "cardinality": "c", "pattern": "("}
		}, "pattern"},
		{"undefined const label", func(d map[string]any) {
			d["const_labels"] = append(d["const_labels"].([]any), "region")
		}, `const label "region" is not defined`},
		{"missing const label", func(d map[string]any) { d["const_labels"] = []any{"service", "environment"} }, `must include "domain"`},
		{"missing tier", func(d map[string]any) { delete(d["required_labels"].(map[string]any), "domain") }, "required_labels.domain"},
		{"required not const", func(d map[string]any) {
			d["required_labels"].(map[string]any)["service"] = []any{"route"}
		}, "must be a const label"},
		{"duplicate", func(d map[string]any) { addMetric(d, shared("platform_retry_total", "counter")) }, "duplicate"},
		{"metric name", func(d map[string]any) { addMetric(d, shared("platform_Bad_total", "counter")) }, "not snake_case"},
		{"help", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["help"] = ""
			addMetric(d, m)
		}, "help is required"},
		{"counter suffix", func(d map[string]any) { addMetric(d, shared("platform_x_count", "counter")) }, "_total"},
		{"histogram suffix", func(d map[string]any) {
			m := shared("platform_x_ms", "histogram")
			m["buckets"] = "request_duration"
			addMetric(d, m)
		}, "_seconds"},
		{"histogram unit", func(d map[string]any) {
			m := shared("platform_x_seconds", "histogram")
			m["unit"], m["buckets"] = "ms", "request_duration"
			addMetric(d, m)
		}, "unit"},
		{"gauge suffix", func(d map[string]any) { addMetric(d, shared("platform_x_total", "gauge")) }, "gauges must not"},
		{"type", func(d map[string]any) { addMetric(d, shared("platform_x_total", "summary")) }, "unknown type"},
		{"bucket preset", func(d map[string]any) {
			m := shared("platform_x_seconds", "histogram")
			m["buckets"] = "nope"
			addMetric(d, m)
		}, "bucket preset"},
		{"shared histogram buckets", func(d map[string]any) { addMetric(d, shared("platform_x_seconds", "histogram")) }, "must name a bucket preset"},
		{"status", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["status"] = "draft"
			addMetric(d, m)
		}, "unknown status"},
		{"deprecated shared", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["status"] = "deprecated"
			addMetric(d, m)
		}, "retired by deleting the entry"},
		{"aggregation", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["aggregation"] = ""
			addMetric(d, m)
		}, "aggregation"},
		{"platform prefix", func(d map[string]any) { addMetric(d, shared("x_total", "counter")) }, "platform_*"},
		{"domain unknown", func(d map[string]any) {
			m := shared("hr_x_total", "counter")
			m["tier"], m["domain"] = "domain", "hr"
			addMetric(d, m)
		}, `unknown domain "hr"`},
		{"domain prefix", func(d map[string]any) {
			m := shared("billing_x_total", "counter")
			m["tier"], m["domain"] = "domain", "iam"
			addMetric(d, m)
		}, "iam_*"},
		{"undefined label", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["labels"] = []any{"colour"}
			addMetric(d, m)
		}, "not in the label vocabulary"},
		{"const label declared", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["labels"] = []any{"service"}
			addMetric(d, m)
		}, "injected centrally"},
		{"legacy status", func(d map[string]any) {
			addMetric(d, legacy("old_total", "approved", []any{"platform_retry_total"}, "later"))
		}, "legacy metrics must be deprecated"},
		{"legacy replacement", func(d map[string]any) {
			addMetric(d, legacy("old_total", "deprecated", []any{"platform_nope_total"}, "later"))
		}, "is not in the registry"},
		{"legacy replacement legacy", func(d map[string]any) {
			addMetric(d, legacy("old_total", "deprecated", []any{"http_requests_total"}, "later"))
		}, "itself legacy"},
		{"legacy no replacement", func(d map[string]any) {
			addMetric(d, legacy("old_total", "deprecated", []any{}, "later"))
		}, "replacements are required"},
		{"legacy sunset", func(d map[string]any) {
			addMetric(d, legacy("old_total", "deprecated", []any{"platform_retry_total"}, ""))
		}, "sunset is required"},
		{"label values for undeclared label", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["label_values"] = map[string]any{"outcome": []any{"success"}}
			addMetric(d, m)
		}, "not one of its labels"},
		{"label values outside vocabulary", func(d map[string]any) {
			m := shared("platform_x_total", "counter")
			m["labels"] = []any{"outcome"}
			m["label_values"] = map[string]any{"outcome": []any{"maybe"}}
			addMetric(d, m)
		}, `outcome="maybe" is outside`},
		{"service tier", func(d map[string]any) {
			m := shared("iam_svc_x_total", "counter")
			m["tier"] = "service"
			addMetric(d, m)
		}, "cannot be registered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := obsregistry.Parse(mutate(t, tc.fn))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// deprecatedRegistry is the default registry with http_requests_total moved
// back to "deprecated", to exercise the compatibility-period code paths that
// the shipped registry (everything "removed") no longer reaches.
func deprecatedRegistry(t *testing.T) []byte {
	t.Helper()
	return mutate(t, func(d map[string]any) {
		for _, m := range metricsOf(d) {
			if m := m.(map[string]any); m["name"] == "http_requests_total" {
				m["status"] = "deprecated"
			}
		}
	})
}

func legacy(name, status string, repl []any, sunset string) map[string]any {
	return map[string]any{"name": name, "tier": "legacy", "type": "counter", "status": status,
		"help": "h", "labels": []any{}, "replacements": repl, "sunset": sunset}
}

func TestParse_LegacyNamesExemptFromSuffixRules(t *testing.T) {
	data := mutate(t, func(d map[string]any) {
		m := legacy("old_pending_total", "deprecated", []any{"platform_retry_total"}, "later")
		m["type"] = "gauge"
		addMetric(d, m)
	})
	if _, err := obsregistry.Parse(data); err != nil {
		t.Fatalf("legacy gauge ending in _total must be registrable: %v", err)
	}
}

func TestRegistry_ImportsReleasedLibraryContracts(t *testing.T) {
	r := obsregistry.Default()
	for name, want := range map[string]string{
		"platform_messages_received_total":  "queue",
		"platform_retry_total":              "operation,event_type",
		"platform_dlq_messages_total":       "operation,event_type,reason",
		"platform_db_queries_total":         "pool,operation,outcome",
		"platform_db_pool_connections":      "pool,state",
		"platform_messages_published_total": "topic,event_type,outcome",
	} {
		m, ok := r.Lookup(name)
		if !ok || strings.Join(m.Labels, ",") != want {
			t.Errorf("%s labels = %v, want %s (released platform-events / platform-pgcommon contract)", name, m, want)
		}
	}
	if m, _ := r.Lookup("platform_db_query_duration_seconds"); !slices.Contains(m.LabelValues["operation"], "SELECT") {
		t.Error("platform_db_query_duration_seconds must carry pgcommon's operation value set")
	}
}

func TestParse_RejectsMalformedJSON(t *testing.T) {
	if _, err := obsregistry.Parse([]byte(`{"unknown_field": 1}`)); err == nil {
		t.Fatal("want error for unknown field")
	}
}

func TestNormalizeEnvironment(t *testing.T) {
	r := obsregistry.Default()
	for in, want := range map[string]string{"prod": "prod", " PROD ": "prod", "dev": "dev", "local": "local", "staging": "staging"} {
		got, err := r.NormalizeEnvironment(in)
		if err != nil || got != want {
			t.Errorf("NormalizeEnvironment(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "qa", "production", "development"} {
		if _, err := r.NormalizeEnvironment(in); err == nil {
			t.Errorf("NormalizeEnvironment(%q): want error", in)
		}
	}
}

func TestCheckIdentity(t *testing.T) {
	r := obsregistry.Default()
	if env, err := r.CheckIdentity("iam", "event_consumer", "Prod"); err != nil || env != "prod" {
		t.Fatalf("CheckIdentity = %q, %v", env, err)
	}
	if _, err := r.CheckIdentity("hr", "event-consumer", "prod"); err == nil || !strings.Contains(err.Error(), "domain") {
		t.Errorf("unknown domain: err = %v", err)
	}
	if _, err := r.CheckIdentity("iam", "Event Consumer", "prod"); err == nil || !strings.Contains(err.Error(), "service") {
		t.Errorf("bad service: err = %v", err)
	}
	if _, err := r.CheckIdentity("iam", "event-consumer", "qa"); err == nil {
		t.Error("bad environment: want error")
	}
}

func TestServicePrefix(t *testing.T) {
	for _, tc := range []struct{ domain, service, want string }{
		{"iam", "event-consumer", "iam_event_consumer_"},
		{"iam", "iam-event-consumer", "iam_event_consumer_"},
		{"billing", "Invoice--API", "billing_invoice_api_"},
		{"tender_management", "tender_management-bids", "tender_management_bids_"},
	} {
		if got := obsregistry.ServicePrefix(tc.domain, tc.service); got != tc.want {
			t.Errorf("ServicePrefix(%q, %q) = %q, want %q", tc.domain, tc.service, got, tc.want)
		}
	}
}

func TestCheckCustom(t *testing.T) {
	r := obsregistry.Default()
	ok := []struct {
		typ            obsregistry.Type
		name, dom, svc string
		labels         []string
	}{
		{obsregistry.Counter, "iam_event_consumer_hmac_failures_total", "iam", "event-consumer", []string{"reason"}},
		{obsregistry.Gauge, "iam_event_consumer_cache_entries", "iam", "event-consumer", nil},
		{obsregistry.Histogram, "iam_event_consumer_lookup_seconds", "iam", "event-consumer", nil},
		{obsregistry.Counter, "orders_created_total", "", "orders", nil}, // legacy mode: no namespace rule
	}
	for _, tc := range ok {
		if err := r.CheckCustom(tc.typ, tc.name, tc.labels, tc.dom, tc.svc); err != nil {
			t.Errorf("CheckCustom(%s): %v", tc.name, err)
		}
	}
	bad := []struct {
		typ       obsregistry.Type
		name      string
		labels    []string
		dom, want string
	}{
		{obsregistry.Counter, "Bad_total", nil, "iam", "snake_case"},
		{obsregistry.Counter, "platform_retry_total", nil, "iam", "NewShared"},
		{obsregistry.Counter, "iam_auth_events_total", nil, "iam", "NewShared"},
		{obsregistry.Counter, "platform_custom_total", nil, "iam", "ratified"},
		{obsregistry.Counter, "go_custom_total", nil, "iam", "reserved"},
		{obsregistry.Counter, "iam_event_consumer_failures", nil, "iam", "_total"},
		{obsregistry.Histogram, "iam_event_consumer_lookup_ms", nil, "iam", "_seconds"},
		{obsregistry.Counter, "iam_event_consumer_x_total", []string{"Bad"}, "iam", "snake_case"},
		{obsregistry.Counter, "iam_event_consumer_x_total", []string{"tenant_id"}, "iam", "prohibited"},
		{obsregistry.Counter, "iam_event_consumer_x_total", []string{"customer_id"}, "iam", "prohibited"},
		{obsregistry.Counter, "iam_event_consumer_x_total", []string{"environment"}, "iam", "injected centrally"},
		{obsregistry.Counter, "iam_x_total", nil, "iam", "iam_event_consumer_<metric>"},
		{obsregistry.Counter, "billing_event_consumer_x_total", nil, "iam", "iam_event_consumer_"},
	}
	for _, tc := range bad {
		err := r.CheckCustom(tc.typ, tc.name, tc.labels, tc.dom, "event-consumer")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("CheckCustom(%s, %v) = %v, want error containing %q", tc.name, tc.labels, err, tc.want)
		}
	}
}

func TestSharedMetric(t *testing.T) {
	r := obsregistry.Default()
	if m, err := r.SharedMetric("platform_messages_processed_total", obsregistry.Counter, "billing"); err != nil || m.Name == "" {
		t.Fatalf("platform metric: %v", err)
	}
	if _, err := r.SharedMetric("iam_auth_events_total", obsregistry.Counter, "iam"); err != nil {
		t.Fatalf("domain metric in its domain: %v", err)
	}
	for _, tc := range []struct {
		name string
		typ  obsregistry.Type
		dom  string
		want string
	}{
		{"platform_nope_total", obsregistry.Counter, "iam", "not in the Platform Observability Registry"},
		{"http_requests_total", obsregistry.Counter, "iam", "not a shared one"},
		{"platform_queue_depth", obsregistry.Counter, "iam", "is a gauge"},
		{"iam_auth_events_total", obsregistry.Counter, "billing", `belongs to domain "iam"`},
	} {
		if _, err := r.SharedMetric(tc.name, tc.typ, tc.dom); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SharedMetric(%s) = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestDomainsAndLabels(t *testing.T) {
	r := obsregistry.Default()
	if !r.IsDomain("tender_management") || r.IsDomain("") || r.IsDomain("hr") {
		t.Error("IsDomain mismatch")
	}
	if !r.Labels["status_class"].Allows("4xx") || r.Labels["status_class"].Allows("418") {
		t.Error("enum label Allows mismatch")
	}
	if !r.Labels["route"].Allows("/api/items/:id") || r.Labels["route"].Allows("api") {
		t.Error("pattern label Allows mismatch")
	}
}

func TestDomains_EmptyWithoutDomainLabel(t *testing.T) {
	r := &obsregistry.Registry{}
	if r.Domains() != nil || r.IsDomain("iam") {
		t.Error("registry without a domain label has no domains")
	}
}

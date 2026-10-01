// Package obsregistry is the Platform Observability Registry: the approved
// platform-shared and domain-shared metrics, their label vocabularies, and the
// naming rules of the Enterprise Platform Observability Standard. It validates
// both registry edits (Parse) and what services actually emit (Validate*).
package obsregistry

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Tier is a metric's namespace classification.
type Tier string

// Metric tiers. TierLegacy covers pre-standard names kept during the
// compatibility period; TierExempt covers runtime collectors (go_*, process_*).
const (
	TierPlatform Tier = "platform"
	TierDomain   Tier = "domain"
	TierService  Tier = "service"
	TierLegacy   Tier = "legacy"
	TierExempt   Tier = "exempt"
)

// Status is a registry entry's governance state.
type Status string

// Registry statuses. Proposed entries await observability governance
// ratification; deprecated entries are legacy names still emitted during a
// compatibility period; removed entries are legacy names no library emits any
// more — kept so emitting or querying them is reported with the successor.
const (
	StatusApproved   Status = "approved"
	StatusProposed   Status = "proposed"
	StatusDeprecated Status = "deprecated"
	StatusRemoved    Status = "removed"
)

// Type is a Prometheus metric type.
type Type string

// Metric types the standard defines naming rules for.
const (
	Counter   Type = "counter"
	Gauge     Type = "gauge"
	Histogram Type = "histogram"
)

// Metric is one registry entry.
type Metric struct {
	Name   string `json:"name"`
	Tier   Tier   `json:"tier"`
	Domain string `json:"domain,omitempty"`
	// Owner is the platform library that emits the metric (empty: services
	// emit it themselves through the shared constructors).
	Owner   string   `json:"owner,omitempty"`
	Type    Type     `json:"type"`
	Unit    string   `json:"unit,omitempty"`
	Buckets string   `json:"buckets,omitempty"`
	Status  Status   `json:"status"`
	Help    string   `json:"help"`
	Labels  []string `json:"labels"`
	// LabelValues narrows a label's vocabulary for this metric (e.g. the
	// operation values one metric can take). Every value must also satisfy
	// the label's global definition.
	LabelValues     map[string][]string `json:"label_values,omitempty"`
	Aggregation     string              `json:"aggregation,omitempty"`
	Cardinality     string              `json:"cardinality,omitempty"`
	GovernanceNotes string              `json:"governance_notes,omitempty"`
	// Replacements and Sunset apply to legacy metrics: the successors to
	// migrate to and when the legacy name is removed.
	Replacements []string `json:"replacements,omitempty"`
	Sunset       string   `json:"sunset,omitempty"`
}

// Allows reports whether value is allowed for label on this metric: the
// entry's own value set when it declares one, else the label's vocabulary.
func (r *Registry) allows(m *Metric, label, value string) bool {
	if m != nil {
		if vals, ok := m.LabelValues[label]; ok {
			return slices.Contains(vals, value)
		}
	}
	def := r.Labels[label]
	return def == nil || def.Allows(value)
}

// Label is one entry of the label vocabulary: exactly one of Values (an
// allowed-value enum) or Pattern (a regular expression) constrains it.
type Label struct {
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	MaxValues   int      `json:"max_values,omitempty"`
	Cardinality string   `json:"cardinality"`

	re *regexp.Regexp
}

// Allows reports whether value is in the label's vocabulary.
func (l *Label) Allows(value string) bool {
	if l.re != nil {
		return l.re.MatchString(value)
	}
	return slices.Contains(l.Values, value)
}

// Registry is a parsed, self-consistent registry document.
type Registry struct {
	SchemaVersion           int                  `json:"schema_version"`
	ConstLabels             []string             `json:"const_labels"`
	RequiredLabels          map[Tier][]string    `json:"required_labels"`
	ProhibitedLabels        []string             `json:"prohibited_labels"`
	ProhibitedLabelSuffixes []string             `json:"prohibited_label_suffixes"`
	ExemptPrefixes          []string             `json:"exempt_prefixes"`
	BucketPresets           map[string][]float64 `json:"bucket_presets"`
	Labels                  map[string]*Label    `json:"labels"`
	Metrics                 []Metric             `json:"metrics"`

	byName map[string]*Metric
}

//go:embed registry.json
var embedded []byte

var (
	defaultOnce sync.Once
	defaultReg  *Registry
)

// Default returns the registry embedded in this module version. The embedded
// document is validated by this module's tests, so a parse failure is a
// build defect and panics.
func Default() *Registry {
	defaultOnce.Do(func() {
		r, err := Parse(embedded)
		if err != nil {
			panic("obsregistry: embedded registry.json is invalid: " + err.Error())
		}
		defaultReg = r
	})
	return defaultReg
}

// Embedded returns a copy of the embedded registry document.
func Embedded() []byte { return bytes.Clone(embedded) }

var (
	nameRE      = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	labelNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
)

// Parse decodes a registry document and checks it against the standard's
// naming, labelling and governance rules. All problems are reported together.
func Parse(data []byte) (*Registry, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r Registry
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("decode registry: %w", err)
	}
	if errs := r.compile(); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &r, nil
}

func (r *Registry) compile() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if r.SchemaVersion != 1 {
		fail("schema_version %d is not supported (want 1)", r.SchemaVersion)
	}
	for name, l := range r.Labels {
		if !labelNameRE.MatchString(name) {
			fail("label %q: name is not snake_case", name)
		}
		if r.isProhibitedLabel(name) {
			fail("label %q: name is prohibited (high cardinality)", name)
		}
		if l.Description == "" || l.Cardinality == "" {
			fail("label %q: description and cardinality are required", name)
		}
		switch {
		case len(l.Values) > 0 && l.Pattern != "":
			fail("label %q: set values or pattern, not both", name)
		case len(l.Values) == 0 && l.Pattern == "":
			fail("label %q: values or pattern is required", name)
		case l.Pattern != "":
			re, err := regexp.Compile(l.Pattern)
			if err != nil {
				fail("label %q: pattern: %v", name, err)
			}
			l.re = re
		}
	}
	for _, name := range r.ConstLabels {
		if r.Labels[name] == nil {
			fail("const label %q is not defined in labels", name)
		}
	}
	for _, name := range []string{"domain", "service", "environment"} {
		if !slices.Contains(r.ConstLabels, name) {
			fail("const_labels must include %q", name)
		}
	}
	for _, tier := range []Tier{TierPlatform, TierDomain, TierService} {
		if _, ok := r.RequiredLabels[tier]; !ok {
			fail("required_labels.%s is missing", tier)
		}
		for _, name := range r.RequiredLabels[tier] {
			if !slices.Contains(r.ConstLabels, name) {
				fail("required label %q (%s) must be a const label so it is injected centrally", name, tier)
			}
		}
	}

	r.byName = make(map[string]*Metric, len(r.Metrics))
	for i := range r.Metrics {
		m := &r.Metrics[i]
		if _, dup := r.byName[m.Name]; dup {
			fail("metric %q: duplicate entry", m.Name)
		}
		r.byName[m.Name] = m
	}
	for i := range r.Metrics {
		errs = append(errs, r.checkEntry(&r.Metrics[i])...)
	}
	return errs
}

func (r *Registry) checkEntry(m *Metric) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("metric %q: "+format, append([]any{m.Name}, args...)...))
	}
	if !nameRE.MatchString(m.Name) {
		fail("name is not snake_case")
	}
	if m.Help == "" {
		fail("help is required")
	}
	// Legacy entries record pre-standard names as they are; naming rules apply
	// to their replacements.
	if err := suffixError(m.Type, m.Name, m.Unit); err != nil && m.Tier != TierLegacy {
		fail("%v", err)
	}
	if m.Type == Histogram && m.Buckets != "" && r.BucketPresets[m.Buckets] == nil {
		fail("unknown bucket preset %q", m.Buckets)
	}
	switch m.Status {
	case StatusApproved, StatusProposed, StatusDeprecated, StatusRemoved:
	default:
		fail("unknown status %q", m.Status)
	}

	switch m.Tier {
	case TierPlatform, TierDomain:
		if m.Status == StatusDeprecated || m.Status == StatusRemoved {
			fail("shared metrics are retired by deleting the entry, not by status %s", m.Status)
		}
		if m.Aggregation == "" {
			fail("aggregation expectations are required for shared metrics")
		}
		if m.Tier == TierPlatform && !strings.HasPrefix(m.Name, "platform_") {
			fail("platform-tier metrics must be named platform_*")
		}
		if m.Tier == TierDomain {
			if !r.IsDomain(m.Domain) {
				fail("unknown domain %q", m.Domain)
			} else if !strings.HasPrefix(m.Name, m.Domain+"_") {
				fail("domain-tier metrics must be named %s_*", m.Domain)
			}
		}
		if m.Type == Histogram && m.Buckets == "" {
			fail("shared histograms must name a bucket preset so buckets aggregate across services")
		}
		for _, l := range m.Labels {
			switch {
			case r.Labels[l] == nil:
				fail("label %q is not in the label vocabulary", l)
			case slices.Contains(r.ConstLabels, l):
				fail("label %q is a const label and is injected centrally", l)
			}
		}
		for _, l := range sortedKeys(m.LabelValues) {
			if !slices.Contains(m.Labels, l) {
				fail("label_values for %q, which is not one of its labels", l)
				continue
			}
			for _, v := range m.LabelValues[l] {
				if def := r.Labels[l]; def != nil && !def.Allows(v) {
					fail("label_values %s=%q is outside the label vocabulary", l, v)
				}
			}
		}
	case TierLegacy:
		if m.Status != StatusDeprecated && m.Status != StatusRemoved {
			fail("legacy metrics must be deprecated or removed")
		}
		if len(m.Replacements) == 0 {
			fail("replacements are required for legacy metrics")
		}
		for _, name := range m.Replacements {
			repl := r.byName[name]
			switch {
			case repl == nil:
				fail("replacement %q is not in the registry", name)
			case repl.Tier == TierLegacy:
				fail("replacement %q is itself legacy", name)
			}
		}
		if m.Sunset == "" {
			fail("sunset is required for legacy metrics")
		}
	default:
		fail("tier %q cannot be registered (want platform, domain or legacy)", m.Tier)
	}
	return errs
}

// suffixError enforces the standard's suffix rules: counters end in _total,
// histograms in _seconds (or _bytes for registry entries declaring unit
// bytes), and gauges never in _total.
func suffixError(t Type, name, unit string) error {
	switch t {
	case Counter:
		if !strings.HasSuffix(name, "_total") {
			return errors.New("counters must end in _total")
		}
	case Histogram:
		want := "_seconds"
		if unit == "bytes" {
			want = "_bytes"
		} else if unit != "" && unit != "seconds" {
			return fmt.Errorf("histogram unit %q is not supported (want seconds or bytes)", unit)
		}
		if !strings.HasSuffix(name, want) {
			return fmt.Errorf("histograms must end in %s", want)
		}
	case Gauge:
		if strings.HasSuffix(name, "_total") {
			return errors.New("gauges must not end in _total (reserved for counters)")
		}
	default:
		return fmt.Errorf("unknown type %q", t)
	}
	return nil
}

// Lookup returns the registry entry for name.
func (r *Registry) Lookup(name string) (*Metric, bool) {
	m, ok := r.byName[name]
	return m, ok
}

// Domains returns the approved domain names.
func (r *Registry) Domains() []string {
	if l := r.Labels["domain"]; l != nil {
		return slices.Clone(l.Values)
	}
	return nil
}

// IsDomain reports whether d is an approved domain.
func (r *Registry) IsDomain(d string) bool {
	return d != "" && slices.Contains(r.Domains(), d)
}

// Buckets returns a copy of the named bucket preset.
func (r *Registry) Buckets(preset string) []float64 {
	return slices.Clone(r.BucketPresets[preset])
}

// NormalizeEnvironment trims and lowercases env (the precedence and
// normalization every platform library applies) and checks it against the
// environment vocabulary. There are no aliases: "production" is rejected so
// every library in a process emits the same value.
func (r *Registry) NormalizeEnvironment(env string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(env))
	if l := r.Labels["environment"]; l != nil && l.Allows(e) {
		return e, nil
	}
	return "", fmt.Errorf("environment %q is not in the registry vocabulary %v (set APP_ENV or Config.Environment to one of them)",
		env, r.Labels["environment"].Values)
}

// CheckIdentity validates the centrally injected identity labels and returns
// the normalized environment.
func (r *Registry) CheckIdentity(domain, service, environment string) (string, error) {
	if !r.IsDomain(domain) {
		return "", fmt.Errorf("domain %q is not in the registry vocabulary %v", domain, r.Domains())
	}
	if !r.Labels["service"].Allows(service) {
		return "", fmt.Errorf("service %q does not match the service label pattern %s", service, r.Labels["service"].Pattern)
	}
	return r.NormalizeEnvironment(environment)
}

// CheckVersion validates a build version against the version label
// vocabulary (the platform_build_info version label).
func (r *Registry) CheckVersion(version string) error {
	if l := r.Labels["version"]; l != nil && !l.Allows(version) {
		return fmt.Errorf("build version %q does not match the version label pattern %s (set Config.BuildVersion or BUILD_VERSION)", version, l.Pattern)
	}
	return nil
}

// ServicePrefix returns the <domain>_<service>_ prefix Tier 3 metrics of
// service must use. A service name already starting with the domain
// (iam-event-consumer in iam) is not prefixed twice.
func ServicePrefix(domain, service string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(service) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	s = strings.TrimPrefix(s, domain+"_")
	return domain + "_" + s + "_"
}

// domainOf returns the approved domain name is namespaced under, preferring
// the longest match (tender_management over a hypothetical tender).
func (r *Registry) domainOf(name string) string {
	best := ""
	for _, d := range r.Domains() {
		if strings.HasPrefix(name, d+"_") && len(d) > len(best) {
			best = d
		}
	}
	return best
}

func (r *Registry) isExempt(name string) bool {
	for _, p := range r.ExemptPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func (r *Registry) isProhibitedLabel(name string) bool {
	if slices.Contains(r.ProhibitedLabels, name) {
		return true
	}
	for _, s := range r.ProhibitedLabelSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// CheckCustom validates a service-owned (Tier 3) collector before it is
// registered. domain and service are the process identity; with an empty
// domain (e.g. a non-gincommon emitter checking names only) only the naming
// and label rules apply. Shared metrics
// must be created from their registry entry instead.
func (r *Registry) CheckCustom(t Type, name string, labelNames []string, domain, service string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("metric %q: name must be snake_case [a-z0-9_]", name)
	}
	if m, ok := r.Lookup(name); ok {
		if m.Tier == TierLegacy {
			return fmt.Errorf("metric %q is a %s legacy name; emit its replacement %s instead", name, m.Status, strings.Join(m.Replacements, ", "))
		}
		return fmt.Errorf("metric %q is a %s registry metric; create it from the registry (NewShared*) instead", name, m.Tier)
	}
	if strings.HasPrefix(name, "platform_") {
		return fmt.Errorf("metric %q: platform_* metrics must be ratified in the Platform Observability Registry before use", name)
	}
	if r.isExempt(name) {
		return fmt.Errorf("metric %q: prefix is reserved for runtime collectors", name)
	}
	if err := suffixError(t, name, ""); err != nil {
		return fmt.Errorf("metric %q: %w", name, err)
	}
	for _, l := range labelNames {
		switch {
		case !labelNameRE.MatchString(l):
			return fmt.Errorf("metric %q: label %q must be snake_case", name, l)
		case r.isProhibitedLabel(l):
			return fmt.Errorf("metric %q: label %q is prohibited (unbounded cardinality)", name, l)
		case slices.Contains(r.ConstLabels, l):
			return fmt.Errorf("metric %q: label %q is injected centrally and must not be declared", name, l)
		}
	}
	if domain != "" {
		if prefix := ServicePrefix(domain, service); !strings.HasPrefix(name, prefix) {
			return fmt.Errorf("metric %q: service-specific metrics must be named %s<metric>; domain-shared %s_* metrics must be ratified in the registry", name, prefix, domain)
		}
	}
	return nil
}

// SharedMetric returns the registry entry a shared collector named name is
// built from, checking it may be emitted by a service in domain.
func (r *Registry) SharedMetric(name string, t Type, domain string) (*Metric, error) {
	m, ok := r.Lookup(name)
	switch {
	case !ok:
		return nil, fmt.Errorf("metric %q is not in the Platform Observability Registry", name)
	case m.Tier != TierPlatform && m.Tier != TierDomain:
		return nil, fmt.Errorf("metric %q is a %s metric, not a shared one", name, m.Tier)
	case m.Type != t:
		return nil, fmt.Errorf("metric %q is a %s, not a %s", name, m.Type, t)
	case m.Tier == TierDomain && m.Domain != domain:
		return nil, fmt.Errorf("metric %q belongs to domain %q; this service is in domain %q", name, m.Domain, domain)
	}
	return m, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

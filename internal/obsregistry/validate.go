package obsregistry

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Severity of a Finding.
type Severity string

// Finding severities. Errors fail CI; warnings are reported only.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Rule identifiers reported in findings.
const (
	RuleNameFormat      = "name-format"
	RuleNamespace       = "namespace"
	RuleType            = "type"
	RuleSuffix          = "suffix"
	RuleRequiredLabel   = "required-label"
	RuleLabelVocabulary = "label-vocabulary"
	RuleProhibitedLabel = "prohibited-label"
	RuleLabelValue      = "label-value"
	RuleCardinality     = "cardinality"
	RuleStatus          = "status"
	RuleDeprecated      = "deprecated"
	RuleRemoved         = "removed"
)

// Finding is one compliance problem with an emitted metric.
type Finding struct {
	Severity Severity
	Rule     string
	Metric   string
	Message  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s [%s] %s: %s", f.Severity, f.Rule, f.Metric, f.Message)
}

// Options tune validation.
type Options struct {
	// Strict reports metrics still awaiting registry ratification (status
	// proposed) as errors instead of warnings.
	Strict bool
}

// Report is the outcome of validating a set of metric families.
type Report struct {
	Findings []Finding
	// Checked is the number of metric families validated.
	Checked int
	// Tiers maps every classified metric name to its tier.
	Tiers map[string]Tier
}

// Errors returns the error findings.
func (rep *Report) Errors() []Finding { return rep.filter(SeverityError) }

// Warnings returns the warning findings.
func (rep *Report) Warnings() []Finding { return rep.filter(SeverityWarning) }

// OK reports whether there are no error findings.
func (rep *Report) OK() bool { return len(rep.Errors()) == 0 }

func (rep *Report) filter(s Severity) []Finding {
	var out []Finding
	for _, f := range rep.Findings {
		if f.Severity == s {
			out = append(out, f)
		}
	}
	return out
}

// WriteTo writes one line per finding followed by a summary.
func (rep *Report) WriteTo(w io.Writer) (int64, error) {
	var b strings.Builder
	for _, f := range rep.Findings {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%d metrics checked: %d errors, %d warnings\n",
		rep.Checked, len(rep.Errors()), len(rep.Warnings()))
	n, err := io.WriteString(w, b.String())
	return int64(n), err
}

// ValidateText validates a Prometheus text-format scrape.
func (r *Registry) ValidateText(rd io.Reader, opts Options) (*Report, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := parser.TextToMetricFamilies(rd)
	if err != nil {
		return nil, fmt.Errorf("parse metrics: %w", err)
	}
	list := make([]*dto.MetricFamily, 0, len(fams))
	for _, f := range fams {
		list = append(list, f)
	}
	return r.ValidateFamilies(list, opts), nil
}

// ValidateFamilies checks gathered metric families against the standard:
// namespace classification, naming and suffix rules, required labels,
// registry compliance, and label vocabulary and cardinality.
func (r *Registry) ValidateFamilies(fams []*dto.MetricFamily, opts Options) *Report {
	sorted := slices.Clone(fams)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].GetName() < sorted[j].GetName() })
	rep := &Report{Tiers: make(map[string]Tier, len(sorted))}
	names := make(map[string]bool, len(sorted))
	for _, f := range sorted {
		names[f.GetName()] = true
	}
	for _, f := range sorted {
		rep.Checked++
		// prometheus_client (Python) exposes <metric>_created timestamps as a
		// separate family; they belong to their parent metric, not a namespace.
		if base, ok := strings.CutSuffix(f.GetName(), "_created"); ok && (names[base+"_total"] || names[base]) {
			rep.Tiers[f.GetName()] = TierExempt
			continue
		}
		r.validateFamily(rep, f, opts)
	}
	return rep
}

func (r *Registry) validateFamily(rep *Report, f *dto.MetricFamily, opts Options) {
	name := f.GetName()
	add := func(sev Severity, rule, format string, args ...any) {
		rep.Findings = append(rep.Findings, Finding{Severity: sev, Rule: rule, Metric: name, Message: fmt.Sprintf(format, args...)})
	}
	if r.isExempt(name) {
		rep.Tiers[name] = TierExempt
		return
	}
	if !nameRE.MatchString(name) {
		add(SeverityError, RuleNameFormat, "name must be snake_case [a-z0-9_]")
		return
	}
	t, ok := familyType(f)
	if !ok {
		add(SeverityError, RuleType, "type %s is not covered by the standard (use counter, gauge or histogram)", f.GetType())
		return
	}

	entry, registered := r.Lookup(name)
	if registered && entry.Tier == TierLegacy && entry.Status == StatusRemoved {
		rep.Tiers[name] = TierLegacy
		add(SeverityError, RuleRemoved, "legacy metric was removed; emit %s instead", strings.Join(entry.Replacements, ", "))
		return
	}
	if registered && entry.Tier == TierLegacy {
		rep.Tiers[name] = TierLegacy
		add(SeverityWarning, RuleDeprecated, "legacy metric; migrate dashboards, alerts, recording rules, SLOs and HPAs to %s. Sunset: %s",
			strings.Join(entry.Replacements, ", "), entry.Sunset)
		return
	}

	service := firstLabelValue(f, "service")
	var tier Tier
	switch {
	case registered:
		tier = entry.Tier
		if entry.Type != t {
			add(SeverityError, RuleType, "registry defines a %s, emitted as %s", entry.Type, t)
		}
		if entry.Status == StatusProposed {
			sev := SeverityWarning
			if opts.Strict {
				sev = SeverityError
			}
			add(sev, RuleStatus, "registry entry is proposed and awaits observability governance ratification")
		}
	case strings.HasPrefix(name, "platform_"):
		add(SeverityError, RuleNamespace, "platform_* metric is not in the Platform Observability Registry; submit it for ratification")
		return
	default:
		d := r.domainOf(name)
		prefix := ServicePrefix(d, service)
		switch {
		case d == "":
			add(SeverityError, RuleNamespace, "unclassified name; use platform_*, <domain>_* or <domain>_<service>_* with domain in %v", r.Domains())
			return
		case service == "" || !strings.HasPrefix(name, prefix):
			add(SeverityError, RuleNamespace, "%s_* metric is not in the domain registry; service-specific metrics must be named %s<metric>", d, prefix)
			return
		}
		tier = TierService
		if dom := firstLabelValue(f, "domain"); dom != "" && dom != d {
			add(SeverityError, RuleNamespace, "metric is namespaced under domain %q but emitted with domain=%q", d, dom)
		}
	}
	rep.Tiers[name] = tier

	unit := ""
	if registered {
		unit = entry.Unit
	}
	if err := suffixError(t, name, unit); err != nil {
		add(SeverityError, RuleSuffix, "%v", err)
	}
	r.checkLabels(f, tier, entry, add)
}

func (r *Registry) checkLabels(f *dto.MetricFamily, tier Tier, entry *Metric, add func(Severity, string, string, ...any)) {
	required := r.RequiredLabels[tier]
	var allowed []string
	if entry != nil {
		allowed = append(slices.Clone(r.ConstLabels), entry.Labels...)
		if entry.Tier == TierDomain {
			if dom := firstLabelValue(f, "domain"); dom != "" && dom != entry.Domain {
				add(SeverityError, RuleNamespace, "domain metric of %q emitted with domain=%q", entry.Domain, dom)
			}
		}
	}

	reported := map[string]bool{}
	once := func(key string, sev Severity, rule, format string, args ...any) {
		if !reported[key] {
			reported[key] = true
			add(sev, rule, format, args...)
		}
	}
	distinct := map[string]map[string]struct{}{}
	for _, m := range f.GetMetric() {
		present := map[string]bool{}
		for _, lp := range m.GetLabel() {
			ln, lv := lp.GetName(), lp.GetValue()
			present[ln] = true
			switch {
			case r.isProhibitedLabel(ln):
				once("prohibited:"+ln, SeverityError, RuleProhibitedLabel, "label %q is prohibited (unbounded cardinality)", ln)
				continue
			case allowed != nil && !slices.Contains(allowed, ln):
				once("vocab:"+ln, SeverityError, RuleLabelVocabulary, "label %q is not approved for this shared metric (allowed: %v)", ln, allowed)
				continue
			}
			def := r.Labels[ln]
			if def == nil {
				continue // service-owned label on a Tier 3 metric
			}
			if !r.allows(entry, ln, lv) {
				once("value:"+ln+"="+lv, SeverityError, RuleLabelValue, "label %s=%q is outside the vocabulary", ln, lv)
			}
			if distinct[ln] == nil {
				distinct[ln] = map[string]struct{}{}
			}
			distinct[ln][lv] = struct{}{}
		}
		for _, ln := range required {
			if !present[ln] {
				once("required:"+ln, SeverityError, RuleRequiredLabel, "required label %q is missing", ln)
			}
		}
		if entry != nil {
			for _, ln := range entry.Labels {
				if !present[ln] {
					once("declared:"+ln, SeverityError, RuleLabelVocabulary, "registry label %q is missing", ln)
				}
			}
		}
	}
	for _, ln := range sortedKeys(distinct) {
		if limit := r.Labels[ln].MaxValues; limit > 0 && len(distinct[ln]) > limit {
			add(SeverityError, RuleCardinality, "label %q has %d distinct values (limit %d)", ln, len(distinct[ln]), limit)
		}
	}
}

func familyType(f *dto.MetricFamily) (Type, bool) {
	switch f.GetType() {
	case dto.MetricType_COUNTER:
		return Counter, true
	case dto.MetricType_GAUGE:
		return Gauge, true
	case dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
		return Histogram, true
	default:
		return "", false
	}
}

func firstLabelValue(f *dto.MetricFamily, name string) string {
	for _, m := range f.GetMetric() {
		for _, lp := range m.GetLabel() {
			if lp.GetName() == name {
				return lp.GetValue()
			}
		}
	}
	return ""
}

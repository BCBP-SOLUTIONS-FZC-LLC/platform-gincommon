package obsregistry

import (
	"fmt"
	"io"
	"strings"
)

// WriteInventory renders the registry as the Markdown observability
// inventory (docs/observability/metric-registry.md).
func (r *Registry) WriteInventory(w io.Writer) error {
	var b strings.Builder
	b.WriteString("# Platform Observability Registry\n\n")
	b.WriteString("<!-- Generated from internal/obsregistry/registry.json by `make metrics-inventory`. Do not edit. -->\n\n")
	b.WriteString("Every metric carries the centrally injected const labels ")
	b.WriteString(codeList(r.ConstLabels))
	b.WriteString(". Required labels: platform tier ")
	b.WriteString(codeList(r.RequiredLabels[TierPlatform]))
	b.WriteString(", domain tier ")
	b.WriteString(codeList(r.RequiredLabels[TierDomain]))
	b.WriteString(", service tier ")
	b.WriteString(codeList(r.RequiredLabels[TierService]))
	b.WriteString(".\n\n")

	sections := []struct {
		title string
		tier  Tier
	}{
		{"Tier 1: platform-shared metrics", TierPlatform},
		{"Tier 2: domain-shared metrics", TierDomain},
		{"Legacy metrics (removed or deprecated)", TierLegacy},
	}
	for _, s := range sections {
		fmt.Fprintf(&b, "## %s\n\n", s.title)
		if s.tier == TierLegacy {
			b.WriteString("| Metric | Owner | Type | Status | Replacements | Sunset |\n|---|---|---|---|---|---|\n")
		} else {
			b.WriteString("| Metric | Owner | Type | Status | Labels | Meaning | Aggregation |\n|---|---|---|---|---|---|---|\n")
		}
		for _, m := range r.Metrics {
			if m.Tier != s.tier {
				continue
			}
			if s.tier == TierLegacy {
				fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n", m.Name, owner(m), m.Type, m.Status, codeList(m.Replacements), cell(m.Sunset))
				continue
			}
			typ := string(m.Type)
			if m.Buckets != "" {
				typ += " (" + m.Buckets + ")"
			}
			name := "`" + m.Name + "`"
			if m.Domain != "" {
				name += " (" + m.Domain + ")"
			}
			labels := codeList(m.Labels)
			for _, l := range sortedKeys(m.LabelValues) {
				labels += "<br>`" + l + "` ∈ " + codeList(m.LabelValues[l])
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
				name, owner(m), typ, m.Status, labels, cell(m.Help), cell(m.Aggregation))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Label vocabulary\n\n| Label | Allowed values | Max distinct values | Cardinality | Meaning |\n|---|---|---|---|---|\n")
	for _, name := range sortedKeys(r.Labels) {
		l := r.Labels[name]
		allowed := codeList(l.Values)
		if l.Pattern != "" {
			allowed = "pattern `" + l.Pattern + "`"
		}
		limit := "-"
		if l.MaxValues > 0 {
			limit = fmt.Sprint(l.MaxValues)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", name, cell(allowed), limit, cell(l.Cardinality), cell(l.Description))
	}

	b.WriteString("\n## Prohibited labels\n\n")
	b.WriteString(codeList(r.ProhibitedLabels))
	b.WriteString(", and any label ending in ")
	b.WriteString(codeList(r.ProhibitedLabelSuffixes))
	b.WriteString(".\n\n## Histogram bucket presets\n\n| Preset | Upper bounds |\n|---|---|\n")
	for _, name := range sortedKeys(r.BucketPresets) {
		fmt.Fprintf(&b, "| `%s` | %s |\n", name, strings.Trim(fmt.Sprint(r.BucketPresets[name]), "[]"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func owner(m Metric) string {
	if m.Owner == "" {
		return "services"
	}
	return m.Owner
}

func codeList(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return "`" + strings.Join(items, "`, `") + "`"
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

package obsregistry

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Reference is a metric name found in a dashboard, alert, recording rule,
// SLO, HPA manifest or runbook that needs attention.
type Reference struct {
	File        string
	Line        int
	Metric      string
	Severity    Severity
	Replacement string
	Message     string
}

func (ref Reference) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", ref.File, ref.Line, ref.Severity, ref.Message)
}

var identRE = regexp.MustCompile(`[A-Za-z_:][A-Za-z0-9_:]*`)

// histogramSeriesSuffixes are the per-series suffixes a histogram exposes.
var histogramSeriesSuffixes = []string{"_bucket", "_sum", "_count"}

// ScanReferences reports metric names in rd that are legacy (deprecated, with
// their replacement) or claim the platform_ namespace without a registry
// entry. file labels the results. Recording-rule names (containing ':') are
// skipped; their inputs are what is checked.
func (r *Registry) ScanReferences(file string, rd io.Reader) ([]Reference, error) {
	var refs []Reference
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		seen := map[string]bool{}
		for _, tok := range identRE.FindAllString(sc.Text(), -1) {
			// Metric names never contain ':' (recording rules) or end in '_' (prose such as "platform_*").
			if strings.Contains(tok, ":") || strings.HasSuffix(tok, "_") || seen[tok] {
				continue
			}
			seen[tok] = true
			if ref, ok := r.checkReference(tok); ok {
				ref.File, ref.Line = file, line
				refs = append(refs, ref)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return refs, fmt.Errorf("scan %s: %w", file, err)
	}
	return refs, nil
}

func (r *Registry) checkReference(tok string) (Reference, bool) {
	m, base := r.resolveSeries(tok)
	switch {
	case m != nil && m.Tier == TierLegacy:
		repls := make([]string, len(m.Replacements))
		for i, name := range m.Replacements {
			repls[i] = name
			// Carry a histogram series suffix over when the successor is a histogram too.
			if suffix := strings.TrimPrefix(tok, base); suffix != "" {
				if succ, ok := r.Lookup(name); ok && succ.Type == Histogram {
					repls[i] += suffix
				}
			}
		}
		repl := strings.Join(repls, " or ")
		if m.Status == StatusRemoved {
			return Reference{
				Metric: tok, Severity: SeverityError, Replacement: repl,
				Message: fmt.Sprintf("legacy metric %s was removed and is no longer emitted; use %s", tok, repl),
			}, true
		}
		return Reference{
			Metric: tok, Severity: SeverityWarning, Replacement: repl,
			Message: fmt.Sprintf("legacy metric %s is deprecated (sunset: %s); use %s", tok, m.Sunset, repl),
		}, true
	// Unregistered names are only reported when they look like a series name;
	// group names and prose (platform_events, platform_messages_received/...)
	// are not metrics. Unregistered gauges are caught by the scrape check.
	case m == nil && strings.HasPrefix(tok, "platform_") && hasSeriesSuffix(tok):
		return Reference{
			Metric: tok, Severity: SeverityError,
			Message: fmt.Sprintf("%s is not in the Platform Observability Registry", tok),
		}, true
	}
	return Reference{}, false
}

// resolveSeries maps a series name (including histogram _bucket/_sum/_count
// series) to its registry entry and the entry's base name.
func (r *Registry) resolveSeries(tok string) (*Metric, string) {
	if m, ok := r.Lookup(tok); ok {
		return m, tok
	}
	for _, s := range histogramSeriesSuffixes {
		base, found := strings.CutSuffix(tok, s)
		if !found {
			continue
		}
		if m, ok := r.Lookup(base); ok && m.Type == Histogram {
			return m, base
		}
	}
	return nil, tok
}

var seriesSuffixes = []string{"_total", "_seconds", "_bytes", "_bucket", "_sum", "_count"}

func hasSeriesSuffix(tok string) bool {
	for _, s := range seriesSuffixes {
		if strings.HasSuffix(tok, s) {
			return true
		}
	}
	return false
}

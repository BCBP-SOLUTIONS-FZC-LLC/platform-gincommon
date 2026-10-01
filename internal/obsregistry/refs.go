package obsregistry

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync/atomic"
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

// ErrReferenceFileTooLarge is returned by ScanReferences for input larger
// than the per-file cap (64 MiB, the same bound as a fetched scrape).
var ErrReferenceFileTooLarge = errors.New("file exceeds the reference scan size limit")

// maxReferenceFileBytes is the per-file ScanReferences cap.
var maxReferenceFileBytes atomic.Int64

func init() { maxReferenceFileBytes.Store(maxScrapeBytes) }

// SetMaxReferenceFileBytesForTesting lowers the ScanReferences per-file cap so
// tests need not build 64 MiB inputs, and returns a func restoring the
// previous cap. This package is internal; consuming services cannot reach it.
func SetMaxReferenceFileBytesForTesting(n int64) (restore func()) {
	prev := maxReferenceFileBytes.Swap(n)
	return func() { maxReferenceFileBytes.Store(prev) }
}

var identRE = regexp.MustCompile(`[A-Za-z_:][A-Za-z0-9_:]*`)

// histogramSeriesSuffixes are the per-series suffixes a histogram exposes.
var histogramSeriesSuffixes = []string{"_bucket", "_sum", "_count"}

// ScanReferences reports metric names in rd that are legacy (deprecated, with
// their replacement) or claim the platform_ namespace without a registry
// entry. file labels the results. Recording-rule names (containing ':') are
// skipped; their inputs are what is checked.
//
// Input beyond 64 MiB is not read: ScanReferences returns the references found
// so far and an error wrapping ErrReferenceFileTooLarge, so a huge
// newline-free file cannot exhaust memory. Lines are otherwise unbounded.
func (r *Registry) ScanReferences(file string, rd io.Reader) ([]Reference, error) {
	var refs []Reference
	limit := maxReferenceFileBytes.Load()
	// bufio.Reader rather than Scanner: minified dashboard JSON can put
	// megabytes on one line, and a Scanner fails such lines with ErrTooLong.
	// The LimitReader (one byte past the cap, to detect overflow) bounds what
	// a single ReadString can buffer.
	br := bufio.NewReader(io.LimitReader(rd, limit+1))
	var total int64
	for line := 1; ; line++ {
		text, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return refs, fmt.Errorf("scan %s: %w", file, err)
		}
		if total += int64(len(text)); total > limit {
			return refs, fmt.Errorf("scan %s: %w (%d bytes)", file, ErrReferenceFileTooLarge, limit)
		}
		if text == "" && err == io.EOF {
			return refs, nil
		}
		seen := map[string]bool{}
		for _, tok := range identRE.FindAllString(text, -1) {
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
		if err == io.EOF {
			return refs, nil
		}
	}
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

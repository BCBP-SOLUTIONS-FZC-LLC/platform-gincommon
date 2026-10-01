package obsregistry

import (
	"bytes"
	"strings"
)

// prometheus/common ships an OpenMetrics encoder but no parser, so OpenMetrics
// input is normalized to the Prometheus text format (0.0.4) before parsing.
// The normalization keeps everything the standard validates (family names,
// types, label names and values) and drops what it does not (timestamps,
// exemplars, units, _created samples).

// isOpenMetrics reports whether data is OpenMetrics text: it must end with
// the mandatory "# EOF" line.
func isOpenMetrics(data []byte) bool {
	for line := range bytes.Lines(data) {
		if string(bytes.TrimSpace(line)) == "# EOF" {
			return true
		}
	}
	return false
}

// openMetricsToText rewrites OpenMetrics text as Prometheus text:
//   - counter families are named with their _total sample name (OpenMetrics
//     names the family without it), info families with their _info sample
//     name as gauges, stateset families become gauges, unknown becomes
//     untyped, and gaugehistogram _gcount/_gsum samples become _count/_sum;
//   - _created samples of counters, histograms and summaries, "# UNIT" and
//     "# EOF" lines are dropped;
//   - exemplars and (float, seconds) timestamps are stripped from samples.
func openMetricsToText(data []byte) []byte {
	types := map[string]string{}
	for line := range bytes.Lines(data) {
		if f := strings.Fields(string(line)); len(f) == 4 && f[0] == "#" && f[1] == "TYPE" {
			types[f[2]] = f[3]
		}
	}
	var out bytes.Buffer
	for raw := range bytes.Lines(data) {
		line := strings.TrimRight(string(raw), "\r\n")
		switch {
		case strings.TrimSpace(line) == "":
			continue
		case strings.HasPrefix(line, "#"):
			if l, ok := omMetadata(line, types); ok {
				out.WriteString(l)
				out.WriteByte('\n')
			}
		default:
			if l, ok := omSample(line, types); ok {
				out.WriteString(l)
				out.WriteByte('\n')
			}
		}
	}
	return out.Bytes()
}

// omFamilyName maps an OpenMetrics family name to its Prometheus text name.
func omFamilyName(name string, types map[string]string) string {
	switch types[name] {
	case "counter":
		if !strings.HasSuffix(name, "_total") {
			return name + "_total"
		}
	case "info":
		if !strings.HasSuffix(name, "_info") {
			return name + "_info"
		}
	}
	return name
}

var omTypes = map[string]string{
	"counter": "counter", "gauge": "gauge", "histogram": "histogram", "summary": "summary",
	"gaugehistogram": "histogram", "info": "gauge", "stateset": "gauge", "unknown": "untyped",
}

func omMetadata(line string, types map[string]string) (string, bool) {
	f := strings.Fields(line)
	if len(f) < 3 || f[0] != "#" {
		return line, true
	}
	switch f[1] {
	case "EOF", "UNIT":
		return "", false
	case "TYPE":
		t, ok := omTypes[f[len(f)-1]]
		if !ok {
			return line, true // let the parser report it
		}
		return "# TYPE " + omFamilyName(f[2], types) + " " + t, true
	case "HELP":
		rest := strings.TrimPrefix(strings.TrimPrefix(line, "# HELP "), f[2])
		return "# HELP " + omFamilyName(f[2], types) + rest, true
	}
	return line, true
}

func omSample(line string, types map[string]string) (string, bool) {
	nameEnd := strings.IndexAny(line, "{ \t")
	if nameEnd < 0 {
		return line, true // no value: let the parser report it
	}
	name, end := line[:nameEnd], nameEnd
	if line[nameEnd] == '{' {
		if end = labelsEnd(line, nameEnd); end < 0 {
			return line, true // unterminated labels: let the parser report it
		}
	}
	if base, ok := strings.CutSuffix(name, "_created"); ok {
		switch types[base] {
		case "counter", "histogram", "summary", "gaugehistogram":
			return "", false
		}
	}
	if base, ok := strings.CutSuffix(name, "_gcount"); ok && types[base] == "gaugehistogram" {
		name = base + "_count"
	} else if base, ok := strings.CutSuffix(name, "_gsum"); ok && types[base] == "gaugehistogram" {
		name = base + "_sum"
	}
	labels := line[nameEnd:end]
	rest, _, _ := strings.Cut(line[end:], "#") // exemplar
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return line, true
	}
	return name + labels + " " + fields[0], true // value; timestamp dropped
}

// labelsEnd returns the index just past the '}' closing the label set that
// opens at line[open], honoring quoted values and escapes, or -1.
func labelsEnd(line string, open int) int {
	inQuote := false
	for i := open + 1; i < len(line); i++ {
		switch c := line[i]; {
		case inQuote && c == '\\':
			i++
		case c == '"':
			inQuote = !inQuote
		case !inQuote && c == '}':
			return i + 1
		}
	}
	return -1
}

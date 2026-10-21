package problem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// extensionNamePattern is RFC 9457 §3.2's recommendation for extension member names:
// a letter, then letters, digits or "_", at least three characters in all.
var extensionNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,}$`)

// standardMembers are the JSON names of Problem's own members (RFC 9457's and the
// platform's). An extension never replaces one of them.
var standardMembers = []string{
	"type", "title", "status", "detail", "instance",
	"traceId", "errorCode", "tenantId", "timestamp", "validationErrors",
}

// isStandardMember reports whether name is a Problem member, ignoring case:
// encoding/json matches struct members case-insensitively, so "Status" would
// overwrite status in a Go client.
func isStandardMember(name string) bool {
	for _, m := range standardMembers {
		if strings.EqualFold(name, m) {
			return true
		}
	}
	return false
}

// ValidExtensionName reports whether name may be sent as an extension member: it
// follows RFC 9457 §3.2 (a letter, then letters, digits or "_", at least three
// characters) and is not a standard or platform member in any letter case
// ("status", "TraceID", ...). Any other name is dropped when the problem is built
// or serialised, so an extension can never overwrite a standard member.
func ValidExtensionName(name string) bool {
	return extensionNamePattern.MatchString(name) && !isStandardMember(name)
}

// validExtensions is ext without the entries ValidExtensionName rejects, or nil
// when none is left. The map is a shallow copy.
func validExtensions(ext map[string]any) map[string]any {
	var out map[string]any
	for name, v := range ext {
		if !ValidExtensionName(name) {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(ext))
		}
		out[name] = v
	}
	return out
}

// problemMembers is Problem without its JSON methods: the standard members only.
type problemMembers Problem

// MarshalJSON writes the standard members in the specification's order, then the
// valid extension members (ValidExtensionName) sorted by name, so the output is
// deterministic. A problem without extensions serialises exactly as a plain
// struct would. It fails only when an extension value cannot be marshalled.
func (p Problem) MarshalJSON() ([]byte, error) {
	std, err := json.Marshal(problemMembers(p))
	if err != nil || len(p.Extensions) == 0 {
		return std, err
	}
	ext := validExtensions(p.Extensions)
	if len(ext) == 0 {
		return std, nil
	}
	var buf bytes.Buffer
	buf.Write(std[:len(std)-1]) // the members object without its closing brace
	for _, name := range slices.Sorted(maps.Keys(ext)) {
		v, err := json.Marshal(ext[name])
		if err != nil {
			return nil, fmt.Errorf("problem: extension %q: %w", name, err)
		}
		k, _ := json.Marshal(name) // a valid name is plain ASCII: cannot fail
		buf.WriteByte(',')
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads the standard members and keeps every other top-level member
// in Extensions, whatever its name, so a client can read a third-party server's
// extensions. Numbers are json.Number (no float64 rounding of large ids or
// versions); objects, arrays, strings, booleans and null decode as encoding/json
// decodes into any. Extensions is nil when the body has none.
func (p *Problem) UnmarshalJSON(data []byte) error {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	var std problemMembers
	if err := json.Unmarshal(data, &std); err != nil {
		return err
	}
	var ext map[string]any
	for name, raw := range all {
		if isStandardMember(name) {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		_ = dec.Decode(&v) // raw is one valid JSON value: json.Unmarshal checked it
		if ext == nil {
			ext = make(map[string]any)
		}
		ext[name] = v
	}
	*p = Problem(std)
	p.Extensions = ext
	return nil
}

// Extension decodes the extension member name into dst (a pointer, as for
// json.Unmarshal) and reports whether the problem has it:
//
//	var version int64
//	if ok, err := p.Extension("record_version", &version); ok && err == nil {
//		// retry with version
//	}
//
// A missing member returns false and a nil error, leaving dst unchanged.
func (p *Problem) Extension(name string, dst any) (bool, error) {
	v, ok := p.Extensions[name]
	if !ok {
		return false, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return true, fmt.Errorf("problem: extension %q: %w", name, err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return true, fmt.Errorf("problem: extension %q: %w", name, err)
	}
	return true, nil
}

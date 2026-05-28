package domain

import (
	"regexp"
	"strings"
)

var roleTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// NormalizeHeaderValue trims and validates a single-line header value for identity fields.
func NormalizeHeaderValue(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > MaxHeaderValueLen {
		return "", false
	}
	for _, r := range s {
		// Reject ASCII controls, DEL, and Unicode line separators (U+2028/U+2029).
		// Some log parsers treat U+2028/U+2029 as newlines, enabling log injection
		// even though they are valid UTF-8 above 0x7F.
		if r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029 {
			return "", false
		}
	}
	return s, true
}

// ParseRoles splits and validates x-tenant-roles. Invalid tokens are dropped.
func ParseRoles(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	roles := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		r := strings.TrimSpace(p)
		if r == "" || len(r) > MaxRoleLen || !roleTokenPattern.MatchString(r) {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		roles = append(roles, r)
	}
	return roles
}

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// NormalizeRequestID validates a client-provided request ID or reports invalid.
func NormalizeRequestID(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > MaxRequestIDLen {
		return "", false
	}
	if !requestIDPattern.MatchString(s) {
		return "", false
	}
	return s, true
}

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
//
// It fails closed on oversized input: when the header exceeds MaxRolesHeaderLen bytes
// or carries more than MaxRoles valid roles it returns nil (no roles, so a default-deny
// Authorizer grants nothing beyond role-free actions). Callers that must reject such
// requests outright use ParseRolesStrict, which reports ErrInvalidRoles instead.
func ParseRoles(header string) []string {
	roles, err := ParseRolesStrict(header)
	if err != nil {
		return nil
	}
	return roles
}

// ParseRolesStrict is ParseRoles that reports over-limit input: it returns
// ErrInvalidRoles when header is longer than MaxRolesHeaderLen bytes or yields more
// than MaxRoles distinct valid roles. Individually invalid tokens are still dropped.
func ParseRolesStrict(header string) ([]string, error) {
	if header == "" {
		return nil, nil
	}
	if len(header) > MaxRolesHeaderLen {
		return nil, ErrInvalidRoles
	}
	parts := strings.Split(header, ",")
	roles := make([]string, 0, min(len(parts), MaxRoles))
	seen := make(map[string]struct{}, min(len(parts), MaxRoles))
	for _, p := range parts {
		r := strings.TrimSpace(p)
		if r == "" || len(r) > MaxRoleLen || !roleTokenPattern.MatchString(r) {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		if len(roles) == MaxRoles {
			return nil, ErrInvalidRoles
		}
		seen[r] = struct{}{}
		roles = append(roles, r)
	}
	return roles, nil
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

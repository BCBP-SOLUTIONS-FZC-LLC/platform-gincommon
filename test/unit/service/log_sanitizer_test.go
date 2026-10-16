package service_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
)

func TestSanitize(t *testing.T) {
	in := map[string]interface{}{"a": 1, "b": "x"}
	out := service.Sanitize(in)
	if len(out) != len(in) {
		t.Fatalf("len = %d want %d", len(out), len(in))
	}
	if out["a"] != 1 || out["b"] != "x" {
		t.Fatalf("Sanitize() = %v", out)
	}
	in["a"] = 99
	if out["a"] != 1 {
		t.Fatal("Sanitize should copy values")
	}
}

func TestSanitize_RedactsSensitiveKeys(t *testing.T) {
	in := map[string]interface{}{
		"user_id":       "u1",
		"authorization": "Bearer secret",
		"access_token":  "tok",
		"note":          "ok",
	}
	out := service.Sanitize(in)
	if out["user_id"] != "u1" {
		t.Fatalf("user_id = %v", out["user_id"])
	}
	if out["authorization"] != "[REDACTED]" || out["access_token"] != "[REDACTED]" {
		t.Fatalf("Sanitize() = %v", out)
	}
	if out["note"] != "ok" {
		t.Fatalf("note = %v", out["note"])
	}
}

func TestSanitize_RedactsJWT(t *testing.T) {
	out := service.Sanitize(map[string]any{"jwt": "eyJhbGc...", "jwt_token": "x"})
	if out["jwt"] != "[REDACTED]" {
		t.Fatalf("jwt = %v, want [REDACTED]", out["jwt"])
	}
	if out["jwt_token"] != "[REDACTED]" {
		t.Fatalf("jwt_token = %v, want [REDACTED]", out["jwt_token"])
	}
}

func TestSanitize_RedactsSession(t *testing.T) {
	out := service.Sanitize(map[string]any{"session": "abc", "session_id": "xyz"})
	if out["session"] != "[REDACTED]" {
		t.Fatalf("session = %v, want [REDACTED]", out["session"])
	}
	if out["session_id"] != "[REDACTED]" {
		t.Fatalf("session_id = %v, want [REDACTED]", out["session_id"])
	}
}

func TestSanitize_RedactsBearer(t *testing.T) {
	out := service.Sanitize(map[string]any{"bearer": "token123", "bearer_token": "x"})
	if out["bearer"] != "[REDACTED]" {
		t.Fatalf("bearer = %v, want [REDACTED]", out["bearer"])
	}
	if out["bearer_token"] != "[REDACTED]" {
		t.Fatalf("bearer_token = %v, want [REDACTED]", out["bearer_token"])
	}
}

func TestSanitizeString_StripsControlChars(t *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(string) bool
	}{
		{"newline", "user\ninjected: evil", func(s string) bool { return !containsRune(s, '\n') }},
		{"carriage-return", "val\rinjected", func(s string) bool { return !containsRune(s, '\r') }},
		{"null-byte", "val\x00null", func(s string) bool { return !containsRune(s, '\x00') }},
		{"clean", "clean-value", func(s string) bool { return s == "clean-value" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := service.SanitizeString(c.input)
			if !c.check(got) {
				t.Fatalf("SanitizeString(%q) = %q — control char not stripped", c.input, got)
			}
		})
	}
}

func TestSanitizeString_TruncatesAt256Bytes(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := service.SanitizeString(long)
	if len(got) > 256 {
		t.Fatalf("SanitizeString len = %d, want ≤ 256", len(got))
	}
}

// TestSanitizeString_TruncatesAtRuneBoundary ensures the UTF-8 boundary walk runs.
// Input: 255 ASCII bytes + "€" (U+20AC, 3 bytes) = 258 bytes total.
// After truncation the result must be ≤ 256 bytes AND valid UTF-8 (the euro sign is dropped).
func TestSanitizeString_TruncatesAtRuneBoundary(t *testing.T) {
	s := strings.Repeat("a", 255) + "€" // 255 + 3 = 258 bytes
	got := service.SanitizeString(s)
	if len(got) > 256 {
		t.Fatalf("SanitizeString len = %d, want ≤ 256", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("SanitizeString(%q) = %q, want valid UTF-8", s, got)
	}
	// The euro sign must have been dropped — it starts at byte 255 which is within the limit
	// but its full 3-byte encoding crosses the 256-byte boundary.
	if strings.Contains(got, "€") {
		t.Fatal("truncated result must not contain the multi-byte rune that crossed the boundary")
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestSanitize_EmptyKeyNotSensitive(t *testing.T) {
	out := service.Sanitize(map[string]any{"": "value", "  ": "spaced"})
	if out[""] != "value" {
		t.Fatalf("empty key should not be redacted, got %v", out[""])
	}
	if out["  "] != "spaced" {
		t.Fatalf("whitespace-only key should not be redacted, got %v", out["  "])
	}
}

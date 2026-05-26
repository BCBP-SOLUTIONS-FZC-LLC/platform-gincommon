package service_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
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

func TestSanitize_EmptyKeyNotSensitive(t *testing.T) {
	out := service.Sanitize(map[string]any{"": "value", "  ": "spaced"})
	if out[""] != "value" {
		t.Fatalf("empty key should not be redacted, got %v", out[""])
	}
	if out["  "] != "spaced" {
		t.Fatalf("whitespace-only key should not be redacted, got %v", out["  "])
	}
}

package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

func TestRequestDurationBuckets_AreIAMTuned(t *testing.T) {
	b := metrics.RequestDurationBuckets

	if len(b) < 12 {
		t.Fatalf("expected at least 12 buckets, got %d", len(b))
	}
	if b[0] != 0.001 {
		t.Fatalf("first bucket = %v, want 1ms (0.001s)", b[0])
	}
	if b[len(b)-1] != 10 {
		t.Fatalf("last bucket = %v, want 10s IAM timeout tail", b[len(b)-1])
	}

	// Finer resolution in the sub-100ms band where most IAM checks land.
	if b[6] > 0.1 {
		t.Fatalf("expected a bucket at or before 100ms in first half, got %v", b[6])
	}

	for i := 1; i < len(b); i++ {
		if b[i] <= b[i-1] {
			t.Fatalf("buckets must be strictly increasing: %v", b)
		}
	}
}

func TestPayloadSizeBuckets_AreIAMTuned(t *testing.T) {
	b := metrics.PayloadSizeBuckets

	if len(b) < 8 {
		t.Fatalf("expected at least 8 buckets, got %d", len(b))
	}
	if b[0] != 128 {
		t.Fatalf("first bucket = %v, want 128B", b[0])
	}
	if b[len(b)-1] != 1048576 {
		t.Fatalf("last bucket = %v, want 1MiB tail", b[len(b)-1])
	}

	// Dense coverage from 512B through 16KiB for typical policy/token JSON.
	has1K, has16K := false, false
	for _, upper := range b {
		if upper == 1024 {
			has1K = true
		}
		if upper == 16384 {
			has16K = true
		}
	}
	if !has1K || !has16K {
		t.Fatalf("expected 1KiB and 16KiB buckets for IAM payloads, got %v", b)
	}

	for i := 1; i < len(b); i++ {
		if b[i] <= b[i-1] {
			t.Fatalf("buckets must be strictly increasing: %v", b)
		}
	}
}

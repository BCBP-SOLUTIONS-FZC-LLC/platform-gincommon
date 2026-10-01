package metrics_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
)

func TestRateLimitedErrorLog(t *testing.T) {
	now := time.Unix(0, 0)
	var got []string
	l := metrics.NewRateLimitedErrorLog(5*time.Minute, 2, func() time.Time { return now }, func(msg string) { got = append(got, msg) })

	errA := errors.New("collect failed")
	l.Println("error gathering metrics:", errA) // first: immediately
	l.Println("error gathering metrics:", errA) // same, within interval: dropped
	now = now.Add(4 * time.Minute)
	l.Println("error gathering metrics:", errA) // still within 5m of the last emit
	l.Println("other error")                    // distinct: immediately
	now = now.Add(time.Minute)
	l.Println("error gathering metrics:", errA) // 5m after the first emit: again
	want := []string{"error gathering metrics: collect failed", "other error", "error gathering metrics: collect failed"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("emitted %q, want %q", got, want)
	}

	// Bounded memory: a third distinct message evicts (nothing has expired,
	// so the memory is cleared) and is still emitted immediately.
	got = nil
	l.Println("third")
	l.Println("third")
	if len(got) != 1 || got[0] != "third" {
		t.Errorf("emitted %q, want one \"third\"", got)
	}
	// After the clear, earlier messages are new again.
	l.Println("other error")
	if len(got) != 2 {
		t.Errorf("emitted %q after eviction", got)
	}
	// Full again, but every entry has expired: they are pruned (not the
	// whole memory) and the new message is emitted.
	now = now.Add(10 * time.Minute)
	l.Println("fourth")
	l.Println("fourth")
	if len(got) != 3 || got[2] != "fourth" {
		t.Errorf("emitted %q after expiry", got)
	}
}

func TestRateLimitedErrorLog_Defaults(t *testing.T) {
	n := 0
	l := metrics.NewRateLimitedErrorLog(time.Hour, 0, nil, func(string) { n++ })
	l.Println("a")
	l.Println("a")
	l.Println("b")
	if n != 2 {
		t.Errorf("emitted %d, want 2", n)
	}
	metrics.NewRateLimitedErrorLog(time.Hour, 1, nil, nil).Println("nil emit must not panic")
}

package metrics

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// RateLimitedErrorLog is a promhttp.Logger (Println) that emits each distinct
// message immediately the first time, then at most once per interval. A
// broken collector fails every scrape with the same error; without the limit
// a 15-second scrape interval would write the same line 240 times an hour.
//
// It remembers at most maxKeys distinct messages: when full, entries older
// than the interval are dropped, and if none are, the memory is cleared (so
// the worst case is an extra line, never unbounded growth).
type RateLimitedErrorLog struct {
	mu       sync.Mutex
	last     map[string]time.Time
	interval time.Duration
	maxKeys  int
	now      func() time.Time
	emit     func(msg string)
}

// NewRateLimitedErrorLog returns a RateLimitedErrorLog. now nil means
// time.Now; maxKeys below 1 means 1.
func NewRateLimitedErrorLog(interval time.Duration, maxKeys int, now func() time.Time, emit func(msg string)) *RateLimitedErrorLog {
	if now == nil {
		now = time.Now
	}
	if maxKeys < 1 {
		maxKeys = 1
	}
	return &RateLimitedErrorLog{last: map[string]time.Time{}, interval: interval, maxKeys: maxKeys, now: now, emit: emit}
}

// Println formats v like fmt.Println (without the newline) and emits it
// unless the same message was emitted less than interval ago.
func (l *RateLimitedErrorLog) Println(v ...any) {
	msg := strings.TrimSuffix(fmt.Sprintln(v...), "\n")
	if l.allow(msg) && l.emit != nil {
		l.emit(msg)
	}
}

func (l *RateLimitedErrorLog) allow(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if t, ok := l.last[msg]; ok {
		if now.Sub(t) < l.interval {
			return false
		}
		l.last[msg] = now
		return true
	}
	if len(l.last) >= l.maxKeys {
		for k, t := range l.last {
			if now.Sub(t) >= l.interval {
				delete(l.last, k)
			}
		}
		if len(l.last) >= l.maxKeys {
			clear(l.last)
		}
	}
	l.last[msg] = now
	return true
}

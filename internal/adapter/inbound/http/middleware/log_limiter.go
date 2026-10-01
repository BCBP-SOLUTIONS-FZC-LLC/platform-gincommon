package middleware

import (
	"sync"
	"time"
)

// RateLimitLogInterval is the minimum interval between two "rate limited" log entries
// for the same route emitted by MetricsMiddleware.
const RateLimitLogInterval = time.Second

// routeLogLimiter samples log entries per route: at most one per interval. Keys are
// route templates (bounded by the router, plus "unknown"), so the map stays bounded.
// The mutex is only taken on the sampled path (429 responses), never per request.
type routeLogLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	routes   map[string]*routeLogState
}

type routeLogState struct {
	last       time.Time
	suppressed int64
}

func newRouteLogLimiter(interval time.Duration) *routeLogLimiter {
	return &routeLogLimiter{interval: interval, routes: make(map[string]*routeLogState)}
}

// allow reports whether an entry for route may be logged at now. When it may, it also
// returns how many entries were suppressed since the last allowed one and resets that
// count.
//
// The count is only reported on the next allowed entry: there is no timer flush, so the
// suppressed 429s of a route's final burst are never logged if no further 429 arrives on
// that route after the interval. platform_http_rate_limited_requests_total counts every 429, so
// use the metric, not the log, for totals.
func (l *routeLogLimiter) allow(route string, now time.Time) (suppressed int64, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, exists := l.routes[route]
	if !exists {
		l.routes[route] = &routeLogState{last: now}
		return 0, true
	}
	if now.Sub(st.last) < l.interval {
		st.suppressed++
		return 0, false
	}
	suppressed = st.suppressed
	st.last, st.suppressed = now, 0
	return suppressed, true
}

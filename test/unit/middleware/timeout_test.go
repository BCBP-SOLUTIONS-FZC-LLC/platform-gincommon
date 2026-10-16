package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
)

func TestTimeoutMiddleware_SetsContextDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	timeout := 10 * time.Second
	var hasDeadline bool

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(timeout))
	r.GET("/x", func(c *gin.Context) {
		_, hasDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !hasDeadline {
		t.Fatal("expected context deadline to be set with positive timeout")
	}
}

func TestTimeoutMiddleware_ZeroTimeout_NoDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var hasDeadline bool

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(0))
	r.GET("/x", func(c *gin.Context) {
		_, hasDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if hasDeadline {
		t.Fatal("expected no deadline when timeout is 0")
	}
}

func TestTimeoutMiddleware_NegativeTimeout_NoDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var hasDeadline bool

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(-1 * time.Second))
	r.GET("/x", func(c *gin.Context) {
		_, hasDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if hasDeadline {
		t.Fatal("expected no deadline when timeout is negative")
	}
}

// TestTimeoutMiddleware_Writes503WhenDeadlineExpired verifies that when the context
// deadline fires before the handler writes anything, TimeoutMiddleware emits 503.
func TestTimeoutMiddleware_Writes503WhenDeadlineExpired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	// 1 nanosecond — will always have expired before c.Next() even begins.
	r.Use(middleware.TimeoutMiddleware(1 * time.Nanosecond))
	r.GET("/slow", func(c *gin.Context) {
		// Spin until context is definitely done, then return without writing.
		<-c.Request.Context().Done()
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when timeout expires with no response", w.Code)
	}
}

func TestTimeoutMiddleware_DeadlineWithinRange(t *testing.T) {
	gin.SetMode(gin.TestMode)

	timeout := 5 * time.Second
	var deadline time.Time

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(timeout))
	r.GET("/x", func(c *gin.Context) {
		deadline, _ = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	before := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	after := time.Now()

	if deadline.Before(before.Add(timeout - time.Second)) {
		t.Fatalf("deadline %v too early; expected around %v", deadline, before.Add(timeout))
	}
	if deadline.After(after.Add(timeout + time.Second)) {
		t.Fatalf("deadline %v too late; expected around %v", deadline, after.Add(timeout))
	}
}

// TestTimeoutMiddleware_RecordsTimeoutCounter verifies that http_request_timeout_total
// is incremented when the context deadline expires before the handler writes a response.
func TestTimeoutMiddleware_RecordsTimeoutCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-timeout-counter", "test", reg)

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(1 * time.Nanosecond))
	r.GET("/slow", func(c *gin.Context) {
		<-c.Request.Context().Done()
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}

	counter, err := metrics.HttpRequestTimeoutTotal.GetMetricWithLabelValues(http.MethodGet, "/slow")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("http_request_timeout_total = %v, want 1", got)
	}
}

// TestTimeoutMiddleware_NoTimeoutCounter_WhenHandlerResponds verifies that
// http_request_timeout_total is NOT incremented when the handler responds normally.
func TestTimeoutMiddleware_NoTimeoutCounter_WhenHandlerResponds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := prometheus.NewRegistry()
	fixtures.InitMetricsWithRegisterer("svc-timeout-ok", "test", reg)

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(10 * time.Second))
	r.GET("/fast", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	counter, err := metrics.HttpRequestTimeoutTotal.GetMetricWithLabelValues(http.MethodGet, "/fast")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	if got := testutil.ToFloat64(counter); got != 0 {
		t.Fatalf("http_request_timeout_total = %v, want 0 for successful request", got)
	}
}

// TestTimeoutMiddleware_NilSafe_TimeoutCounter verifies that a nil HttpRequestTimeoutTotal
// does not panic when the timeout fires.
func TestTimeoutMiddleware_NilSafe_TimeoutCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := metrics.HttpRequestTimeoutTotal
	metrics.HttpRequestTimeoutTotal = nil
	defer func() { metrics.HttpRequestTimeoutTotal = saved }()

	r := gin.New()
	r.Use(middleware.TimeoutMiddleware(1 * time.Nanosecond))
	r.GET("/slow", func(c *gin.Context) {
		<-c.Request.Context().Done()
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

package middleware

import (
	"math"
	"net/http"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/gin-gonic/gin"
)

// MetricsMiddleware records Prometheus HTTP metrics (duration, sizes, panics, 429).
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if metrics.HttpActiveRequests == nil {
			c.Next()
			return
		}

		method := c.Request.Method

		route := label.HTTPRoute(c)
		metrics.HttpActiveRequests.WithLabelValues(route).Inc()
		defer metrics.HttpActiveRequests.WithLabelValues(route).Dec()

		defer func() {
			if r := recover(); r != nil {
				metrics.HttpPanicTotal.WithLabelValues(method, route).Inc()
				panic(r)
			}
		}()

		start := time.Now()

		c.Next()

		duration := time.Since(start).Seconds()
		code := c.Writer.Status()
		statusClass := metrics.HTTPStatusClass(code)
		errorClass, _ := metrics.RequestOutcome(code)

		metrics.HttpRequestsTotal.WithLabelValues(
			method,
			route,
			statusClass,
			errorClass,
		).Inc()

		outcomeLabels := []string{method, route, statusClass, errorClass}

		durationMetric := metrics.HttpRequestDuration.WithLabelValues(outcomeLabels...)
		reqSizeMetric := metrics.HttpRequestSizeBytes.WithLabelValues(outcomeLabels...)
		respSizeMetric := metrics.HttpResponseSizeBytes.WithLabelValues(outcomeLabels...)

		durationMetric.Observe(duration)
		reqSizeMetric.Observe(metrics.RequestBodySize(c.Request.ContentLength))
		respSizeMetric.Observe(math.Max(0, float64(c.Writer.Size())))

		if code == http.StatusTooManyRequests {
			metrics.RecordHTTPRateLimited(method, route)
		}
	}
}

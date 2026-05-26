package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

// NewRouter builds the local demo router: observability on all routes; /health and /metrics
// are public. Use gincommon.ProtectedMiddlewares on route groups in consumer services.
func NewRouter(log port.Logger, serviceName string) *gin.Engine {
	r := gin.New()
	// Disable X-Forwarded-For trust so c.ClientIP() always returns the real remote address.
	// Consumers behind a trusted proxy should call r.SetTrustedProxies with their proxy CIDRs.
	_ = r.SetTrustedProxies(nil)

	cfg := gincommon.Config{
		Logger:       log,
		ServiceName:  serviceName,
		BuildVersion: config.BuildVersion(),
	}
	for _, m := range gincommon.ObservabilityMiddlewares(cfg) {
		r.Use(m)
	}

	public := r.Group("/")
	{
		public.GET("/health", healthHandler)
		public.GET("/metrics", metricsHandler)
		if config.AppEnv() == "dev" {
			public.GET("/panic", panicHandler)
		}
	}

	return r
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func metricsHandler(c *gin.Context) {
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}

func panicHandler(c *gin.Context) {
	panic("test panic")
}

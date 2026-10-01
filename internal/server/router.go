package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

// DemoRequestTimeout is the per-request deadline the demo router sets through
// gincommon.Config.RequestTimeout (the http.Server in cmd/server leaves WriteTimeout
// unset and relies on it).
const DemoRequestTimeout = 30 * time.Second

// NewRouter builds the local demo router: observability on all routes; /health and /metrics
// are public. Use gincommon.ProtectedMiddlewares on route groups in consumer services.
func NewRouter(log port.Logger, serviceName string) *gin.Engine {
	r := gin.New()
	// Disable X-Forwarded-For trust so c.ClientIP() always returns the real remote address.
	// Consumers behind a trusted proxy should call r.SetTrustedProxies with their proxy CIDRs.
	_ = r.SetTrustedProxies(nil)

	// Domain/Environment come from OBSERVABILITY_DOMAIN / APP_ENV, so the demo
	// emits platform_* metrics when OBSERVABILITY_DOMAIN is set in .env.
	cfg := gincommon.Config{
		Logger:       log,
		ServiceName:  serviceName,
		BuildVersion: config.BuildVersion(),
		// Innermost observability handler: a handler that overruns the deadline
		// without responding gets a 503 that metrics, logs and spans all record.
		RequestTimeout: DemoRequestTimeout,
	}
	for _, m := range gincommon.ObservabilityMiddlewares(cfg) {
		r.Use(m)
	}

	public := r.Group("/")
	{
		public.GET("/health", healthHandler)
		public.GET("/metrics", gin.WrapH(gincommon.MetricsHandler()))
		if config.AppEnv() == "dev" {
			public.GET("/panic", panicHandler)
		}
	}

	return r
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func panicHandler(c *gin.Context) {
	panic("test panic")
}

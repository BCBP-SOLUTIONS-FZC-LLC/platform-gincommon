package gincommon

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
)

// Config configures the default middleware stacks for a Gin service.
//
// Identity headers (x-user-id, x-tenant-id, x-tenant-roles) must be set by a trusted API
// gateway. ProtectedMiddlewares validates format but does not cryptographically verify identity.
type Config struct {
	// Logger emits structured logs (http_request, panics). Nil disables all logging (no-op).
	Logger port.Logger
	// ServiceName is the Prometheus const label and OTEL service.name.
	ServiceName string
	// BuildVersion is the Prometheus/OTEL version label; empty uses BUILD_VERSION env.
	BuildVersion string
	// Tracing overrides OTEL_* env when non-nil fields are set.
	Tracing *TracingOptions
}

// TracingOptions overrides OpenTelemetry settings for ObservabilityMiddlewares.
type TracingOptions struct {
	// Endpoint is the OTLP/gRPC collector host:port (e.g. localhost:4317).
	Endpoint string
	// Insecure enables plaintext OTLP when non-nil.
	Insecure *bool
	// SampleRatio is in [0,1]; 1 means always sample when non-nil.
	SampleRatio *float64
	// EnableBaggage enables W3C baggage propagation when non-nil.
	EnableBaggage *bool
}

// ObservabilityMiddlewares returns panic recovery, tracing, metrics, and logging (no auth).
// It panics if cfg.ServiceName is empty, as an empty service name produces invalid Prometheus labels.
func ObservabilityMiddlewares(cfg Config) []gin.HandlerFunc {
	if cfg.ServiceName == "" {
		panic("gincommon.Config.ServiceName is required")
	}
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)

	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	telemetry := resolveTelemetry(cfg)
	EnsureInitTelemetry(cfg.ServiceName, version, telemetry)

	return []gin.HandlerFunc{
		middleware.PanicRecoveryMiddleware(cfg.Logger),
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware(cfg.ServiceName),
		middleware.CorrelationHeadersMiddleware(),
		middleware.MetricsMiddleware(cfg.Logger),
		middleware.LoggingMiddleware(cfg.Logger),
	}
}

// ProtectedMiddlewares returns auth and tenant context middleware.
func ProtectedMiddlewares(cfg Config) []gin.HandlerFunc {
	ctxSvc := service.NewContextService()
	return []gin.HandlerFunc{
		middleware.RequireAuth(),
		middleware.ContextMiddleware(ctxSvc),
	}
}

// DefaultMiddlewares is observability plus protected middleware (full stack).
func DefaultMiddlewares(cfg Config) []gin.HandlerFunc {
	mw := ObservabilityMiddlewares(cfg)
	mw = append(mw, ProtectedMiddlewares(cfg)...)
	return mw
}

// TimeoutMiddleware sets a per-request context deadline.
// Insert before ObservabilityMiddlewares or ProtectedMiddlewares in the router.
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return middleware.TimeoutMiddleware(timeout)
}

func resolveTelemetry(cfg Config) otel.TelemetryConfig {
	tc := otel.TelemetryConfig{
		Endpoint:      config.OtelExporterEndpoint(),
		Insecure:      config.OtelExporterInsecure(),
		SampleRatio:   config.OtelTraceSampleRatio(),
		EnableBaggage: config.OtelEnableBaggage(),
	}
	if cfg.Tracing == nil {
		return tc
	}
	if cfg.Tracing.Endpoint != "" {
		tc.Endpoint = cfg.Tracing.Endpoint
	}
	if cfg.Tracing.Insecure != nil {
		tc.Insecure = *cfg.Tracing.Insecure
	}
	if cfg.Tracing.SampleRatio != nil {
		tc.SampleRatio = *cfg.Tracing.SampleRatio
	}
	if cfg.Tracing.EnableBaggage != nil {
		tc.EnableBaggage = *cfg.Tracing.EnableBaggage
	}
	return tc
}

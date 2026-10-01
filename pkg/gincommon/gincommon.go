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
	// Domain is the registry domain (iam, workflow, billing, ...) of the service.
	// Required: empty uses OBSERVABILITY_DOMAIN, and ObservabilityMiddlewares
	// panics when neither is set or the registry does not define the domain.
	// It is the domain const label (with service and environment) on every
	// gincommon collector and every NewCounterVec / NewShared* collector, per
	// the Enterprise Platform Observability Standard.
	Domain string
	// Environment is the environment label (local, dev, test, staging, prod),
	// trimmed and lowercased. Empty uses APP_ENV, then ENVIRONMENT, the
	// precedence platform-events and platform-pgcommon use, so every library in
	// the process labels the same environment. Required;
	// ObservabilityMiddlewares panics on any other value (e.g. "production").
	Environment string
	// RequestTimeout, when positive, appends TimeoutMiddleware(RequestTimeout) as the
	// innermost ObservabilityMiddlewares handler, so metrics, the access log and the
	// server span record the same 503 the client receives on a timeout. Zero or
	// negative disables it. See TimeoutMiddleware for the semantics.
	RequestTimeout time.Duration
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
	// IgnoreRemoteParentSampled, when non-nil, overrides
	// OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED: true re-samples spans whose
	// remote parent is sampled with SampleRatio, so callers cannot force 100%
	// sampling with a sampled traceparent. Enable it on edge services.
	IgnoreRemoteParentSampled *bool
}

// ObservabilityMiddlewares returns panic recovery, tracing, metrics, and logging (no auth).
// It panics if cfg.ServiceName is empty, as an empty service name produces invalid Prometheus labels,
// and if the observability identity (Domain, Environment) is missing or rejected by the registry.
// Metrics initialization happens once per process: a later call must use the same
// ServiceName, Domain and Environment (a different identity is a configuration error
// reported by metrics initialization, not silently ignored).
//
// Order: PanicRecovery, RequestID, Tracing, CorrelationHeaders, Metrics, Logging and,
// when cfg.RequestTimeout > 0, Timeout (innermost).
func ObservabilityMiddlewares(cfg Config) []gin.HandlerFunc {
	if cfg.ServiceName == "" {
		panic("gincommon.Config.ServiceName is required")
	}
	metrics.MustInitFromConfig(metrics.Options{
		ServiceName:  cfg.ServiceName,
		BuildVersion: cfg.BuildVersion,
		Domain:       cfg.Domain,
		Environment:  cfg.Environment,
	})

	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	telemetry := resolveTelemetry(cfg)
	ensureInitTelemetry(cfg.ServiceName, version, telemetry)

	mw := []gin.HandlerFunc{
		middleware.PanicRecoveryMiddleware(cfg.Logger),
		middleware.RequestIDMiddleware(),
		middleware.TracingMiddleware(cfg.ServiceName),
		middleware.CorrelationHeadersMiddleware(),
		middleware.MetricsMiddleware(cfg.Logger),
		middleware.LoggingMiddleware(cfg.Logger),
	}
	if cfg.RequestTimeout > 0 {
		mw = append(mw, middleware.TimeoutMiddleware(cfg.RequestTimeout))
	}
	return mw
}

// ProtectedMiddlewares returns auth and tenant context middleware: RequireAuth then
// ContextMiddleware. cfg.Logger (nil-safe) receives a warn-level "auth: request rejected"
// audit entry for every 401, with the claimed identity sanitized and truncated.
func ProtectedMiddlewares(cfg Config) []gin.HandlerFunc {
	ctxSvc := service.NewContextService()
	return []gin.HandlerFunc{
		middleware.RequireAuth(cfg.Logger),
		middleware.ContextMiddleware(ctxSvc, cfg.Logger),
	}
}

// DefaultMiddlewares is observability plus protected middleware (full stack).
func DefaultMiddlewares(cfg Config) []gin.HandlerFunc {
	mw := ObservabilityMiddlewares(cfg)
	mw = append(mw, ProtectedMiddlewares(cfg)...)
	return mw
}

// TimeoutMiddleware sets a per-request context deadline. Zero or negative is a no-op.
//
// When the deadline expires (context.DeadlineExceeded; a client disconnect is not a
// timeout) and the handler returned without writing a response, it writes a 503 JSON
// ErrorResponse and increments platform_http_request_timeouts_total. It does not
// interrupt the handler goroutine: handlers must honour c.Request.Context() (pass it to
// DB and HTTP calls) for the deadline to have any effect.
//
// Prefer Config.RequestTimeout. When using this directly, register it AFTER
// ObservabilityMiddlewares (and before ProtectedMiddlewares/handlers), never before:
// the observability middleware reads the response status when its c.Next() returns, so
// only an inner TimeoutMiddleware's 503 is what metrics, logs and spans record.
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return middleware.TimeoutMiddleware(timeout)
}

func resolveTelemetry(cfg Config) otel.TelemetryConfig {
	return telemetryConfig(cfg.Domain, cfg.Environment, cfg.Logger, cfg.Tracing)
}

// Package grpccommon provides gRPC server interceptors for observability, authentication,
// and authorization. Authentication and tracing use the same platform conventions as pkg/gincommon.
package grpccommon

import (
	"context"

	mw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	otelsetup "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"google.golang.org/grpc"
)

// Config identifies the service for Prometheus const labels and OTel trace scopes.
type Config struct {
	// ServiceName is required for Prometheus const labels (panics if empty in Full* functions).
	ServiceName string
	// BuildVersion is the Prometheus version label; empty uses BUILD_VERSION env, then "unknown".
	BuildVersion string
	// Logger is used by logging and panic recovery interceptors; nil disables logging.
	Logger port.Logger
	// Tracing overrides OTEL_* env vars when non-nil fields are set.
	// If nil, values are read from environment variables.
	Tracing *TracingOptions
	// Domain and Environment are the required observability identity, exactly
	// as in gincommon.Config: Domain (else OBSERVABILITY_DOMAIN) and Environment
	// (else APP_ENV, then ENVIRONMENT) become the domain/environment const
	// labels; the interceptor constructors panic when they are missing or
	// rejected by the registry.
	Domain      string
	Environment string
}

func initMetrics(cfg Config) {
	metrics.MustInitFromConfig(metrics.Options{
		ServiceName:  cfg.ServiceName,
		BuildVersion: cfg.BuildVersion,
		Domain:       cfg.Domain,
		Environment:  cfg.Environment,
	})
}

// TracingOptions overrides OpenTelemetry settings for gRPC observability interceptors.
type TracingOptions struct {
	// Endpoint is the OTLP/gRPC collector host:port (e.g. localhost:4317).
	Endpoint string
	// Insecure enables plaintext OTLP when non-nil.
	Insecure *bool
	// SampleRatio is in [0,1]; 1 means always sample.
	SampleRatio *float64
	// EnableBaggage enables W3C baggage propagation when non-nil.
	EnableBaggage *bool
	// IgnoreRemoteParentSampled, when non-nil, overrides
	// OTEL_TRACES_IGNORE_REMOTE_PARENT_SAMPLED (see gincommon.TracingOptions).
	IgnoreRemoteParentSampled *bool
}

// resolveGRPCTelemetry builds a TelemetryConfig from env defaults, overridden by cfg.Tracing.
func resolveGRPCTelemetry(cfg Config) otelsetup.TelemetryConfig {
	tc := otelsetup.EnvConfig(cfg.Domain, cfg.Environment, cfg.Logger)
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
	if cfg.Tracing.IgnoreRemoteParentSampled != nil {
		tc.IgnoreRemoteParentSampled = *cfg.Tracing.IgnoreRemoteParentSampled
	}
	return tc
}

// ObservabilityUnaryInterceptors returns the observability interceptor chain for unary RPCs:
// panic recovery → request ID → tracing → metrics → logging → error sanitizer.
//
// The error sanitizer (innermost, so every signal records what the client gets) keeps
// status errors (only the inner status of a wrapped one, never the wrappers' text),
// gives context errors a fixed message, and turns any other handler error into
// Internal "internal server error", logged as an error-level "unclassified error"
// entry with the error-chain types but never err.Error(). grpc-go would otherwise
// send a plain error to the client as Unknown with its full text.
// Panics if cfg.ServiceName is empty.
func ObservabilityUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	if cfg.ServiceName == "" {
		panic("grpccommon.Config.ServiceName is required")
	}
	initMetrics(cfg)
	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	otelsetup.EnsureInitTelemetry(cfg.ServiceName, version, resolveGRPCTelemetry(cfg))
	return []grpc.UnaryServerInterceptor{
		mw.UnaryPanicRecoveryInterceptor(cfg.Logger),
		mw.UnaryRequestIDInterceptor(),
		mw.UnaryTracingInterceptor(cfg.ServiceName),
		mw.UnaryMetricsInterceptor(),
		mw.UnaryLoggingInterceptor(cfg.Logger),
		mw.UnaryErrorSanitizerInterceptor(cfg.Logger, cfg.ServiceName),
	}
}

// ObservabilityStreamInterceptors returns the observability interceptor chain for streaming RPCs,
// in the same order as ObservabilityUnaryInterceptors (including the error sanitizer).
// Panics if cfg.ServiceName is empty.
func ObservabilityStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	if cfg.ServiceName == "" {
		panic("grpccommon.Config.ServiceName is required")
	}
	initMetrics(cfg)
	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	otelsetup.EnsureInitTelemetry(cfg.ServiceName, version, resolveGRPCTelemetry(cfg))
	return []grpc.StreamServerInterceptor{
		mw.StreamPanicRecoveryInterceptor(cfg.Logger),
		mw.StreamRequestIDInterceptor(),
		mw.StreamTracingInterceptor(cfg.ServiceName),
		mw.StreamMetricsInterceptor(),
		mw.StreamLoggingInterceptor(cfg.Logger),
		mw.StreamErrorSanitizerInterceptor(cfg.Logger, cfg.ServiceName),
	}
}

// ProtectedUnaryInterceptors returns auth + context interceptors for unary RPCs.
// Place after ObservabilityUnaryInterceptors.
func ProtectedUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	ctxSvc := service.NewContextService()
	return []grpc.UnaryServerInterceptor{
		mw.UnaryRequireAuthInterceptor(),
		mw.UnaryContextInterceptor(ctxSvc),
	}
}

// ProtectedStreamInterceptors returns auth + context interceptors for streaming RPCs.
func ProtectedStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	ctxSvc := service.NewContextService()
	return []grpc.StreamServerInterceptor{
		mw.StreamRequireAuthInterceptor(),
		mw.StreamContextInterceptor(ctxSvc),
	}
}

// FullUnaryInterceptors returns the complete interceptor stack for unary RPCs:
// panic recovery → request ID → tracing → metrics → logging → error sanitizer → auth → context —
// i.e. ObservabilityUnaryInterceptors followed by ProtectedUnaryInterceptors.
//
// Logging wraps auth, so calls rejected by RequireAuth (codes.Unauthenticated)
// still produce a grpc_request log entry at Warn. Authenticated calls still carry
// user_id and tenant_id: the context interceptor hands the verified RequestContext
// back to the logging interceptor after it is built.
// Panics if cfg.ServiceName is empty.
func FullUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	return append(ObservabilityUnaryInterceptors(cfg), ProtectedUnaryInterceptors(cfg)...)
}

// FullStreamInterceptors returns the complete interceptor stack for streaming RPCs:
// panic recovery → request ID → tracing → metrics → logging → error sanitizer → auth → context.
// Same ordering and logging semantics as FullUnaryInterceptors.
// Panics if cfg.ServiceName is empty.
func FullStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	return append(ObservabilityStreamInterceptors(cfg), ProtectedStreamInterceptors(cfg)...)
}

// DefaultUnaryInterceptors returns ObservabilityUnaryInterceptors for backward compatibility.
func DefaultUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	return ObservabilityUnaryInterceptors(cfg)
}

// DefaultStreamInterceptors returns ObservabilityStreamInterceptors for backward compatibility.
func DefaultStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	return ObservabilityStreamInterceptors(cfg)
}

// RequirePermission returns a unary interceptor that authorizes using action+resource.
// Requires ProtectedUnaryInterceptors (RequestContext in Go context).
func RequirePermission(action, resource string, auth port.Authorizer) grpc.UnaryServerInterceptor {
	return mw.UnaryAuthZInterceptor(auth, action, resource, nil)
}

// RequirePermissionWithLogger is like RequirePermission but logs access denials.
func RequirePermissionWithLogger(action, resource string, auth port.Authorizer, logger port.Logger) grpc.UnaryServerInterceptor {
	return mw.UnaryAuthZInterceptor(auth, action, resource, logger)
}

// RequireStreamPermission returns a stream interceptor that authorizes using action+resource.
// Requires ProtectedStreamInterceptors (RequestContext in stream context).
func RequireStreamPermission(action, resource string, auth port.Authorizer) grpc.StreamServerInterceptor {
	return mw.StreamAuthZInterceptor(auth, action, resource, nil)
}

// RequireStreamPermissionWithLogger is like RequireStreamPermission but logs access denials.
func RequireStreamPermissionWithLogger(action, resource string, auth port.Authorizer, logger port.Logger) grpc.StreamServerInterceptor {
	return mw.StreamAuthZInterceptor(auth, action, resource, logger)
}

// NewDemoRBAC returns a reference port.Authorizer for demos and tests only — not for production IAM.
// Logic: "read" is always permitted; "delete" requires the "admin" role; all other actions are denied.
func NewDemoRBAC() *service.RBACService {
	return service.NewRBACService()
}

// RequestContextFromContext retrieves the domain.RequestContext from a gRPC handler context.
func RequestContextFromContext(ctx context.Context) (*domain.RequestContext, bool) {
	return domain.RequestContextFromGoContext(ctx)
}

// RequestIDFromContext retrieves the request correlation ID from a gRPC handler context.
func RequestIDFromContext(ctx context.Context) string {
	return mw.RequestIDFromContext(ctx)
}

// logSyncer is the optional Sync() error capability exposed by buffered loggers
// (e.g. the Zap-backed port.Logger). Shutdown flushes it if implemented.
type logSyncer interface {
	Sync() error
}

// Shutdown performs graceful telemetry cleanup for a gRPC service. Call it from main()
// during shutdown (e.g. on SIGTERM) AFTER grpc.Server.GracefulStop has returned:
// it stops the active OpenTelemetry TracerProvider — flushing any buffered spans to the
// collector — and, when logger implements Sync() error (the Zap logger does), flushes
// buffered log entries. Passing a nil logger flushes tracing only.
//
// It returns the logger's Sync error (if any) so callers can surface flush failures;
// the tracing provider is always flushed regardless of the logger result.
func Shutdown(logger port.Logger) error {
	otelsetup.Shutdown()
	if s, ok := logger.(logSyncer); ok {
		return s.Sync()
	}
	return nil
}

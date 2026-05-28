// Package grpccommon provides gRPC server interceptors for observability, authentication,
// and authorization. Authentication and tracing use the same platform conventions as pkg/gincommon.
package grpccommon

import (
	"context"

	mw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	otelsetup "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/port"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/core/service"
	"google.golang.org/grpc"
)

// Config identifies the service for Prometheus const labels and OTel trace scopes.
type Config struct {
	// ServiceName is required for Prometheus const labels (panics if empty in Full* functions).
	ServiceName string
	// BuildVersion is the Prometheus version label; empty becomes "unknown".
	BuildVersion string
	// Logger is used by logging and panic recovery interceptors; nil disables logging.
	Logger port.Logger
	// Tracing overrides OTEL_* env vars when non-nil fields are set.
	// If nil, values are read from environment variables.
	Tracing *TracingOptions
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
}

// resolveGRPCTelemetry builds a TelemetryConfig from env defaults, overridden by cfg.Tracing.
func resolveGRPCTelemetry(cfg Config) otelsetup.TelemetryConfig {
	tc := otelsetup.TelemetryConfig{
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

// ObservabilityUnaryInterceptors returns the observability interceptor chain for unary RPCs:
// panic recovery → request ID → tracing → metrics → logging.
// Panics if cfg.ServiceName is empty.
func ObservabilityUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	if cfg.ServiceName == "" {
		panic("grpccommon.Config.ServiceName is required")
	}
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)
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
	}
}

// ObservabilityStreamInterceptors returns the observability interceptor chain for streaming RPCs.
// Panics if cfg.ServiceName is empty.
func ObservabilityStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	if cfg.ServiceName == "" {
		panic("grpccommon.Config.ServiceName is required")
	}
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)
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
// panic recovery → request ID → tracing → metrics → auth → context → logging.
//
// Logging is placed last (innermost) so its deferred log entry runs with the
// fully-enriched context — including user_id and tenant_id from ContextInterceptor.
// ObservabilityUnaryInterceptors places logging before auth and context, which means
// those fields would always be absent from log entries when using that helper alone.
func FullUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	if cfg.ServiceName == "" {
		panic("grpccommon.Config.ServiceName is required")
	}
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)
	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	otelsetup.EnsureInitTelemetry(cfg.ServiceName, version, resolveGRPCTelemetry(cfg))
	ctxSvc := service.NewContextService()
	return []grpc.UnaryServerInterceptor{
		mw.UnaryPanicRecoveryInterceptor(cfg.Logger),
		mw.UnaryRequestIDInterceptor(),
		mw.UnaryTracingInterceptor(cfg.ServiceName),
		mw.UnaryMetricsInterceptor(),
		mw.UnaryRequireAuthInterceptor(),
		mw.UnaryContextInterceptor(ctxSvc),
		mw.UnaryLoggingInterceptor(cfg.Logger),
	}
}

// FullStreamInterceptors returns the complete interceptor stack for streaming RPCs:
// panic recovery → request ID → tracing → metrics → auth → context → logging.
//
// Logging is placed last (innermost) so its deferred log entry sees the enriched
// stream context from StreamContextInterceptor (containing user_id and tenant_id).
func FullStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	if cfg.ServiceName == "" {
		panic("grpccommon.Config.ServiceName is required")
	}
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)
	version := cfg.BuildVersion
	if version == "" {
		version = config.BuildVersion()
	}
	otelsetup.EnsureInitTelemetry(cfg.ServiceName, version, resolveGRPCTelemetry(cfg))
	ctxSvc := service.NewContextService()
	return []grpc.StreamServerInterceptor{
		mw.StreamPanicRecoveryInterceptor(cfg.Logger),
		mw.StreamRequestIDInterceptor(),
		mw.StreamTracingInterceptor(cfg.ServiceName),
		mw.StreamMetricsInterceptor(),
		mw.StreamRequireAuthInterceptor(),
		mw.StreamContextInterceptor(ctxSvc),
		mw.StreamLoggingInterceptor(cfg.Logger),
	}
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

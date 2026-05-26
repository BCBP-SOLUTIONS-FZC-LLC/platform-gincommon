// Package grpccommon provides gRPC server interceptors for observability (metrics).
// Authentication, authorization, and tracing for gRPC must be added by the consuming service
// (e.g. interceptors from your gateway or service mesh, or custom unary interceptors).
package grpccommon

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"google.golang.org/grpc"
)

// Config identifies the service for Prometheus const labels on gRPC metrics.
type Config struct {
	// ServiceName is the Prometheus service label.
	ServiceName string
	// BuildVersion is the Prometheus version label; empty becomes "unknown" in metrics.Init.
	BuildVersion string
}

// DefaultUnaryInterceptors returns Prometheus unary server interceptors (metrics only).
func DefaultUnaryInterceptors(cfg Config) []grpc.UnaryServerInterceptor {
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)
	return []grpc.UnaryServerInterceptor{
		middleware.UnaryMetricsInterceptor(),
	}
}

// DefaultStreamInterceptors returns Prometheus stream server interceptors (metrics only).
func DefaultStreamInterceptors(cfg Config) []grpc.StreamServerInterceptor {
	metrics.Init(cfg.ServiceName, cfg.BuildVersion)
	return []grpc.StreamServerInterceptor{
		middleware.StreamMetricsInterceptor(),
	}
}

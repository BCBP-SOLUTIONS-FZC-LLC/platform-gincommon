package otel

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/port"

// TelemetryConfig controls OTLP export, sampling, resource identity and
// propagation for InitTelemetry / EnsureInitTelemetry.
type TelemetryConfig struct {
	// Endpoint is the OTLP/gRPC collector address (host:port). Empty skips exporter setup.
	Endpoint string
	// Insecure uses plaintext gRPC when true.
	Insecure bool
	// SampleRatio is in [0,1]; 1 means always sample. It applies to root spans
	// (and, with IgnoreRemoteParentSampled, to spans with a sampled remote parent).
	SampleRatio float64
	// EnableBaggage adds W3C baggage to the text map propagator.
	EnableBaggage bool
	// IgnoreRemoteParentSampled re-samples spans whose remote parent is marked
	// sampled with SampleRatio instead of always sampling them, so an external
	// caller cannot force 100% sampling by sending a sampled traceparent.
	// Enable it on edge services (or have the gateway re-issue traceparent).
	IgnoreRemoteParentSampled bool
	// Domain is the observability domain; it becomes the service.namespace
	// resource attribute when non-empty.
	Domain string
	// Environment becomes the deployment.environment.name resource attribute
	// when non-empty.
	Environment string
	// Logger receives initialisation warnings, the effective configuration and
	// OpenTelemetry SDK errors. Nil routes them to stderr (log.Printf).
	Logger port.Logger
	// Provisional marks the service name as a placeholder (no service name was
	// configured). A later EnsureInitTelemetry replaces a provisional provider
	// instead of keeping it, so an explicitly configured ServiceName wins.
	Provisional bool
}

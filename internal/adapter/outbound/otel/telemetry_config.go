package otel

// TelemetryConfig controls OTLP export and propagation for InitTelemetry.
type TelemetryConfig struct {
	// Endpoint is the OTLP/gRPC collector address (host:port). Empty skips exporter setup.
	Endpoint string
	// Insecure uses plaintext gRPC when true.
	Insecure bool
	// SampleRatio is in [0,1]; 1 means always sample.
	SampleRatio float64
	// EnableBaggage adds W3C baggage to the text map propagator.
	EnableBaggage bool
}

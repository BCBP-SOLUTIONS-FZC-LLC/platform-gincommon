package port

// Logger is a structured logging port used by HTTP and gRPC middleware.
// Fatal is intentionally excluded: calling os.Exit from middleware bypasses all
// deferred cleanup (OTel flush, graceful shutdown, connection draining).
// Fatal-level decisions belong in main(), not in shared middleware.
type Logger interface {
	Debug(msg string, fields map[string]interface{})
	Info(msg string, fields map[string]interface{})
	Warn(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
}

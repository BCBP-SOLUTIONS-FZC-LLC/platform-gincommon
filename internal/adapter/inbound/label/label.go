package label

import "github.com/gin-gonic/gin"

// Unknown is used when a request has no registered route template (HTTP) or method (gRPC).
const Unknown = "unknown"

// HTTPRoute returns the Gin route template (e.g. "/api/items/:id") from FullPath when set.
// Returns Unknown for unregistered routes — never the raw URL path, which would cause
// unbounded Prometheus label cardinality from scanner probes and path-parameter abuse.
func HTTPRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return Unknown
}

// GRPCMethod returns the gRPC full method name, or Unknown when unset.
func GRPCMethod(fullMethod string) string {
	if fullMethod == "" {
		return Unknown
	}
	return fullMethod
}

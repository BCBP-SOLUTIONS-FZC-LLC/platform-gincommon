package label

import "github.com/gin-gonic/gin"

// Unknown is used when a request has no registered route template (HTTP) or method (gRPC).
const Unknown = "unknown"

// HTTPRoute returns the Gin route template from FullPath when set; otherwise the
// request URL path (e.g. unregistered /metrics scrapes). Unknown only if both are empty.
func HTTPRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	if path := c.Request.URL.Path; path != "" {
		return path
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

package label

import (
	"regexp"
	"sync"

	"github.com/gin-gonic/gin"
)

// Unknown is used when a request has no registered route template (HTTP) or method (gRPC).
const Unknown = "unknown"

// MaxGRPCMethodLen caps the length of a gRPC method label value. Longer names map to Unknown.
const MaxGRPCMethodLen = 256

// grpcMethodPattern is the shape of a well-formed gRPC full method name
// ("/package.Service/Method"). It is a strict subset of the registry's
// grpc_method label pattern (^(unknown|/?[A-Za-z0-9_.]+/[A-Za-z0-9_]+)$), so
// every value GRPCMethod emits satisfies the observability registry.
var grpcMethodPattern = regexp.MustCompile(`^/[A-Za-z0-9_.]+/[A-Za-z0-9_]+$`)

var (
	knownMu      sync.RWMutex
	knownMethods map[string]struct{} // nil = no allow-list registered (pattern-only mode)
)

// HTTPRoute returns the Gin route template (e.g. "/api/items/:id") from FullPath when set.
// Returns Unknown for unregistered routes — never the raw URL path, which would cause
// unbounded Prometheus label cardinality from scanner probes and path-parameter abuse.
func HTTPRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return Unknown
}

// GRPCMethod returns a bounded gRPC method label for fullMethod.
//
// The full method name is client-controlled whenever a server installs
// grpc.UnknownServiceHandler (or a proxy forwards arbitrary paths), so it is
// never used verbatim: names that are empty, longer than MaxGRPCMethodLen, or not
// shaped like "/package.Service/Method" map to Unknown. Once RegisterGRPCMethods
// has been called, names outside the registered set also map to Unknown, which
// bounds the label to the methods the server actually serves.
func GRPCMethod(fullMethod string) string {
	if fullMethod == "" || len(fullMethod) > MaxGRPCMethodLen || !grpcMethodPattern.MatchString(fullMethod) {
		return Unknown
	}
	knownMu.RLock()
	defer knownMu.RUnlock()
	if knownMethods != nil {
		if _, ok := knownMethods[fullMethod]; !ok {
			return Unknown
		}
	}
	return fullMethod
}

// RegisterGRPCMethods adds fullMethods to the process-wide allow-list used by
// GRPCMethod. The first call switches GRPCMethod from pattern-only validation to
// allow-list mode; later calls extend the list (e.g. several servers in one process).
//
// The list is process-wide: once it is non-empty, methods of any server whose
// methods were not registered map to Unknown, and so do services registered on a
// server after its methods were snapshotted. See grpccommon.RegisterKnownMethods.
func RegisterGRPCMethods(fullMethods ...string) {
	if len(fullMethods) == 0 {
		return // nothing to allow; an empty list must not map every method to Unknown
	}
	knownMu.Lock()
	defer knownMu.Unlock()
	if knownMethods == nil {
		knownMethods = make(map[string]struct{}, len(fullMethods))
	}
	for _, m := range fullMethods {
		knownMethods[m] = struct{}{}
	}
}

// ResetGRPCMethods clears the allow-list, restoring pattern-only validation. For tests.
func ResetGRPCMethods() {
	knownMu.Lock()
	defer knownMu.Unlock()
	knownMethods = nil
}

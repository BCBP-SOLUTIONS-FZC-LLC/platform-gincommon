package grpccommon

import (
	"time"

	mw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// ServiceInfoProvider is implemented by *grpc.Server (GetServiceInfo).
type ServiceInfoProvider interface {
	GetServiceInfo() map[string]grpc.ServiceInfo
}

// RegisterKnownMethods bounds the grpc_method metric label and the server span name
// to the methods srv actually serves. Call it once after every service (including
// RegisterHealth) is registered on srv and before Serve:
//
//	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpccommon.FullUnaryInterceptors(cfg)...))
//	pb.RegisterFooServer(srv, impl)
//	grpccommon.RegisterHealth(srv)
//	grpccommon.RegisterKnownMethods(srv)
//
// Afterwards any other method — e.g. paths reaching a grpc.UnknownServiceHandler —
// is recorded as "unknown" (span: rpc.method="_OTHER" plus rpc.method_original).
// Without it, method names are still validated against /package.Service/Method and
// a 256-byte cap, so malformed names always map to "unknown".
//
// WARNING — the allow-list is PROCESS-WIDE, not per server, and the first call
// switches every server in the process into allow-list mode. Failure modes:
//
//   - Multiple servers in one process: call RegisterKnownMethods for EVERY server.
//     After the first call, methods of a server that was never passed in are
//     recorded as "unknown" (the list is additive, so registering each is safe).
//   - Call order: call it only after ALL services, including RegisterHealth, are
//     registered on srv. It snapshots srv.GetServiceInfo() at call time; a service
//     registered later is NOT picked up and its methods are recorded as "unknown"
//     until RegisterKnownMethods is called again.
//   - "unknown" spikes on a dashboard after a deploy almost always mean one of the
//     two mistakes above, not client abuse.
func RegisterKnownMethods(srv ServiceInfoProvider) {
	var methods []string
	for svc, info := range srv.GetServiceInfo() {
		for _, m := range info.Methods {
			methods = append(methods, "/"+svc+"/"+m.Name)
		}
	}
	label.RegisterGRPCMethods(methods...)
}

// RegisterHealth registers the standard grpc.health.v1.Health service on srv and
// returns its server, initially SERVING for the overall ("") service. Use the
// returned server's SetServingStatus to report per-service health and Shutdown
// during graceful stop so clients see NOT_SERVING before the listener closes.
// Health probes carry no identity metadata, so the auth and context interceptors
// (Protected*/Full* chains) let exactly /grpc.health.v1.Health/Check, /Watch and
// /List through without identity (no other path under that prefix) —
// the gRPC counterpart of registering gincommon.HealthHandler before the protected
// group. Observability (logs, metrics, spans) still applies to probes.
func RegisterHealth(srv grpc.ServiceRegistrar) *health.Server {
	h := health.NewServer()
	healthpb.RegisterHealthServer(srv, h)
	return h
}

// TimeoutUnaryInterceptor caps each unary RPC's context deadline at d — the gRPC
// counterpart of gincommon.TimeoutMiddleware. An earlier client deadline is kept;
// d applies when the call has no deadline or a later one. Zero or negative d is a
// no-op. Place it after the observability (or Full) interceptors, innermost, so metrics, logs
// and spans record the DeadlineExceeded the client receives rather than whatever an
// abandoned handler returns later. Unary handlers must honour ctx.
func TimeoutUnaryInterceptor(d time.Duration) grpc.UnaryServerInterceptor {
	return mw.UnaryTimeoutInterceptor(d)
}

// TimeoutStreamInterceptor is the streaming counterpart of TimeoutUnaryInterceptor.
// The handler runs in a goroutine; at the deadline the interceptor returns
// DeadlineExceeded and the abandoned handler's later result (or a panic after the
// deadline, which is only logged to stderr) is discarded. Place it after the observability (or Full) interceptors, innermost, so metrics, logs
// and spans record the DeadlineExceeded the client receives rather than whatever an
// abandoned handler returns later.
func TimeoutStreamInterceptor(d time.Duration) grpc.StreamServerInterceptor {
	return mw.StreamTimeoutInterceptor(d)
}

// UnaryClientInterceptor propagates the caller's W3C trace context (global OTel
// propagator) and x-request-id into outgoing metadata on unary client calls. It
// forwards no identity, so it is safe on any connection:
//
//	grpc.NewClient(addr, grpc.WithChainUnaryInterceptor(grpccommon.UnaryClientInterceptor()))
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return mw.UnaryClientPropagationInterceptor()
}

// StreamClientInterceptor is the streaming counterpart of UnaryClientInterceptor.
func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return mw.StreamClientPropagationInterceptor()
}

// UnaryClientIdentityInterceptor forwards the verified caller identity (x-user-id,
// x-tenant-id, x-tenant-roles from the RequestContext) on unary client calls. It is
// opt-in on purpose: install it only on connections to trusted internal services
// that accept gateway-style identity metadata — never on calls leaving the trust
// boundary. Calls without a RequestContext pass through unchanged. Combine it with
// UnaryClientInterceptor for trace and request-ID propagation.
func UnaryClientIdentityInterceptor() grpc.UnaryClientInterceptor {
	return mw.UnaryClientIdentityInterceptor()
}

// StreamClientIdentityInterceptor is the streaming counterpart of UnaryClientIdentityInterceptor.
func StreamClientIdentityInterceptor() grpc.StreamClientInterceptor {
	return mw.StreamClientIdentityInterceptor()
}

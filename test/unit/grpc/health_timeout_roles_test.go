package grpc_test

// Tests for the exact-match health exemption, StreamTimeoutInterceptor's
// handler-goroutine deadline enforcement, and strict x-tenant-roles parsing.

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
)

// healthNearMisses look like health methods but are not one of Check/Watch/List.
// A prefix match would let them (routed by grpc.UnknownServiceHandler) skip auth.
var healthNearMisses = []string{
	"/grpc.health.v1.Health/CheckX",
	"/grpc.health.v1.HealthX/Check",
	"/grpc.health.v1.Health/x/a.B/C",
	"/grpc.health.v1.Health/x/admin.Svc/Delete",
	"/grpc.health.v1.Health/Check/",
	"/grpc.health.v1.Health/",
}

func noopStream(any, grpc.ServerStream) error { return nil }

func TestHealthNearMiss_RejectedByAuthAndContext(t *testing.T) {
	ctxSvc := service.NewContextService()
	unary := map[string]grpc.UnaryServerInterceptor{
		"auth":    grpcmw.UnaryRequireAuthInterceptor(),
		"context": grpcmw.UnaryContextInterceptor(ctxSvc),
	}
	stream := map[string]grpc.StreamServerInterceptor{
		"auth":    grpcmw.StreamRequireAuthInterceptor(),
		"context": grpcmw.StreamContextInterceptor(ctxSvc),
	}
	for _, m := range healthNearMisses {
		for name, ic := range unary {
			h := chainUnary([]grpc.UnaryServerInterceptor{ic}, m, okUnary)
			if _, err := h(context.Background(), nil); status.Code(err) != codes.Unauthenticated {
				t.Errorf("unary %s %q: err = %v, want Unauthenticated", name, m, err)
			}
		}
		for name, ic := range stream {
			h := chainStream([]grpc.StreamServerInterceptor{ic}, m, noopStream)
			if err := h(nil, &testServerStream{}); status.Code(err) != codes.Unauthenticated {
				t.Errorf("stream %s %q: err = %v, want Unauthenticated", name, m, err)
			}
		}
	}
}

func TestHealthNearMiss_RejectedByAuthZ(t *testing.T) {
	auth := grpccommon.NewDemoRBAC()
	cfg := grpccommon.Config{ServiceName: "health-near-miss"}
	for _, m := range healthNearMisses {
		// AuthZ alone: no RequestContext, so a non-exempt method is refused.
		h := chainUnary([]grpc.UnaryServerInterceptor{grpccommon.RequirePermission("read", "items", auth)}, m, okUnary)
		if _, err := h(context.Background(), nil); status.Code(err) != codes.FailedPrecondition {
			t.Errorf("unary authz %q: err = %v, want FailedPrecondition", m, err)
		}
		sh := chainStream([]grpc.StreamServerInterceptor{grpccommon.RequireStreamPermission("read", "items", auth)}, m, noopStream)
		if err := sh(nil, &testServerStream{}); status.Code(err) != codes.FailedPrecondition {
			t.Errorf("stream authz %q: err = %v, want FailedPrecondition", m, err)
		}
		// Full protected chain + authz: Unauthenticated, handler never reached.
		uics := append(grpccommon.ProtectedUnaryInterceptors(cfg), grpccommon.RequirePermission("read", "items", auth))
		if _, err := chainUnary(uics, m, okUnary)(context.Background(), nil); status.Code(err) != codes.Unauthenticated {
			t.Errorf("unary chain %q: err = %v, want Unauthenticated", m, err)
		}
		sics := append(grpccommon.ProtectedStreamInterceptors(cfg), grpccommon.RequireStreamPermission("read", "items", auth))
		if err := chainStream(sics, m, noopStream)(nil, &testServerStream{}); status.Code(err) != codes.Unauthenticated {
			t.Errorf("stream chain %q: err = %v, want Unauthenticated", m, err)
		}
	}
}

func TestHealthListExempt(t *testing.T) {
	cfg := grpccommon.Config{ServiceName: "health-list"}
	h := chainUnary(grpccommon.ProtectedUnaryInterceptors(cfg), "/grpc.health.v1.Health/List", okUnary)
	if _, err := h(context.Background(), nil); err != nil {
		t.Fatalf("List err = %v", err)
	}
}

// ─── strict roles ─────────────────────────────────────────────────────────────

func manyRoles(n int) string {
	r := make([]string, n)
	for i := range r {
		r[i] = "r" + strconv.Itoa(i)
	}
	return strings.Join(r, ",")
}

func TestContextInterceptor_InvalidRolesUnauthenticated(t *testing.T) {
	ctxSvc := service.NewContextService()
	for name, roles := range map[string]string{
		"too many": manyRoles(domain.MaxRoles + 1),
		"too long": strings.Repeat("a", domain.MaxRolesHeaderLen+1),
	} {
		ctx := identityCtx("x-user-id", "u", "x-tenant-id", "t", "x-tenant-roles", roles)
		reached := false
		_, err := grpcmw.UnaryContextInterceptor(ctxSvc)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/p.S/M"},
			func(context.Context, any) (any, error) { reached = true; return nil, nil })
		if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "invalid tenant roles" || reached {
			t.Errorf("unary %s: err = %v reached=%v, want Unauthenticated invalid tenant roles", name, err, reached)
		}
		err = grpcmw.StreamContextInterceptor(ctxSvc)(nil, &testServerStream{ctx: ctx},
			&grpc.StreamServerInfo{FullMethod: "/p.S/M"}, noopStream)
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("stream %s: err = %v, want Unauthenticated", name, err)
		}
	}
}

func TestContextInterceptor_RepeatedRolesJoinedAndCounted(t *testing.T) {
	ctxSvc := service.NewContextService()
	// Repeated entries are joined, then parsed strictly as one header.
	ctx := identityCtx("x-user-id", "u", "x-tenant-id", "t", "x-tenant-roles", "admin", "x-tenant-roles", "editor,bad role")
	var got []string
	_, err := grpcmw.UnaryContextInterceptor(ctxSvc)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/p.S/M"},
		func(c context.Context, _ any) (any, error) {
			rc, _ := domain.RequestContextFromGoContext(c)
			got = rc.Roles
			return nil, nil
		})
	if err != nil || strings.Join(got, ",") != "admin,editor" {
		t.Fatalf("roles = %v, err = %v; want [admin editor]", got, err)
	}
	// Split across entries, the total still counts against MaxRoles.
	half := domain.MaxRoles/2 + 1
	ctx = identityCtx("x-user-id", "u", "x-tenant-id", "t",
		"x-tenant-roles", manyRoles(half), "x-tenant-roles", strings.ReplaceAll(manyRoles(half), "r", "s"))
	_, err = grpcmw.UnaryContextInterceptor(ctxSvc)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/p.S/M"}, okUnary)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("split over-limit roles: err = %v, want Unauthenticated", err)
	}
}

// ─── stream timeout ───────────────────────────────────────────────────────────

// blockingStream models a transport stream: RecvMsg blocks until the transport
// context (not the handler's capped context) is cancelled, as grpc-go does.
type blockingStream struct {
	testServerStream
	recvCalls atomic.Int32
}

func (s *blockingStream) RecvMsg(any) error {
	s.recvCalls.Add(1)
	<-s.testServerStream.Context().Done()
	return status.Error(codes.Canceled, "transport closed")
}

func TestTimeoutStream_IdleRecvLoopReturnsDeadlineExceeded(t *testing.T) {
	transport, teardown := context.WithCancel(context.Background())
	defer teardown()
	ss := &blockingStream{testServerStream: testServerStream{ctx: transport}}
	handlerDone := make(chan error, 1)
	start := time.Now()
	err := grpccommon.TimeoutStreamInterceptor(50*time.Millisecond)(nil, ss, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error {
			var lastErr error
			defer func() { handlerDone <- lastErr }()
			for i := 0; i < 1000; i++ {
				if lastErr = s.RecvMsg(nil); status.Code(lastErr) == codes.DeadlineExceeded {
					return lastErr
				}
			}
			return lastErr
		})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("interceptor took %v, want ~50ms", el)
	}
	// grpc-go tears the transport stream down when the handler returns.
	teardown()
	select {
	case herr := <-handlerDone:
		if status.Code(herr) != codes.DeadlineExceeded {
			t.Fatalf("handler's next RecvMsg = %v, want DeadlineExceeded", herr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler goroutine leaked after stream teardown")
	}
	if n := ss.recvCalls.Load(); n != 1 {
		t.Fatalf("transport RecvMsg calls = %d, want 1 (later calls short-circuited)", n)
	}
}

func TestTimeoutStream_ExpiredWrapperShortCircuits(t *testing.T) {
	transport, teardown := context.WithCancel(context.Background())
	defer teardown()
	ss := &blockingStream{testServerStream: testServerStream{ctx: transport}}
	captured := make(chan grpc.ServerStream, 1)
	release := make(chan struct{})
	err := grpccommon.TimeoutStreamInterceptor(20*time.Millisecond)(nil, ss, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error {
			captured <- s
			<-release
			return nil
		})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("err = %v", err)
	}
	s := <-captured
	for name, e := range map[string]error{
		"SendMsg": s.SendMsg(nil), "RecvMsg": s.RecvMsg(nil),
		"SetHeader": s.SetHeader(nil), "SendHeader": s.SendHeader(nil),
	} {
		if status.Code(e) != codes.DeadlineExceeded {
			t.Errorf("%s after expiry = %v, want DeadlineExceeded", name, e)
		}
	}
	s.SetTrailer(nil) // must not panic
	close(release)
}

func TestTimeoutStream_PassesCallsBeforeExpiry(t *testing.T) {
	ss := &testServerStream{}
	err := grpccommon.TimeoutStreamInterceptor(time.Second)(nil, ss, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error {
			if s.SendMsg(nil) != nil || s.RecvMsg(nil) != nil || s.SetHeader(nil) != nil || s.SendHeader(nil) != nil {
				t.Error("stream calls failed before expiry")
			}
			s.SetTrailer(nil)
			return status.Error(codes.NotFound, "nope")
		})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("err = %v, want handler error NotFound", err)
	}
}

func TestTimeoutStream_PanicPropagatesToInterceptorGoroutine(t *testing.T) {
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recovered %v, want original panic value boom", r)
		}
	}()
	_ = grpccommon.TimeoutStreamInterceptor(time.Second)(nil, &testServerStream{}, &grpc.StreamServerInfo{},
		func(any, grpc.ServerStream) error { panic("boom") })
	t.Fatal("expected panic")
}

func TestTimeoutStream_PanicRecoveryStillCatches(t *testing.T) {
	ics := []grpc.StreamServerInterceptor{
		grpcmw.StreamPanicRecoveryInterceptor(nil),
		grpccommon.TimeoutStreamInterceptor(time.Second),
	}
	err := chainStream(ics, "/p.S/M", func(any, grpc.ServerStream) error { panic("boom") })(nil, &testServerStream{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("err = %v, want Internal from panic recovery", err)
	}
}

func TestTimeoutStream_PanicAfterDeadlineDropped(t *testing.T) {
	transport, teardown := context.WithCancel(context.Background())
	ss := &blockingStream{testServerStream: testServerStream{ctx: transport}}
	panicking := make(chan struct{})
	err := grpccommon.TimeoutStreamInterceptor(20*time.Millisecond)(nil, ss, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error {
			_ = s.RecvMsg(nil)
			close(panicking)
			panic(strings.Repeat("late boom ", 100))
		})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("err = %v", err)
	}
	teardown()
	<-panicking
	time.Sleep(20 * time.Millisecond) // the process must survive the late panic
}

func TestTimeoutStream_ClientCancelWaitsForHandler(t *testing.T) {
	transport, cancel := context.WithCancel(context.Background())
	ss := &blockingStream{testServerStream: testServerStream{ctx: transport}}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	err := grpccommon.TimeoutStreamInterceptor(time.Hour)(nil, ss, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error { return s.RecvMsg(nil) })
	if status.Code(err) != codes.Canceled {
		t.Fatalf("err = %v, want the handler's own Canceled error", err)
	}
}

func TestTimeoutStream_EarlierClientDeadlineRunsInline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	orig := &testServerStream{ctx: ctx}
	_ = grpccommon.TimeoutStreamInterceptor(time.Hour)(nil, orig, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error {
			if s != orig {
				t.Error("earlier client deadline: stream should pass through unchanged")
			}
			return nil
		})
}

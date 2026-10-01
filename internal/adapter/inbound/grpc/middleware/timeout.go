package middleware

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryTimeoutInterceptor caps the handler's context deadline at d: it applies d when
// the incoming call has no deadline or a later one, and leaves an earlier client
// deadline untouched. Zero or negative d returns a pass-through interceptor.
//
// A unary handler cannot be interrupted safely from outside, so the cap is only
// effective when the handler honours ctx (passes it to database/HTTP/gRPC calls
// or checks ctx.Done()). A handler that ignores ctx runs to completion; the client
// still sees its own deadline expire.
func UnaryTimeoutInterceptor(d time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, cancel := capDeadline(ctx, d)
		defer cancel()
		return handler(ctx, req)
	}
}

// errStreamTimeout is returned to the client (and by every later stream call the
// abandoned handler makes) when the StreamTimeoutInterceptor deadline fires.
var errStreamTimeout = status.Error(codes.DeadlineExceeded, "stream deadline exceeded")

// StreamTimeoutInterceptor is the streaming counterpart of UnaryTimeoutInterceptor.
//
// A capped deadline on the stream's context alone is not enough: RecvMsg / SendMsg
// block on the transport stream, whose context the cap does not reach, so a
// handler looping on RecvMsg against an idle client would never return. The
// interceptor therefore runs the handler in a goroutine and returns
// codes.DeadlineExceeded as soon as the capped deadline fires. grpc-go then ends
// the RPC and tears the transport stream down, which unblocks the handler's
// pending RecvMsg/SendMsg; from that point every stream call the handler makes
// returns DeadlineExceeded, so the goroutine exits as soon as the handler acts on
// that error. A handler that ignores both ctx and stream errors (busy work with no
// stream calls) keeps running after the RPC ends — that cannot be prevented.
//
// A panic in the handler goroutine is recovered there and re-raised in the
// interceptor goroutine with the original value, so outer panic-recovery,
// metrics, logging and tracing interceptors see it as usual. A panic after the
// deadline already fired (the RPC is over) is logged to stderr and dropped, since
// re-raising it would crash the process.
//
// When the client deadline is already earlier than d, the handler runs inline:
// grpc-go itself cancels the transport stream at the client deadline.
func StreamTimeoutInterceptor(d time.Duration) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if d <= 0 {
			return handler(srv, ss)
		}
		parent := ss.Context()
		ctx, cancel := capDeadline(parent, d)
		defer cancel()
		if ctx == parent {
			return handler(srv, ss)
		}

		ts := &timeoutStream{ServerStream: ss, ctx: ctx}
		done := make(chan streamResult, 1) // buffered: the goroutine never blocks on send
		go func() {
			panicked := true
			defer func() {
				if !panicked {
					return
				}
				r := recover()
				if ts.expired.Load() {
					log.Printf("grpc stream timeout: handler panicked after deadline: %q", truncatePanic(r))
					return
				}
				done <- streamResult{panicked: true, val: r}
			}()
			err := handler(srv, ts)
			panicked = false
			done <- streamResult{err: err}
		}()

		select {
		case r := <-done:
			return r.rethrow()
		case <-ctx.Done():
		}
		// Prefer a result that raced with the deadline.
		select {
		case r := <-done:
			return r.rethrow()
		default:
		}
		if parent.Err() != nil {
			// The client cancelled / its deadline hit: grpc-go is already tearing
			// the stream down, so the handler unblocks on its own — keep the
			// normal semantics and return what it returns.
			r := <-done
			return r.rethrow()
		}
		ts.expired.Store(true)
		return errStreamTimeout
	}
}

// streamResult is the outcome of a stream handler run by StreamTimeoutInterceptor.
type streamResult struct {
	err      error
	panicked bool
	val      any
}

// rethrow re-raises a recovered handler panic with its original value, or returns
// the handler's error.
func (r streamResult) rethrow() error {
	if r.panicked {
		panic(r.val)
	}
	return r.err
}

// timeoutStream gives the handler the capped context and, once the deadline
// has fired, short-circuits every stream call with DeadlineExceeded so an
// abandoned handler never touches the finished transport stream again.
// (A call already in flight when the deadline fires is unblocked by grpc-go's
// stream teardown.)
type timeoutStream struct {
	grpc.ServerStream
	ctx     context.Context
	expired atomic.Bool
}

func (s *timeoutStream) Context() context.Context { return s.ctx }

func (s *timeoutStream) SendMsg(m any) error {
	if s.expired.Load() {
		return errStreamTimeout
	}
	return s.ServerStream.SendMsg(m)
}

func (s *timeoutStream) RecvMsg(m any) error {
	if s.expired.Load() {
		return errStreamTimeout
	}
	return s.ServerStream.RecvMsg(m)
}

func (s *timeoutStream) SetHeader(md metadata.MD) error {
	if s.expired.Load() {
		return errStreamTimeout
	}
	return s.ServerStream.SetHeader(md)
}

func (s *timeoutStream) SendHeader(md metadata.MD) error {
	if s.expired.Load() {
		return errStreamTimeout
	}
	return s.ServerStream.SendHeader(md)
}

func (s *timeoutStream) SetTrailer(md metadata.MD) {
	if s.expired.Load() {
		return
	}
	s.ServerStream.SetTrailer(md)
}

func truncatePanic(v any) string {
	const maxLen = 256
	s := fmt.Sprint(v)
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func capDeadline(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= d {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

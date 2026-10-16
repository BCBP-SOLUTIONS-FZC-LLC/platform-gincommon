package grpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
)

// When the client cancels before the interceptor's deadline, grpc-go tears the
// stream down itself: the interceptor waits for the handler and returns the
// handler's own error. It must not report its stream deadline.
func TestStreamTimeout_ClientCancelReturnsHandlerResult(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ss := &testServerStream{ctx: parent}
	handlerErr := status.Error(codes.Canceled, "client went away")

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := grpccommon.TimeoutStreamInterceptor(time.Minute)(nil, ss, &grpc.StreamServerInfo{},
		func(_ any, s grpc.ServerStream) error {
			<-s.Context().Done()
			time.Sleep(10 * time.Millisecond) // finish after the interceptor saw the cancel
			return handlerErr
		})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("err = %v, want the handler's %v", err, handlerErr)
	}
	if status.Code(err) == codes.DeadlineExceeded {
		t.Fatal("a client cancel must not be reported as the interceptor's deadline")
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("interceptor took %v", el)
	}
}

// A handler that finishes inside the deadline returns its own result unchanged.
func TestStreamTimeout_HandlerFinishesFirst(t *testing.T) {
	ss := &testServerStream{ctx: context.Background()}
	want := status.Error(codes.NotFound, "nope")
	err := grpccommon.TimeoutStreamInterceptor(time.Minute)(nil, ss, &grpc.StreamServerInfo{},
		func(any, grpc.ServerStream) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

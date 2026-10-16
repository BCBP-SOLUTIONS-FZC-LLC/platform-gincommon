package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
)

type panickyLogger struct{}

func (panickyLogger) Debug(string, map[string]any) {}
func (panickyLogger) Info(string, map[string]any)  {}
func (panickyLogger) Warn(string, map[string]any)  {}
func (panickyLogger) Error(string, map[string]any) { panic("logger down") }

func TestErrorSanitizer_Unary(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/svc.V1/Get"}
	cases := map[string]struct {
		err  error
		code codes.Code
		msg  string
	}{
		"nil":           {nil, codes.OK, ""},
		"canceled":      {fmt.Errorf("q secret: %w", context.Canceled), codes.Canceled, "canceled"},
		"plain":         {errors.New("secret"), codes.Internal, grpcmw.MsgInternal},
		"status kept":   {status.Error(codes.InvalidArgument, "bad page"), codes.InvalidArgument, "bad page"},
		"joined status": {errors.Join(errors.New("secret"), status.Error(codes.NotFound, "nope")), codes.NotFound, "nope"},
	}
	for name, tc := range cases {
		for logName, logger := range map[string]interface {
			Debug(string, map[string]any)
			Info(string, map[string]any)
			Warn(string, map[string]any)
			Error(string, map[string]any)
		}{"nil": nil, "panicky": panickyLogger{}} {
			ic := grpcmw.UnaryErrorSanitizerInterceptor(logger, "svc")
			resp, err := ic(context.Background(), "req", info, func(context.Context, any) (any, error) { return "resp", tc.err })
			st := status.Convert(err)
			if resp != "resp" || st.Code() != tc.code || st.Message() != tc.msg {
				t.Errorf("%s/%s logger: %v %v %q", name, logName, resp, st.Code(), st.Message())
			}
		}
	}
}

func TestErrorSanitizer_StreamStatusAndNil(t *testing.T) {
	ic := grpcmw.StreamErrorSanitizerInterceptor(nil, "")
	ss := &testServerStream{ctx: context.Background()}
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{{nil, codes.OK}, {status.Error(codes.Aborted, "x"), codes.Aborted}} {
		err := ic(nil, ss, &grpc.StreamServerInfo{FullMethod: "/svc.V1/Watch"}, func(any, grpc.ServerStream) error { return tc.err })
		if status.Code(err) != tc.code {
			t.Errorf("stream %v → %v", tc.err, err)
		}
	}
}

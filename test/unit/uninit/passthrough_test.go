package uninit_test

// This file sorts before uninit_test.go, so its tests run before any test in this
// package initializes metrics: they pin that the metrics middleware and interceptors
// are a pure pass-through for consumers that never initialize metrics.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	grpcmw "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/grpc/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
)

func requireUninitialized(t *testing.T) {
	t.Helper()
	if metrics.Initialized() || metrics.HTTPEnabled() || metrics.GRPCEnabled() {
		t.Fatal("these tests must run before any metrics initialization")
	}
}

func TestMetricsMiddleware_PassThroughBeforeInit(t *testing.T) {
	requireUninitialized(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.MetricsMiddleware(nil))
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusTeapot, "ok") })
	r.GET("/panic", func(*gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusTeapot || w.Body.String() != "ok" {
		t.Fatalf("response = %d %q, want the handler's", w.Code, w.Body.String())
	}
	// Without metrics the middleware adds no recover: a panic still reaches the
	// caller (PanicRecoveryMiddleware's job), unchanged.
	func() {
		defer func() {
			if recover() != "boom" {
				t.Error("panic was swallowed or replaced")
			}
		}()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()
	requireUninitialized(t)
}

type stream struct{ grpc.ServerStream }

func (stream) Context() context.Context     { return context.Background() }
func (stream) SetHeader(metadata.MD) error  { return nil }
func (stream) SendHeader(metadata.MD) error { return nil }
func (stream) SetTrailer(metadata.MD)       {}

func TestGRPCMetricsInterceptors_PassThroughBeforeInit(t *testing.T) {
	requireUninitialized(t)
	want := errors.New("handler error")

	resp, err := grpcmw.UnaryMetricsInterceptor()(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/svc.V1/Get"},
		func(_ context.Context, req any) (any, error) { return req.(string) + "-resp", want })
	if resp != "req-resp" || !errors.Is(err, want) {
		t.Fatalf("unary = %v, %v; want the handler's result", resp, err)
	}

	called := false
	err = grpcmw.StreamMetricsInterceptor()(nil, stream{}, &grpc.StreamServerInfo{FullMethod: "/svc.V1/Watch"},
		func(any, grpc.ServerStream) error { called = true; return want })
	if !called || !errors.Is(err, want) {
		t.Fatalf("stream: called=%v err=%v; want the handler's result", called, err)
	}
	requireUninitialized(t)
}

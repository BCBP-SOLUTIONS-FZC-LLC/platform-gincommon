package gincommon_test

// This file imports only the public pkg/ API (plus gin, grpc and the standard
// library), exactly as a consuming module must: it proves a service outside this
// module can implement Authorizer and Logger and name RequestContext without
// importing internal/ packages, which Go forbids across module boundaries.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
)

// tenantAuthorizer is a consumer-side policy: only tenant "acme" may "write" orders.
type tenantAuthorizer struct{ calls int }

func (a *tenantAuthorizer) Authorize(_ context.Context, rc *gincommon.RequestContextData, action, resource string) error {
	a.calls++
	if rc.TenantID == "acme" && action == "write" && resource == "orders" {
		return nil
	}
	return fmt.Errorf("tenant %s: %w", rc.TenantID, gincommon.ErrForbidden)
}

// grpcAuthorizer is written against grpccommon's names only.
type grpcAuthorizer struct{}

func (grpcAuthorizer) Authorize(_ context.Context, rc *grpccommon.RequestContextData, _, _ string) error {
	if rc.UserID == "u-ok" {
		return nil
	}
	return grpccommon.ErrForbidden
}

// consumerLogger implements the public Logger alias.
type consumerLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *consumerLogger) add(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, level+":"+msg)
}
func (l *consumerLogger) Debug(m string, _ map[string]interface{}) { l.add("debug", m) }
func (l *consumerLogger) Info(m string, _ map[string]interface{})  { l.add("info", m) }
func (l *consumerLogger) Warn(m string, _ map[string]interface{})  { l.add("warn", m) }
func (l *consumerLogger) Error(m string, _ map[string]interface{}) { l.add("error", m) }

var (
	_ gincommon.Authorizer  = (*tenantAuthorizer)(nil)
	_ grpccommon.Authorizer = grpcAuthorizer{}
	// The aliases name the same types, so one implementation serves both stacks.
	_ grpccommon.Authorizer = (*tenantAuthorizer)(nil)
	_ gincommon.Logger      = (*consumerLogger)(nil)
	_ grpccommon.Logger     = (*consumerLogger)(nil)
	_ gincommon.Authorizer  = (*gincommon.DemoRBAC)(nil)
	_ *grpccommon.DemoRBAC  = grpccommon.NewDemoRBAC()
)

// tenantOf and userOf name the context type through the public aliases.
func tenantOf(rc *gincommon.RequestContextData) string { return rc.TenantID }
func userOf(rc *grpccommon.RequestContextData) string  { return rc.UserID }

func TestConsumerAuthorizer_Gin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &tenantAuthorizer{}
	log := &consumerLogger{}
	cfg := gincommon.Config{Logger: log, ServiceName: "pkg-test"}

	r := gin.New()
	r.Use(gincommon.ProtectedMiddlewares(cfg)...)
	r.POST("/orders", gincommon.RequirePermissionWithLogger("write", "orders", auth, cfg.Logger), func(c *gin.Context) {
		rc, ok := gincommon.RequestContext(c)
		if !ok || tenantOf(rc) != "acme" {
			t.Errorf("RequestContext = %+v, %v", rc, ok)
		}
		c.Status(http.StatusCreated)
	})
	r.POST("/orders-plain", gincommon.RequirePermission("write", "orders", auth), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	cases := []struct {
		path, tenant string
		want         int
	}{
		{"/orders", "acme", http.StatusCreated},
		{"/orders", "globex", http.StatusForbidden},
		{"/orders-plain", "acme", http.StatusCreated},
		{"/orders-plain", "globex", http.StatusForbidden},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, nil)
		req.Header.Set(gincommon.HeaderUserID, "u1")
		req.Header.Set(gincommon.HeaderTenantID, tc.tenant)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s tenant %s: status = %d, want %d", tc.path, tc.tenant, w.Code, tc.want)
		}
	}
	if auth.calls != len(cases) {
		t.Fatalf("Authorize calls = %d, want %d", auth.calls, len(cases))
	}
	if !errors.Is(auth.Authorize(context.Background(), &gincommon.RequestContextData{TenantID: "x"}, "read", "y"), gincommon.ErrForbidden) {
		t.Fatal("denial must wrap gincommon.ErrForbidden")
	}
}

func TestConsumerAuthorizer_GRPC(t *testing.T) {
	cfg := grpccommon.Config{ServiceName: "pkg-test", Logger: &consumerLogger{}}
	chain := append(grpccommon.ProtectedUnaryInterceptors(cfg),
		grpccommon.RequirePermissionWithLogger("write", "orders", grpcAuthorizer{}, cfg.Logger))

	call := func(userID string) error {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
			strings.ToLower(gincommon.HeaderUserID), userID,
			strings.ToLower(gincommon.HeaderTenantID), "acme",
		))
		info := &grpc.UnaryServerInfo{FullMethod: "/orders.v1.Orders/Create"}
		final := func(ctx context.Context, _ any) (any, error) {
			rc, ok := grpccommon.RequestContextFromContext(ctx)
			if !ok || userOf(rc) != userID {
				return nil, errors.New("request context missing")
			}
			return "ok", nil
		}
		handler := final
		for i := len(chain) - 1; i >= 0; i-- {
			ic, next := chain[i], handler
			handler = func(ctx context.Context, req any) (any, error) { return ic(ctx, req, info, next) }
		}
		_, err := handler(ctx, nil)
		return err
	}

	if err := call("u-ok"); err != nil {
		t.Fatalf("allowed call: %v", err)
	}
	if err := call("u-denied"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied call: %v, want PermissionDenied", err)
	}
}

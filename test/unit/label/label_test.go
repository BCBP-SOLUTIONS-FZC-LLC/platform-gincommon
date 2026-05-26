package label_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/inbound/label"
	"github.com/gin-gonic/gin"
)

func TestHTTPRoute_KnownPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/users/1", nil)
	c.FullPath()

	r := gin.New()
	r.GET("/users/:id", func(c *gin.Context) {
		if got := label.HTTPRoute(c); got != "/users/:id" {
			t.Fatalf("HTTPRoute = %q, want /users/:id", got)
		}
	})
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/1", nil))
}

func TestHTTPRoute_FallsBackToURLPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/metrics", nil)

	if got := label.HTTPRoute(c); got != "/metrics" {
		t.Fatalf("HTTPRoute = %q, want /metrics", got)
	}
}

func TestHTTPRoute_UnknownWhenNoPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.URL.Path = ""

	if got := label.HTTPRoute(c); got != label.Unknown {
		t.Fatalf("HTTPRoute = %q, want %q", got, label.Unknown)
	}
}

func TestGRPCMethod(t *testing.T) {
	if got := label.GRPCMethod("/pkg.Service/Method"); got != "/pkg.Service/Method" {
		t.Fatalf("GRPCMethod = %q", got)
	}
	if got := label.GRPCMethod(""); got != label.Unknown {
		t.Fatalf("empty GRPCMethod = %q, want %q", got, label.Unknown)
	}
}

package gincommon_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestTimeoutMiddleware_ViaGincommon_SetsDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var hasDeadline bool
	r := gin.New()
	r.Use(gincommon.TimeoutMiddleware(5 * time.Second))
	r.GET("/x", func(c *gin.Context) {
		_, hasDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !hasDeadline {
		t.Fatal("expected context deadline to be set")
	}
}

func TestTimeoutMiddleware_ViaGincommon_ZeroTimeout_NoDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var hasDeadline bool
	r := gin.New()
	r.Use(gincommon.TimeoutMiddleware(0))
	r.GET("/x", func(c *gin.Context) {
		_, hasDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if hasDeadline {
		t.Fatal("expected no deadline for zero timeout")
	}
}

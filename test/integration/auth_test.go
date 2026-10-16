package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/testenv"
)

func TestRequireAuth_InvalidHeaders_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	r := gin.New()
	cfg := gincommon.Config{Logger: log, ServiceName: "auth-test"}
	for _, m := range gincommon.DefaultMiddlewares(cfg) {
		r.Use(m)
	}
	r.GET("/secure", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("x-user-id", "bad\nuser")
	req.Header.Set("x-tenant-id", "tenant-1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

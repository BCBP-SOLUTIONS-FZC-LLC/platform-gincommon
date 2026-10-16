package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/testenv"
)

func TestMetricsMiddleware_RecordsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log, _ := logger.NewLogger("dev")
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{Logger: log, ServiceName: "metrics-int"}) {
		r.Use(m)
	}
	r.GET("/metrics", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("iteration %d: status = %d", i, w.Code)
		}
	}
}

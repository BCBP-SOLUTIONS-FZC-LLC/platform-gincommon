package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/test/testenv"
)

func TestServerStack_NonZeroTraceIDInLogsWithoutOTLP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	otel.Shutdown()
	t.Cleanup(otel.Shutdown)

	log := &fixtures.MockLogger{}
	r := gin.New()
	for _, m := range gincommon.DefaultMiddlewares(gincommon.Config{
		Logger:       log,
		ServiceName:  "integration-trace",
		BuildVersion: "test",
	}) {
		r.Use(m)
	}
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without auth headers", w.Code)
	}

	traceID, _ := log.Fields["trace_id"].(string)
	if traceID == "00000000000000000000000000000000" {
		t.Fatalf("trace_id = %q, want non-zero from DefaultMiddlewares OTel init", traceID)
	}
	if len(traceID) != 32 {
		t.Fatalf("trace_id = %q, want 32-char hex OTel trace ID", traceID)
	}
}

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/otel"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/testenv"
)

func TestServerSetup_DevEnvironment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_ENV", "dev")
	t.Setenv("OTEL_SERVICE_NAME", "setup-test-dev")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	router, shutdownTracing, err := server.Setup()
	if err != nil {
		t.Fatalf("Setup() err = %v", err)
	}
	defer shutdownTracing()
	t.Cleanup(otel.Shutdown)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestServerSetup_ProdEnvironmentSetsReleaseMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OTEL_SERVICE_NAME", "setup-test-prod")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	router, shutdownTracing, err := server.Setup()
	if err != nil {
		t.Fatalf("Setup() err = %v", err)
	}
	defer shutdownTracing()
	t.Cleanup(otel.Shutdown)
	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("gin mode = %q want %q", gin.Mode(), gin.ReleaseMode)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d", w.Code)
	}

	gin.SetMode(gin.TestMode)
}

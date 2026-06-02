package server_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
)

// TestSetup_NewLoggerError exercises Setup's error path: when logger.NewLogger fails
// (LOG_FILE points under a regular file so the directory cannot be created), Setup must
// flush tracing and return the error rather than a router.
func TestSetup_NewLoggerError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	t.Setenv("LOG_FILE", filepath.Join(blocker, "sub", "app.log"))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	router, shutdown, err := server.Setup()
	if err == nil {
		t.Fatal("expected Setup error when logger construction fails")
	}
	if router != nil {
		t.Fatal("expected nil router on Setup error")
	}
	if shutdown != nil {
		t.Fatal("expected nil shutdown on Setup error")
	}
}

func TestSetGinMode_DevLeavesTestMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server.SetGinMode("dev")
	if gin.Mode() != gin.TestMode {
		t.Fatalf("gin mode = %q want %q", gin.Mode(), gin.TestMode)
	}
}

func TestSetGinMode_ProdSetsReleaseMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server.SetGinMode("prod")
	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("gin mode = %q want %q", gin.Mode(), gin.ReleaseMode)
	}
	gin.SetMode(gin.TestMode)
}

package server_test

import (
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
)

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

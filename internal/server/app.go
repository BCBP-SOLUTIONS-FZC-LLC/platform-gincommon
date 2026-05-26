package server

import (
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

// Setup loads environment, initializes OpenTelemetry, configures Gin, and returns the HTTP router.
// Call the returned shutdown function on process exit (e.g. defer shutdown() in main).
func Setup() (*gin.Engine, func(), error) {
	_ = godotenv.Load()

	shutdownTracing := gincommon.InitTracingFromEnv()

	log, err := logger.NewLogger(config.AppEnv())
	if err != nil {
		shutdownTracing()
		return nil, nil, err
	}

	SetGinMode(config.AppEnv())

	router := NewRouter(log, config.OtelServiceName())
	return router, shutdownTracing, nil
}

// SetGinMode sets Gin to release mode for non-dev environments.
func SetGinMode(appEnv string) {
	if appEnv != "dev" {
		gin.SetMode(gin.ReleaseMode)
	}
}

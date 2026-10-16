package server

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
)

// Setup loads environment, initializes OpenTelemetry, configures Gin, and returns the HTTP router.
// Call the returned shutdown function on process exit (e.g. defer shutdown() in main).
func Setup() (*gin.Engine, func(), error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("warn: .env parse error: %v", err)
	}

	shutdownTracing := gincommon.InitTracingFromEnv()

	zapLog, err := logger.NewLogger(config.AppEnv())
	if err != nil {
		shutdownTracing()
		return nil, nil, err
	}

	SetGinMode(config.AppEnv())

	shutdown := func() {
		shutdownTracing()
		_ = zapLog.Sync() // flush buffered log entries before process exit
	}

	router := NewRouter(zapLog, config.OtelServiceName())
	return router, shutdown, nil
}

// SetGinMode sets Gin to release mode for non-dev/development/local environments.
func SetGinMode(appEnv string) {
	if !config.IsDevEnv(appEnv) {
		gin.SetMode(gin.ReleaseMode)
	}
}

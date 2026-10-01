package testenv

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// Defaults for the required observability identity. gincommon panics at startup
// without a domain and an in-vocabulary environment, so test processes that
// build the middleware stack get these unless .env or the caller sets them.
// ENVIRONMENT is used (not APP_ENV) so APP_ENV-dependent defaults (OTLP TLS,
// sampling ratio, the demo /panic route) keep their unset behaviour.
const (
	DefaultDomain      = "iam"
	DefaultEnvironment = "test"
)

func init() {
	_ = godotenv.Load(".env")
	if _, err := os.Stat(".env"); err != nil {
		_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	}
	setDefault("OBSERVABILITY_DOMAIN", DefaultDomain)
	setDefault("ENVIRONMENT", DefaultEnvironment)
}

func setDefault(key, value string) {
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
	}
}

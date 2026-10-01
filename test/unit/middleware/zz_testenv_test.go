package middleware_test

// Loads .env and the required observability identity defaults
// (OBSERVABILITY_DOMAIN, ENVIRONMENT) before any test builds the middleware stack.
import _ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/testenv"

package gincommon

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler returns a Gin handler that responds 200 OK with {"status":"ok"}.
// Register this route before ProtectedMiddlewares — it does not require auth headers.
func HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

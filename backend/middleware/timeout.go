package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ExtendedTimeout creates a middleware that extends the write deadline for long-running operations.
// this is necessary because the server's default WriteTimeout may be too short for operations
// like downloading updates.
func ExtendedTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// get the underlying connection and extend the write deadline
		rc := http.NewResponseController(c.Writer)
		if err := rc.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Next()
	}
}

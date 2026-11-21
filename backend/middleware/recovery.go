package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AbortRecovery is the recovery handler of the routers. A handler that
// panics with http.ErrAbortHandler cuts its response on purpose, such as a
// download that fails after its first bytes were sent, so the panic is raised
// again for the server to drop the response without finishing it. Any other
// panic is answered with 500.
func AbortRecovery(c *gin.Context, err any) {
	if e, ok := err.(error); ok && errors.Is(e, http.ErrAbortHandler) {
		panic(http.ErrAbortHandler)
	}
	c.AbortWithStatus(http.StatusInternalServerError)
}

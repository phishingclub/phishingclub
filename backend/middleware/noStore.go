package middleware

import "github.com/gin-gonic/gin"

// NoStore keeps responses out of the browser disk cache. A handler may set
// its own Cache-Control afterwards to override it.
func NoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

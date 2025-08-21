package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health is the Health controller
type Health struct{}

// Health returns a 200 OK
func (c *Health) Health(g *gin.Context) {
	g.Status(http.StatusOK)
}

package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/service"
)

// Version is a controller
type Version struct {
	Common
	versionService *service.Version
}

// Get application version
func (c *Version) Get(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	version, err := c.versionService.Get(g.Request.Context(), session)
	if ok := handleServerError(g, c.Response, err); !ok {
		return
	}
	c.Response.OK(g, version)
}

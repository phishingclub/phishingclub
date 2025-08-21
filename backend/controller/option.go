package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
)

// Option is a Option controller
type Option struct {
	Common
	OptionService *service.Option
}

// Get a update option
func (c *Option) Get(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	key := g.Param("key")
	if key == "" {
		c.Response.BadRequestMessage(g, "option is required")
		return
	}
	ctx := g.Request.Context()
	option, err := c.OptionService.GetOption(
		ctx,
		session,
		key,
	)
	if ok := handleServerError(g, c.Response, err); !ok {
		return
	}
	if key == data.OptionKeyAdminSSOLogin {
		option, err = c.OptionService.MaskSSOSecret(option)
		if ok := handleServerError(g, c.Response, err); !ok {
			return
		}
	}
	c.Response.OK(g, option)
}

// Update sets a option
func (c *Option) Update(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Option
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	err := c.OptionService.SetOptionByKey(g, session, &req)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{},
	)
}

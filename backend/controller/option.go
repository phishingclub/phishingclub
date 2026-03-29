package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/api"
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

// GetAutoPrune returns the full auto prune option (global flag + all per-company entries).
func (c *Option) GetAutoPrune(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	ctx := g.Request.Context()
	opt, err := c.OptionService.GetAutoPruneOption(ctx, session)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, opt)
}

// SetAutoPrune persists the full auto prune option (global flag + all per-company entries).
func (c *Option) SetAutoPrune(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req model.AutoPruneOption
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	ctx := g.Request.Context()
	err := c.OptionService.SetAutoPruneOption(ctx, session, &req)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// GetCompanyAutoPrune returns the per company auto prune enabled flag for the given company.
func (c *Option) GetCompanyAutoPrune(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(g.Param("id"))
	if err != nil {
		c.Response.BadRequestMessage(g, api.InvalidCompanyID)
		return
	}
	ctx := g.Request.Context()
	enabled, err := c.OptionService.GetCompanyAutoPruneOption(ctx, session, &companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"enabled": enabled})
}

// SetCompanyAutoPrune updates the per company auto prune enabled flag within the shared option row.
func (c *Option) SetCompanyAutoPrune(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(g.Param("id"))
	if err != nil {
		c.Response.BadRequestMessage(g, api.InvalidCompanyID)
		return
	}
	// parse only the enabled flag from the request body
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	ctx := g.Request.Context()
	err = c.OptionService.SetCompanyAutoPruneOption(ctx, session, &companyID, req.Enabled)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

package controller

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
)

// AllowListing is the controller for allow listing information
type AllowListing struct {
	Common
	AllowListingService *service.AllowListing
}

// allowListingGetter reads the allow listing information of one id in the
// company in view
type allowListingGetter func(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	companyID *uuid.UUID,
) (*model.AllowListing, error)

// handle reads the session, the id and the optional companyID query, which is
// the company in view, and answers with the allow listing information
func (c *AllowListing) handle(g *gin.Context, get allowListingGetter) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	companyID, ok := strictCompanyIDQuery(g, c.Response)
	if !ok {
		return
	}
	result, err := get(g.Request.Context(), session, id, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

// GetByTemplateID returns the allow listing information of a template
func (c *AllowListing) GetByTemplateID(g *gin.Context) {
	c.handle(g, c.AllowListingService.GetByTemplateID)
}

// GetByCampaignID returns the allow listing information of a campaign
func (c *AllowListing) GetByCampaignID(g *gin.Context) {
	c.handle(g, c.AllowListingService.GetByCampaignID)
}

// GetByProgramID returns the allow listing information of a program
func (c *AllowListing) GetByProgramID(g *gin.Context) {
	c.handle(g, c.AllowListingService.GetByProgramID)
}

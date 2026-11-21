package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
)

// CampaignDefaults is the controller for the new campaign form defaults.
// handlers come in a per company variant (companyID from the url) and a
// global variant that edits the default every company inherits from.
type CampaignDefaults struct {
	Common
	CampaignDefaultsService *service.CampaignDefaults
}

// upsertCampaignDefaultsRequest is the request body of the upsert handlers, a
// null field is not set and is inherited
type upsertCampaignDefaultsRequest struct {
	CloseAfterDays         *int  `json:"closeAfterDays"`
	AnonymizeAfterDays     *int  `json:"anonymizeAfterDays"`
	DataAnonymizeAfterDays *int  `json:"dataAnonymizeAfterDays"`
	IsAnonymous            *bool `json:"isAnonymous"`
	SaveSubmittedData      *bool `json:"saveSubmittedData"`
}

// parseCompanyIDParam parses the :companyID url param
func (c *CampaignDefaults) parseCompanyIDParam(g *gin.Context) (*uuid.UUID, bool) {
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return nil, false
	}
	return &companyID, true
}

func (c *CampaignDefaults) get(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	defaults, err := c.CampaignDefaultsService.GetByCompanyID(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, defaults)
}

func (c *CampaignDefaults) upsert(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req upsertCampaignDefaultsRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	stored, err := c.CampaignDefaultsService.Upsert(
		g.Request.Context(),
		session,
		companyID,
		&model.CampaignDefaults{
			CloseAfterDays:         req.CloseAfterDays,
			AnonymizeAfterDays:     req.AnonymizeAfterDays,
			DataAnonymizeAfterDays: req.DataAnonymizeAfterDays,
			IsAnonymous:            req.IsAnonymous,
			SaveSubmittedData:      req.SaveSubmittedData,
		},
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stored)
}

func (c *CampaignDefaults) remove(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	err := c.CampaignDefaultsService.DeleteByCompanyID(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// GetByCompanyID returns the stored defaults of a company
func (c *CampaignDefaults) GetByCompanyID(g *gin.Context) {
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	c.get(g, companyID)
}

// Upsert stores the defaults of a company
func (c *CampaignDefaults) Upsert(g *gin.Context) {
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	c.upsert(g, companyID)
}

// Delete removes the defaults of a company
func (c *CampaignDefaults) Delete(g *gin.Context) {
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	c.remove(g, companyID)
}

// GetGlobal returns the stored global defaults
func (c *CampaignDefaults) GetGlobal(g *gin.Context) {
	c.get(g, nil)
}

// UpsertGlobal stores the global defaults
func (c *CampaignDefaults) UpsertGlobal(g *gin.Context) {
	c.upsert(g, nil)
}

// DeleteGlobal removes the global defaults
func (c *CampaignDefaults) DeleteGlobal(g *gin.Context) {
	c.remove(g, nil)
}

// GetResolved returns the defaults the new campaign form uses. The optional
// companyID query param picks the company, without it only the global
// defaults apply.
func (c *CampaignDefaults) GetResolved(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var companyID *uuid.UUID
	if raw := g.Query("companyID"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
			return
		}
		companyID = &id
	}
	resolved, err := c.CampaignDefaultsService.GetResolved(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, resolved)
}

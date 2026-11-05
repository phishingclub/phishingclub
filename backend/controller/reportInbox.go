package controller

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
)

// ReportInbox is the report inbox controller. Handlers come in a per company
// variant (companyID from the url) and a global variant (nil companyID) that
// covers global campaigns only.
type ReportInbox struct {
	Common
	ReportInboxService *service.ReportInbox
}

// reportInboxRequest is the request body for upsert and test. An empty client
// secret keeps the stored one.
type reportInboxRequest struct {
	Enabled         bool    `json:"enabled"`
	AuthMode        string  `json:"authMode"`
	Mailbox         string  `json:"mailbox"`
	TenantID        string  `json:"tenantID"`
	ClientID        string  `json:"clientID"`
	ClientSecret    string  `json:"clientSecret"`
	OAuthProviderID *string `json:"oauthProviderID"`
}

func (c *ReportInbox) parseRequest(g *gin.Context) (*model.ReportInbox, bool) {
	var req reportInboxRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return nil, false
	}
	inbox := &model.ReportInbox{
		Enabled:         req.Enabled,
		AuthMode:        strings.TrimSpace(req.AuthMode),
		Mailbox:         strings.TrimSpace(req.Mailbox),
		TenantID:        strings.TrimSpace(req.TenantID),
		ClientID:        strings.TrimSpace(req.ClientID),
		ClientSecret:    strings.TrimSpace(req.ClientSecret),
		OAuthProviderID: nullable.NewNullNullable[uuid.UUID](),
	}
	if inbox.AuthMode == "" {
		inbox.AuthMode = model.ReportInboxAuthApp
	}
	if req.OAuthProviderID != nil && *req.OAuthProviderID != "" {
		id, err := uuid.Parse(*req.OAuthProviderID)
		if err != nil {
			c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
			return nil, false
		}
		inbox.OAuthProviderID.Set(id)
	}
	return inbox, true
}

func (c *ReportInbox) get(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	inbox, err := c.ReportInboxService.GetByCompanyID(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, inbox)
}

func (c *ReportInbox) upsert(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	incoming, ok := c.parseRequest(g)
	if !ok {
		return
	}
	inbox, err := c.ReportInboxService.Upsert(g.Request.Context(), session, companyID, incoming)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, inbox)
}

func (c *ReportInbox) remove(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	err := c.ReportInboxService.DeleteByCompanyID(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

func (c *ReportInbox) test(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	incoming, ok := c.parseRequest(g)
	if !ok {
		return
	}
	result, err := c.ReportInboxService.Test(g.Request.Context(), session, companyID, incoming)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

func (c *ReportInbox) pollNow(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	err := c.ReportInboxService.PollNow(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

func (c *ReportInbox) companyID(g *gin.Context) (*uuid.UUID, bool) {
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return nil, false
	}
	return &companyID, true
}

// GetByCompanyID returns the report inbox of a company
func (c *ReportInbox) GetByCompanyID(g *gin.Context) {
	if id, ok := c.companyID(g); ok {
		c.get(g, id)
	}
}

// Upsert creates or updates the report inbox of a company
func (c *ReportInbox) Upsert(g *gin.Context) {
	if id, ok := c.companyID(g); ok {
		c.upsert(g, id)
	}
}

// Delete removes the report inbox of a company
func (c *ReportInbox) Delete(g *gin.Context) {
	if id, ok := c.companyID(g); ok {
		c.remove(g, id)
	}
}

// Test checks report inbox settings of a company without saving them
func (c *ReportInbox) Test(g *gin.Context) {
	if id, ok := c.companyID(g); ok {
		c.test(g, id)
	}
}

// PollNow makes the report inbox of a company poll right away
func (c *ReportInbox) PollNow(g *gin.Context) {
	if id, ok := c.companyID(g); ok {
		c.pollNow(g, id)
	}
}

// GetGlobal returns the global report inbox
func (c *ReportInbox) GetGlobal(g *gin.Context) { c.get(g, nil) }

// UpsertGlobal creates or updates the global report inbox
func (c *ReportInbox) UpsertGlobal(g *gin.Context) { c.upsert(g, nil) }

// DeleteGlobal removes the global report inbox
func (c *ReportInbox) DeleteGlobal(g *gin.Context) { c.remove(g, nil) }

// TestGlobal checks global report inbox settings without saving them
func (c *ReportInbox) TestGlobal(g *gin.Context) { c.test(g, nil) }

// PollNowGlobal makes the global report inbox poll right away
func (c *ReportInbox) PollNowGlobal(g *gin.Context) { c.pollNow(g, nil) }

package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/service"
)

// CompanyScimConfig is the SCIM configuration controller
type CompanyScimConfig struct {
	Common
	CompanyScimConfigService *service.CompanyScimConfig
	ScimService              *service.Scim
}

// Prune removes the company's SCIM-disabled recipients whose retention window has elapsed.
func (c *CompanyScimConfig) Prune(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	pruned, err := c.ScimService.PruneSoftDeletedAuthorized(
		g.Request.Context(),
		session,
		&companyID,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"pruned": pruned})
}

// Restore clears the SCIM-disabled mark from the company's deprovisioned recipients.
func (c *CompanyScimConfig) Restore(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	restored, err := c.ScimService.RestoreSoftDeletedAuthorized(
		g.Request.Context(),
		session,
		&companyID,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"restored": restored})
}

// upsertScimRequest is the request body for the Upsert handler
type upsertScimRequest struct {
	Enabled bool `json:"enabled"`
}

// GetByCompanyID returns the SCIM configuration for the given company
func (c *CompanyScimConfig) GetByCompanyID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse companyID from url param
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	// get
	config, err := c.CompanyScimConfigService.GetByCompanyID(
		g.Request.Context(),
		session,
		&companyID,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, config)
}

// Upsert creates or updates the SCIM configuration for the given company
func (c *CompanyScimConfig) Upsert(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse companyID from url param
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	// parse request body
	var req upsertScimRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// upsert
	result, err := c.CompanyScimConfigService.Upsert(
		g.Request.Context(),
		session,
		&companyID,
		req.Enabled,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

// RotateToken generates a new bearer token for the given company's SCIM config.
// the plain token is returned once and must be stored by the caller.
func (c *CompanyScimConfig) RotateToken(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse companyID from url param
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	// rotate
	result, err := c.CompanyScimConfigService.RotateToken(
		g.Request.Context(),
		session,
		&companyID,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

// Delete removes the SCIM configuration for the given company
func (c *CompanyScimConfig) Delete(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse companyID from url param
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	// delete
	err = c.CompanyScimConfigService.DeleteByCompanyID(
		g.Request.Context(),
		session,
		&companyID,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

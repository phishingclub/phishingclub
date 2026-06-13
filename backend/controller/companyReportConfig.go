package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
)

// CompanyReportConfig is the report delivery configuration controller. handlers
// come in a per company variant (companyID from the url) and a global variant
// (nil companyID) that edits the fallback config.
type CompanyReportConfig struct {
	Common
	CompanyReportConfigService *service.CompanyReportConfig
	CampaignService            *service.Campaign
}

// upsertReportConfigRequest is the request body for the upsert handlers
type upsertReportConfigRequest struct {
	Enabled             bool    `json:"enabled"`
	SendOnFinish        bool    `json:"sendOnFinish"`
	RecipientGroupID    *string `json:"recipientGroupID"`
	SMTPConfigurationID *string `json:"smtpConfigurationID"`
	SenderEmail         *string `json:"senderEmail"`
	EmailSubject        *string `json:"emailSubject"`
	EmailBody           *string `json:"emailBody"`
}

// get returns the config for the given scope (nil companyID is the global default)
func (c *CompanyReportConfig) get(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	config, err := c.CompanyReportConfigService.GetByCompanyID(
		g.Request.Context(),
		session,
		companyID,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, config)
}

// upsert creates or updates the config for the given scope
func (c *CompanyReportConfig) upsert(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req upsertReportConfigRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	incoming := &model.CompanyReportConfig{
		Enabled:             req.Enabled,
		SendOnFinish:        req.SendOnFinish,
		RecipientGroupID:    nullable.NewNullNullable[uuid.UUID](),
		SMTPConfigurationID: nullable.NewNullNullable[uuid.UUID](),
		SenderEmail:         nullable.NewNullNullable[vo.Email](),
		EmailSubject:        nullable.NewNullableWithValue(""),
		EmailBody:           nullable.NewNullableWithValue(""),
	}
	if req.EmailSubject != nil {
		incoming.EmailSubject.Set(*req.EmailSubject)
	}
	if req.EmailBody != nil {
		incoming.EmailBody.Set(*req.EmailBody)
	}
	if req.RecipientGroupID != nil && *req.RecipientGroupID != "" {
		groupID, err := uuid.Parse(*req.RecipientGroupID)
		if err != nil {
			c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
			return
		}
		incoming.RecipientGroupID.Set(groupID)
	}
	if req.SMTPConfigurationID != nil && *req.SMTPConfigurationID != "" {
		smtpID, err := uuid.Parse(*req.SMTPConfigurationID)
		if err != nil {
			c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
			return
		}
		incoming.SMTPConfigurationID.Set(smtpID)
	}
	if req.SenderEmail != nil && *req.SenderEmail != "" {
		email, err := vo.NewEmail(*req.SenderEmail)
		if err != nil {
			c.Response.BadRequestMessage(g, "invalid sender email")
			return
		}
		incoming.SenderEmail.Set(*email)
	}
	result, err := c.CompanyReportConfigService.Upsert(
		g.Request.Context(),
		session,
		companyID,
		incoming,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

// remove deletes the config for the given scope
func (c *CompanyReportConfig) remove(g *gin.Context, companyID *uuid.UUID) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	err := c.CompanyReportConfigService.DeleteByCompanyID(
		g.Request.Context(),
		session,
		companyID,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// parseCompanyIDParam parses the :companyID url param
func (c *CompanyReportConfig) parseCompanyIDParam(g *gin.Context) (*uuid.UUID, bool) {
	companyID, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		c.Logger.Debugw("failed to parse companyID param", "error", err)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return nil, false
	}
	return &companyID, true
}

// GetByCompanyID returns the report delivery configuration for the given company
func (c *CompanyReportConfig) GetByCompanyID(g *gin.Context) {
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	c.get(g, companyID)
}

// Upsert creates or updates the report delivery configuration for the given company
func (c *CompanyReportConfig) Upsert(g *gin.Context) {
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	c.upsert(g, companyID)
}

// Delete removes the report delivery configuration for the given company
func (c *CompanyReportConfig) Delete(g *gin.Context) {
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	c.remove(g, companyID)
}

// GetGlobal returns the global default report delivery configuration
func (c *CompanyReportConfig) GetGlobal(g *gin.Context) {
	c.get(g, nil)
}

// UpsertGlobal creates or updates the global default report delivery configuration
func (c *CompanyReportConfig) UpsertGlobal(g *gin.Context) {
	c.upsert(g, nil)
}

// DeleteGlobal removes the global default report delivery configuration
func (c *CompanyReportConfig) DeleteGlobal(g *gin.Context) {
	c.remove(g, nil)
}

// GetLogByCompanyID returns the report delivery log for the given company
func (c *CompanyReportConfig) GetLogByCompanyID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, ok := c.parseCompanyIDParam(g)
	if !ok {
		return
	}
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	// map the table column labels to the underlying db columns
	queryArgs.RemapOrderBy(map[string]string{
		"date":       "created_at",
		"created_at": "created_at",
		"campaign":   "campaign_name",
		"group":      "group_name",
		"trigger":    "trigger",
		"status":     "status",
		"recipients": "recipient_count",
	})
	queryArgs.DefaultSortByCreatedAt()
	result, err := c.CompanyReportConfigService.ListLogByCompanyID(
		g.Request.Context(),
		session,
		companyID,
		&repository.ReportSendLogOption{QueryArgs: queryArgs},
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

// SendNow generates the report for the campaign and emails it to the configured
// recipient group right away.
func (c *CompanyReportConfig) SendNow(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	campaignID, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	err := c.CampaignService.SendCampaignReport(
		g.Request.Context(),
		session,
		campaignID,
		true,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

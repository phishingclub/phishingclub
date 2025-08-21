package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
)

// SMTPConfigurationColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var SMTPConfigurationColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "name"),
	"host":       repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "host"),
	"port":       repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "port"),
	"username":   repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "username"),
}

// SMTPConfiguration is a controller
type SMTPConfiguration struct {
	Common
	SMTPConfigurationService *service.SMTPConfiguration
}

// Create creates a new SMTPConfiguration
func (c *SMTPConfiguration) Create(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.SMTPConfiguration
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save SMTP configuration
	id, err := c.SMTPConfigurationService.Create(g, session, &req)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetAll gets SMTP configurations
func (c *SMTPConfiguration) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(SMTPConfigurationColumnsMap)
	companyID := companyIDFromRequestQuery(g)
	// get
	smtpConfigs, err := c.SMTPConfigurationService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.SMTPConfigurationOption{
			QueryArgs:   queryArgs,
			WithCompany: true,
			WithHeaders: true,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, smtpConfigs)
}

// GetByID gets a SMTP configuration by an ID
func (c *SMTPConfiguration) GetByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get SMTP configuration
	smtpConfig, err := c.SMTPConfigurationService.GetByID(
		g.Request.Context(),
		session,
		id,
		&repository.SMTPConfigurationOption{
			WithCompany: true,
			WithHeaders: true,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, smtpConfig)
}

type SMTPConfigurationTestEmailRequest struct {
	Email    vo.Email `json:"email" binding:"required,email"`
	MailFrom vo.Email `json:"mailFrom" binding:"required,mailFrom"`
}

// TestEmail tests the connection to a SMTP configuration
func (c *SMTPConfiguration) TestEmail(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req SMTPConfigurationTestEmailRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// test dial
	err := c.SMTPConfigurationService.SendTestEmail(
		g,
		session,
		id,
		&req.Email,
		&req.MailFrom,
	)
	// handle any error as a validation error
	if err != nil {
		err = errs.NewValidationError(err)
	}
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// UpdateByID updates a SMTP configuration - but not the headers
func (c *SMTPConfiguration) UpdateByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.SMTPConfiguration
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	err := c.SMTPConfigurationService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// AddHeader adds a header to a SMTP configuration
func (c *SMTPConfiguration) AddHeader(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.SMTPHeader
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save header
	smtpID, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	createdID, err := c.SMTPConfigurationService.AddHeader(
		g.Request.Context(),
		session,
		smtpID,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{
		"id": createdID.String(),
	})
}

// RemoveHeader removes a header from a SMTP configuration
func (c *SMTPConfiguration) RemoveHeader(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	headerID, err := uuid.Parse(g.Param("headerID"))
	if err != nil {
		c.Logger.Debugw("invalid header id",
			"headerID", g.Param("headerID"),
			"error", err,
		)
		c.Response.BadRequestMessage(g, api.InvalidSMTPConfigurationID)
		return
	}
	// remove header
	err = c.SMTPConfigurationService.RemoveHeader(
		g.Request.Context(),
		session,
		id,
		&headerID,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// DeleteByID deletes a SMTP configuration
func (c *SMTPConfiguration) DeleteByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete
	err := c.SMTPConfigurationService.DeleteByID(g, session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

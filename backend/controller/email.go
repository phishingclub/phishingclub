package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
)

// EmailOrderByMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var EmailOrderByMap = map[string]string{
	"created_at":     repository.TableColumn(database.EMAIL_TABLE, "created_at"),
	"updated_at":     repository.TableColumn(database.EMAIL_TABLE, "created_at"),
	"name":           repository.TableColumn(database.EMAIL_TABLE, "name"),
	"mail_from":      repository.TableColumn(database.EMAIL_TABLE, "mail_from"),
	"from":           repository.TableColumn(database.EMAIL_TABLE, "from"),
	"subject":        repository.TableColumn(database.EMAIL_TABLE, "subject"),
	"tracking_pixel": repository.TableColumn(database.EMAIL_TABLE, "add_tracking_pixel"),
}

// AddAttachmentsToEmailRequest is a request to add attachments to a message
type AddAttachmentsToEmailRequest struct {
	Attachments []string `json:"ids"` // attachment IDs
}

// RemoveAttachmentFromEmailRequest is a request to remove an attachment from a message
type RemoveAttachmentFromEmailRequest struct {
	AttachmentID string `json:"attachmentID"`
}

// SendTestEmailRequest is a request for sending a test of an e-mail
type SendTestEmailRequest struct {
	SMTPID      *uuid.UUID
	DomainID    *uuid.UUID
	RecipientID *uuid.UUID
}

// Email is a Email controller
type Email struct {
	Common
	EmailService    *service.Email
	TemplateService *service.Template
	EmailRepository *repository.Email
}

// AddAttachments adds attachments to a email
func (m *Email) AddAttachments(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var request AddAttachmentsToEmailRequest
	if ok := m.handleParseRequest(g, &request); !ok {
		return
	}
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	if len(request.Attachments) == 0 {
		m.Response.BadRequestMessage(g, "No attachments provided")
		return
	}
	attachmentIDs := []*uuid.UUID{}
	for _, idParam := range request.Attachments {
		id, err := uuid.Parse(idParam)
		if err != nil {
			m.Logger.Debugw(errs.MsgFailedToParseUUID,
				"error", err,
			)
			m.Response.BadRequestMessage(g, "Invalid attachment ID")
			return
		}
		attachmentIDs = append(attachmentIDs, &id)
	}
	// add attachments to email
	err := m.EmailService.AddAttachments(
		g.Request.Context(),
		session,
		id,
		attachmentIDs,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, gin.H{})
}

// RemoveAttachment removes an attachment from a email
func (m *Email) RemoveAttachment(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req RemoveAttachmentFromEmailRequest
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	attachmentID, err := uuid.Parse(req.AttachmentID)
	if err != nil {
		m.Logger.Debugw(errs.MsgFailedToParseUUID,
			"error", err,
		)
		m.Response.BadRequestMessage(g, "Invalid attachment ID")
		return
	}
	emailID, err := uuid.Parse(g.Param("id"))
	if err != nil {
		m.Logger.Debugw(errs.MsgFailedToParseUUID, "error", err)
		m.Response.BadRequestMessage(g, "Invalid message ID")
		return
	}
	// remove attachment from email
	err = m.EmailService.RemoveAttachment(
		g.Request.Context(),
		session,
		&emailID,
		&attachmentID,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, gin.H{})
}

// Create creates a email
func (m *Email) Create(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Email
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	// save email
	id, err := m.EmailService.Create(
		g,
		session,
		&req,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, gin.H{
		"id": id,
	})
}

// SendTestEmail
func (m *Email) SendTestEmail(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	var req SendTestEmailRequest
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	// send test email
	err := m.EmailService.SendTestEmail(
		g,
		session,
		id,
		req.SMTPID,
		req.DomainID,
		req.RecipientID,
		companyID,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, gin.H{})
}

// GetByID gets a email by ID
func (m *Email) GetByID(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	// get email
	email, err := m.EmailService.GetByID(
		g.Request.Context(),
		session,
		id,
		companyID,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, email)
}

// GetContentByID gets a email content by ID
func (m *Email) GetContentByID(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	// get
	email, err := m.EmailService.GetByID(
		g.Request.Context(),
		session,
		id,
		companyID,
	)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	// build email
	domain := &model.Domain{
		Name: nullable.NewNullableWithValue(
			*vo.NewString255Must("example.test"),
		),
	}
	recipient := model.NewRecipientExample()
	campaignRecipient := model.CampaignRecipient{
		ID: nullable.NewNullableWithValue(
			uuid.New(),
		),
		Recipient: recipient,
	}
	apiSender := model.NewAPISenderExample()
	emailBody, err := m.TemplateService.CreateMailBody(
		"id",
		"/foo",
		domain,
		&campaignRecipient,
		email,
		apiSender,
	)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, emailBody)
}

// GetAll gets all emails using pagination
func (m *Email) GetAll(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := m.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByName()
	queryArgs.RemapOrderBy(EmailOrderByMap)
	emails, err := m.EmailService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		queryArgs,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return

	}
	m.Response.OK(g, emails)
}

// GetOverviews gets all email overviews using pagination
func (m *Email) GetOverviews(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := m.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(EmailOrderByMap)
	queryArgs.DefaultSortByName()
	emails, err := m.EmailService.GetOverviews(
		g.Request.Context(),
		session,
		companyID,
		queryArgs,
	)
	// handle responses
	if ok := m.handleErrors(g, err); !ok {
		return

	}
	m.Response.OK(g, emails)
}

// UpdateByID updates a message by ID
func (m *Email) UpdateByID(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	var email model.Email
	if ok := m.handleParseRequest(g, &email); !ok {
		return
	}
	// update message
	err := m.EmailService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&email,
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, gin.H{})
}

// DeleteByID deletes a message by ID
func (m *Email) DeleteByID(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete message
	err := m.EmailService.DeleteByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, gin.H{})
}

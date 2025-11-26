package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"github.com/wneessen/go-mail"
	"gorm.io/gorm"
)

// Email is a Email service
type Email struct {
	Common
	EmailRepository   *repository.Email
	SMTPService       *SMTPConfiguration
	DomainService     *Domain
	RecipientService  *Recipient
	TemplateService   *Template
	AttachmentService *Attachment
	AttachmentPath    string
}

// AddAttachments adds an attachments to a message
func (m *Email) AddAttachments(
	ctx context.Context,
	session *model.Session,
	messageID *uuid.UUID,
	attachmentIDs []*uuid.UUID,
) error {
	ae := NewAuditEvent("Email.AddAttachments", session)
	ae.Details["messageId"] = messageID.String()

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.Logger.Errorw("failed to get email id", "error", err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// TODO check if the user is privliged for the message
	_, err = m.EmailRepository.GetByID(
		ctx,
		messageID,
		&repository.EmailOption{},
	)
	if err != nil {
		m.Logger.Errorw("failed to add attachment to email", "error", err)
		return errs.Wrap(err)
	}
	// add attachment to message
	attachmentIdsStr := []string{}
	for _, attachmentID := range attachmentIDs {
		attachmentIdsStr = append(attachmentIdsStr, attachmentID.String())
		// get the message to ensure it exists and the user is privliged
		_, err = m.AttachmentService.GetByID(
			ctx,
			session,
			attachmentID,
		)
		if err != nil {
			m.Logger.Errorw("failed to add attachment to email", "error", err)
			return errs.Wrap(err)
		}
		err = m.EmailRepository.AddAttachment(
			ctx,
			messageID,
			attachmentID,
		)
		if err != nil {
			m.Logger.Errorw("failed to add attachment to email", "error", err)
			return errs.Wrap(err)
		}
	}
	ae.Details["attachmentIds"] = attachmentIdsStr
	m.AuditLogAuthorized(ae)
	return nil
}

// RemoveAttachment removes an attachment from a email
func (m *Email) RemoveAttachment(
	ctx context.Context,
	session *model.Session,
	emailID *uuid.UUID,
	attachmentID *uuid.UUID,
) error {
	ae := NewAuditEvent("Email.RemoveAttachment", session)
	ae.Details["emailId"] = emailID.String()
	ae.Details["attachmentId"] = attachmentID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// TODO check if the user is privliged for the email
	_, err = m.EmailRepository.GetByID(
		ctx,
		emailID,
		&repository.EmailOption{},
	)
	if err != nil {
		m.Logger.Errorw("failed to remove attachment from email", "error", err)
		return errs.Wrap(err)
	}
	// get the email to ensure it exists and the user is privliged
	_, err = m.EmailRepository.GetByID(
		ctx,
		emailID,
		&repository.EmailOption{},
	)
	if err != nil {
		m.Logger.Errorw("failed to remove attachment from email", "error", err)
		return errs.Wrap(err)
	}
	// remove attachment from email
	err = m.EmailRepository.RemoveAttachment(
		ctx,
		emailID,
		attachmentID,
	)
	if err != nil {
		m.Logger.Errorw("failed to remove attachment from email", "error", err)
		return errs.Wrap(err)
	}
	m.AuditLogAuthorized(ae)
	return nil
}

// Create creates a new email
func (m *Email) Create(
	ctx context.Context,
	session *model.Session,
	email *model.Email,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Email.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)

		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// validate
	if err := email.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	// validate template content if present
	if content, err := email.Content.Get(); err == nil {
		if err := m.TemplateService.ValidateEmailTemplate(content.String()); err != nil {
			m.Logger.Errorw("failed to validate email template", "error", err)
			return nil, validate.WrapErrorWithField(errors.New("invalid template: "+err.Error()), "content")
		}
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := email.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	name := email.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		m.EmailRepository.DB,
		"emails",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		m.Logger.Errorw("failed to create email", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		m.Logger.Debugw("email name is already taken", "error", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// handle tracking pixel
	email, err = m.toggleTrackingPixel(email)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// create email
	emailID, err := m.EmailRepository.Insert(
		ctx,
		email,
	)
	if err != nil {
		m.Logger.Errorw("failed to create email", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = emailID.String()
	m.AuditLogAuthorized(ae)

	return emailID, nil
}

func (m *Email) toggleTrackingPixel(
	email *model.Email,
) (*model.Email, error) {
	// add tracking pixel
	addTrackingPixel, err := email.AddTrackingPixel.Get()
	if err != nil {
		return email, nil
	}
	c, err := email.Content.Get()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	var tmp string
	if !addTrackingPixel {
		tmp = m.TemplateService.RemoveTrackingPixelFromContent(c.String())
	} else {
		tmp = m.TemplateService.AddTrackingPixel(c.String())
	}
	b, err := vo.NewOptionalString1MB(tmp)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	email.Content.Set(*b)
	return email, nil
}

// GetAll gets all emails by pagination with optional company id
func (m *Email) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Email], error) {
	result := model.NewEmptyResult[model.Email]()
	ae := NewAuditEvent("Email.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get all emails
	emails, err := m.EmailRepository.GetAll(
		ctx,
		companyID,
		&repository.EmailOption{
			QueryArgs: queryArgs,
		},
	)
	if err != nil {
		m.Logger.Errorw("failed to get emails", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return emails, nil
}

// GetOverviews gets all email overviews
func (m *Email) GetOverviews(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Email], error) {
	result := model.NewEmptyResult[model.Email]()
	ae := NewAuditEvent("Email.GetOverviews", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get all emails
	result, err = m.EmailRepository.GetOverviews(
		ctx,
		companyID,
		&repository.EmailOption{
			QueryArgs: queryArgs,
		},
	)
	if err != nil {
		m.Logger.Errorw("failed to get emails", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetByID gets a email by id
func (m *Email) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	companyID *uuid.UUID,
) (*model.Email, error) {
	ae := NewAuditEvent("Email.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get email by id
	email, err := m.EmailRepository.GetByID(
		ctx,
		id,
		&repository.EmailOption{
			WithAttachments: false, // we'll load attachments manually with context filtering
		},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to get email by id", "error", err)
		return nil, errs.Wrap(err)
	}

	// load attachments with proper context filtering
	err = m.loadEmailAttachmentsWithContext(ctx, email, companyID)
	if err != nil {
		m.Logger.Errorw("failed to load email attachments with context", "error", err)
		return nil, errs.Wrap(err)
	}

	// no audit on read
	return email, nil
}

// GetByCompanyID gets a emails by company id
func (m *Email) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.Result[model.Email], error) {
	result := model.NewEmptyResult[model.Email]()
	ae := NewAuditEvent("Email.GetByCompanyID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get emails by id
	result, err = m.EmailRepository.GetAllByCompanyID(
		ctx,
		companyID,
		&repository.EmailOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not an error
		return result, errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to get email by id", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// SendTestEmail sends a test email
func (m *Email) SendTestEmail(
	ctx context.Context,
	session *model.Session,
	emailID *uuid.UUID,
	smtpID *uuid.UUID,
	domainID *uuid.UUID,
	recpID *uuid.UUID,
	companyID *uuid.UUID,
) error {
	ae := NewAuditEvent("Email.SendTestEmail", session)
	ae.Details["emailId"] = emailID.String()
	ae.Details["smtpId"] = smtpID.String()
	ae.Details["recipientId"] = recpID.String()
	ae.Details["domainID"] = domainID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get recipient by id
	recipient, err := m.RecipientService.GetByID(
		ctx,
		session,
		recpID,
		&repository.RecipientOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Infow("failed to send test email - recipient not found",
			"recipientID", recpID.String(),
		)
		return errs.Wrap(err)
	}
	// get smtp by id
	smtp, err := m.SMTPService.GetByID(ctx, session, smtpID, &repository.SMTPConfigurationOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Infow("failed to send test email -  stmp not found",
			"SMTPID", smtpID.String(),
		)
		return errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to send test email, smtp not found", "error", err)
		return err
	}
	// get domain by id
	testDomain, err := m.DomainService.GetByID(ctx, session, domainID, &repository.DomainOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Infow("failed to send test email -  domain not found",
			"DomainID", domainID.String(),
		)
		return errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to send test email, domain not found", "error", err)
		return err
	}
	// get email by id
	email, err := m.EmailRepository.GetByID(
		ctx,
		emailID,
		&repository.EmailOption{
			WithAttachments: false, // we'll load attachments manually with context filtering
		},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Infow("failed to send test email - email not found",
			"emailID", emailID.String(),
		)
		return errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to send test email - email not found", "error", err)
		return errs.Wrap(err)
	}

	// load attachments with proper context filtering
	err = m.loadEmailAttachmentsWithContext(ctx, email, companyID)
	if err != nil {
		m.Logger.Errorw("failed to load email attachments with context for test email", "error", err)
		return errs.Wrap(err)
	}
	campaignRecipient := &model.CampaignRecipient{
		ID:        nullable.NewNullableWithValue(uuid.New()),
		Recipient: recipient,
	}
	smtpPort, err := smtp.Port.Get()
	if err != nil {
		m.Logger.Errorw("failed to get smtp port", "error", err)
		return errs.Wrap(err)
	}
	smtpHost, err := smtp.Host.Get()
	if err != nil {
		m.Logger.Errorw("failed to get smtp host", "error", err)
		return errs.Wrap(err)
	}
	smtpIgnoreCertErrors, err := smtp.IgnoreCertErrors.Get()
	if err != nil {
		m.Logger.Errorw("failed to get smtp ignore cert errors", "error", err)
		return errs.Wrap(err)
	}
	emailOptions := []mail.Option{
		mail.WithPort(smtpPort.Int()),
		mail.WithTLSConfig(
			&tls.Config{
				ServerName: smtpHost.String(),
				// #nosec
				InsecureSkipVerify: smtpIgnoreCertErrors,
				// MinVersion:         tls.VersionTLS12,

			},
		),
	}
	// setup authentication if provided
	username, err := smtp.Username.Get()
	if err != nil {
		m.Logger.Errorw("failed to get smtp username", "error", err)
		return errs.Wrap(err)
	}
	password, err := smtp.Password.Get()
	if err != nil {
		m.Logger.Errorw("failed to get smtp password", "error", err)
		return errs.Wrap(err)
	}
	if un := username.String(); len(un) > 0 {
		emailOptions = append(
			emailOptions,
			mail.WithUsername(
				un,
			),
		)
		if pw := password.String(); len(pw) > 0 {
			emailOptions = append(
				emailOptions,
				mail.WithPassword(
					pw,
				),
			)
		}
	}
	// prepare message
	messageOptions := []mail.MsgOption{
		mail.WithNoDefaultUserAgent(),
	}
	msg := mail.NewMsg(messageOptions...)
	err = msg.EnvelopeFrom(email.MailEnvelopeFrom.MustGet().String())
	if err != nil {
		m.Logger.Errorw("failed to set envelope from", "error", err)
		return errs.Wrap(err)
	}
	// headers
	err = msg.From(email.MailHeaderFrom.MustGet().String())
	if err != nil {
		m.Logger.Errorw("failed to set mail header 'From'", "error", err)
		return errs.Wrap(err)
	}
	recpEmail := campaignRecipient.Recipient.Email.MustGet().String()
	err = msg.To(recpEmail)
	if err != nil {
		m.Logger.Errorw("failed to set mail header 'To'", "error", err)
		return errs.Wrap(err)
	}
	// custom headers
	if headers := smtp.Headers; headers != nil {
		for _, header := range headers {
			key := header.Key.MustGet()
			value := header.Value.MustGet()
			msg.SetGenHeader(
				mail.Header(key.String()),
				value.String(),
			)
		}
	}
	domainName, err := testDomain.Name.Get()
	if err != nil {
		m.Logger.Errorw("failed to get domain name", "error", err)
		return errs.Wrap(err)
	}

	// create template
	content, err := email.Content.Get()
	if err != nil {
		m.Logger.Errorw("failed to get message content", "error", err)
		return errs.Wrap(err)
	}

	mailTmpl, err := template.
		New("email").
		Funcs(TemplateFuncs()).
		Parse(content.String())

	if err != nil {
		m.Logger.Errorw("failed to parse email template", "error", err)
		return errs.Wrap(err)
	}
	t := m.TemplateService.CreateMail(
		ctx,
		domainName.String(),
		"id",
		"/",
		campaignRecipient,
		email,
		nil,
		companyID,
	)

	// process subject through template
	subjectTemplate, err := template.New("subject").Funcs(m.TemplateService.TemplateFuncsWithCompany(ctx, companyID)).Parse(email.MailHeaderSubject.MustGet().String())
	if err != nil {
		m.Logger.Errorw("failed to parse subject template", "error", err)
		return errs.Wrap(err)
	}
	var subjectBuffer bytes.Buffer
	err = subjectTemplate.Execute(&subjectBuffer, t)
	if err != nil {
		m.Logger.Errorw("failed to execute subject template", "error", err)
		return errs.Wrap(err)
	}
	msg.Subject(subjectBuffer.String())

	var bodyBuffer bytes.Buffer
	err = mailTmpl.Execute(&bodyBuffer, t)
	if err != nil {
		m.Logger.Errorw("failed to execute mail template", "error", err)
		return err
	}
	msg.SetBodyString("text/html", bodyBuffer.String())
	// attachments
	attachments := email.Attachments
	for _, attachment := range attachments {
		p, err := m.AttachmentService.GetPath(attachment)
		if err != nil {
			return fmt.Errorf("failed to get attachment path: %s", err)
		}
		if !attachment.EmbeddedContent.MustGet() {
			msg.AttachFile(p.String())
		} else {
			attachmentContent, err := os.ReadFile(p.String())
			if err != nil {
				return errs.Wrap(err)
			}
			// hacky setup of attachment for executing as email template
			attachmentAsEmail := model.Email{
				ID:                email.ID,
				CreatedAt:         email.CreatedAt,
				UpdatedAt:         email.UpdatedAt,
				Name:              email.Name,
				MailEnvelopeFrom:  email.MailEnvelopeFrom,
				MailHeaderFrom:    email.MailHeaderFrom,
				MailHeaderSubject: email.MailHeaderSubject,
				Content:           email.Content,
				AddTrackingPixel:  email.AddTrackingPixel,
				CompanyID:         email.CompanyID,
				Attachments:       email.Attachments,
				Company:           email.Company,
			}
			// really hacky / unsafe
			attachmentAsEmail.Content = nullable.NewNullableWithValue(
				*vo.NewUnsafeOptionalString1MB(string(attachmentContent)),
			)
			attachmentStr, err := m.TemplateService.CreateMailBody(
				ctx,
				"id",
				"/",
				testDomain,
				campaignRecipient,
				&attachmentAsEmail,
				nil,
				companyID,
			)
			if err != nil {
				return fmt.Errorf("failed to setup attachment with embedded content: %s", err)
			}
			msg.AttachReadSeeker(
				filepath.Base(p.String()),
				strings.NewReader(attachmentStr),
			)
		}
	}
	// the client sends all the messages and ensure that all messages are sent
	// in the same connection
	var mc *mail.Client

	// Try different authentication methods based on configuration
	// If username is provided, use authentication; otherwise try without auth first
	if un := username.String(); len(un) > 0 {
		// Try CRAM-MD5 first when credentials are provided
		emailOptionsCRAM5 := append(emailOptions, mail.WithSMTPAuth(mail.SMTPAuthCramMD5))
		mc, _ = mail.NewClient(smtpHost.String(), emailOptionsCRAM5...)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(ctx, msg)

		// Check if it's an authentication error and try PLAIN auth
		if err != nil && (strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "534 ") ||
			strings.Contains(err.Error(), "538 ") ||
			strings.Contains(err.Error(), "CRAM-MD5") ||
			strings.Contains(err.Error(), "authentication failed")) {
			m.Logger.Warnw("CRAM-MD5 authentication failed, trying PLAIN auth", "error", err)
			emailOptionsBasic := emailOptions
			if build.Flags.Production {
				emailOptionsBasic = append(emailOptions, mail.WithSMTPAuth(mail.SMTPAuthPlain))
			}
			mc, _ = mail.NewClient(smtpHost.String(), emailOptionsBasic...)
			if build.Flags.Production {
				mc.SetTLSPolicy(mail.TLSMandatory)
			} else {
				mc.SetTLSPolicy(mail.TLSOpportunistic)
			}
			err = mc.DialAndSendWithContext(ctx, msg)
		}
	} else {
		// No credentials provided, try without authentication (e.g., local postfix)
		mc, _ = mail.NewClient(smtpHost.String(), emailOptions...)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(ctx, msg)

		// If no-auth fails and we get an auth-related error, log it appropriately
		if err != nil && (strings.Contains(err.Error(), "530 ") ||
			strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "authentication required") ||
			strings.Contains(err.Error(), "AUTH")) {
			m.Logger.Warnw("Server requires authentication but no credentials provided", "error", err)
		}
	}
	if err != nil {
		m.Logger.Errorw("failed to send test email", "error", err)
		if msg.HasSendError() {
			m.Logger.Errorw("failed to send test email", "error", msg.SendError())
			return msg.SendError()
		}
		return err
	}
	m.AuditLogAuthorized(ae)
	return nil
}

// UpdateByID updates a email by id
func (m *Email) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	email *model.Email,
) error {
	ae := NewAuditEvent("Email.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.Wrap(errs.ErrAuthorizationFailed)
	}
	// get current by id
	current, err := m.EmailRepository.GetByID(
		ctx,
		id,
		&repository.EmailOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Debugw("failed to update email by ID", "error", err)
		return errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to update email by ID", "error", err)
		return errs.Wrap(err)
	}
	var companyID *uuid.UUID
	if cid, err := email.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	// check uniqueness
	name := email.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		m.EmailRepository.DB,
		"emails",
		name.String(),
		companyID,
		id,
	)
	if err != nil {
		m.Logger.Errorw("failed to create email", "error", err)
		return errs.Wrap(err)
	}
	if !isOK {
		m.Logger.Debugw("email name is already taken", "name", name.String())
		return validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// update email - if a field is present and not null, update it
	if v, err := email.Name.Get(); err == nil {
		current.Name.Set(v)
	}
	if v, err := email.MailEnvelopeFrom.Get(); err == nil {
		current.MailEnvelopeFrom.Set(v)
	}
	if v, err := email.MailHeaderFrom.Get(); err == nil {
		current.MailHeaderFrom.Set(v)
	}
	if v, err := email.MailHeaderSubject.Get(); err == nil {
		current.MailHeaderSubject.Set(v)
	}
	if v, err := email.Content.Get(); err == nil {
		// validate template content before updating
		if err := m.TemplateService.ValidateEmailTemplate(v.String()); err != nil {
			m.Logger.Errorw("failed to validate email template", "error", err)
			return validate.WrapErrorWithField(errors.New("invalid template: "+err.Error()), "content")
		}
		if _, err := email.AddTrackingPixel.Get(); err == nil {
			// handle tracking pixel
			email, err = m.toggleTrackingPixel(email)
			if err != nil {
				return errs.Wrap(err)
			}
			current.Content.Set(email.Content.MustGet())
		} else {
			current.Content.Set(v)
		}
	}
	if v, err := email.AddTrackingPixel.Get(); err == nil {
		current.AddTrackingPixel.Set(v)
	}
	if v, err := email.CompanyID.Get(); err == nil {
		current.CompanyID.Set(v)
	}
	// validate change
	if err := current.Validate(); err != nil {
		m.Logger.Errorw("failed to update email by ID", "error", err)
		return errs.Wrap(err)
	}
	// update email
	err = m.EmailRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		m.Logger.Errorw("failed to update email by ID", "error", err)
		return errs.Wrap(err)
	}
	m.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes a email by id
func (m *Email) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Email.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// delete email by id
	err = m.EmailRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		m.Logger.Errorw("failed to delete message by id", "error", err)
		return errs.Wrap(err)
	}
	m.AuditLogAuthorized(ae)

	return nil
}

// loadEmailAttachmentsWithContext loads attachments for an email with proper context filtering
// this ensures that in global context, only global attachments are loaded
// and in company context, both global and company-specific attachments are loaded
func (m *Email) loadEmailAttachmentsWithContext(
	ctx context.Context,
	email *model.Email,
	companyID *uuid.UUID,
) error {
	// get all attachment IDs associated with this email
	attachmentIDs, err := m.EmailRepository.GetAttachmentIDsByEmailID(ctx, email.ID.MustGet())
	if err != nil {
		return errs.Wrap(err)
	}

	// if no attachments, nothing to do
	if len(attachmentIDs) == 0 {
		email.Attachments = []*model.Attachment{}
		return nil
	}

	// get attachments with proper context filtering
	contextFilteredAttachments := []*model.Attachment{}
	for _, attachmentID := range attachmentIDs {
		attachment, err := m.AttachmentService.AttachmentRepository.GetByID(ctx, attachmentID)
		if err != nil {
			// if attachment doesn't exist, log and continue
			m.Logger.Debugw("attachment not found", "attachmentID", attachmentID, "error", err)
			continue
		}

		// apply context filtering logic
		isAttachmentAccessible := false
		if companyID == nil {
			// global context - only allow global attachments (company_id IS NULL)
			isAttachmentAccessible = !attachment.CompanyID.IsSpecified() || attachment.CompanyID.IsNull()
		} else {
			// company context - allow both global and company-specific attachments
			if !attachment.CompanyID.IsSpecified() || attachment.CompanyID.IsNull() {
				// global attachment
				isAttachmentAccessible = true
			} else {
				// company-specific attachment - check if it matches the context
				attachmentCompanyID := attachment.CompanyID.MustGet()
				isAttachmentAccessible = attachmentCompanyID == *companyID
			}
		}

		if isAttachmentAccessible {
			contextFilteredAttachments = append(contextFilteredAttachments, attachment)
		}
	}

	email.Attachments = contextFilteredAttachments
	return nil
}

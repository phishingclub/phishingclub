package service

import (
	"context"
	"crypto/tls"
	"strings"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// SMTPConfiguration is a SMTP configuration service
type SMTPConfiguration struct {
	Common
	SMTPConfigurationRepository *repository.SMTPConfiguration
	CampaignTemplateService     *CampaignTemplate
}

// Create creates a new SMTP configuration
func (s *SMTPConfiguration) Create(
	ctx context.Context,
	session *model.Session,
	conf *model.SMTPConfiguration,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("SMTPConfiguration.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// validate data
	if err := conf.Validate(); err != nil {
		s.Logger.Errorw("failed to validate SMTP configuration", "error", err)
		return nil, errs.Wrap(err)
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := conf.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	name := conf.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		s.SMTPConfigurationRepository.DB,
		"smtp_configurations",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		s.Logger.Errorw("failed to check SMTP uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		s.Logger.Debugw("smtp configuration name is already taken", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// create config
	id, err := s.SMTPConfigurationRepository.Insert(
		ctx,
		conf,
	)
	if err != nil {
		s.Logger.Errorw("failed to create SMTP configuration", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	s.AuditLogAuthorized(ae)

	return id, nil
}

// GetAll gets SMTP configurations
func (s *SMTPConfiguration) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.SMTPConfigurationOption,
) (*model.Result[model.SMTPConfiguration], error) {
	result := model.NewEmptyResult[model.SMTPConfiguration]()
	ae := NewAuditEvent("SMTPConfiguration.GetAll", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get
	result, err = s.SMTPConfigurationRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		s.Logger.Errorw("failed to get SMTP configurations", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetByID gets a SMTP configuration by ID
func (s *SMTPConfiguration) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.SMTPConfigurationOption,
) (*model.SMTPConfiguration, error) {
	ae := NewAuditEvent("SMTPConfiguration.GetByID", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get config
	config, err := s.SMTPConfigurationRepository.GetByID(
		ctx,
		id,
		options,
	)
	if err != nil {
		s.Logger.Errorw("failed to get SMTP configuration", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return config, nil
}

// SendTestEmail tests a SMTP configuration by ID
func (s *SMTPConfiguration) SendTestEmail(
	g *gin.Context,
	session *model.Session,
	id *uuid.UUID,
	to *vo.Email,
	from *vo.Email,
) error {
	ae := NewAuditEvent("SMTPConfiguration.SendTestEmail", session)
	ae.Details["id"] = id.String()
	ae.Details["to-email"] = to.String()

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	smtpConfig, err := s.GetByID(
		g,
		session,
		id,
		&repository.SMTPConfigurationOption{
			WithHeaders: true,
		},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("smtp configuration did not load", "error", err)
		return err
	}
	smtpPort, err := smtpConfig.Port.Get()
	if err != nil {
		s.Logger.Errorw("failed to get smtp port", "error", err)
		return err
	}
	smtpHost, err := smtpConfig.Host.Get()
	if err != nil {
		s.Logger.Errorw("failed to get smtp host", "error", err)
		return err
	}
	smtpIgnoreCertErrors, err := smtpConfig.IgnoreCertErrors.Get()
	if err != nil {
		s.Logger.Errorw("failed to get smtp ignore cert errors", "error", err)
		return err
	}
	m := mail.NewMsg(mail.WithNoDefaultUserAgent())
	err = m.EnvelopeFrom(from.String())
	if err != nil {
		s.Logger.Errorw("failed to set envelope from", "error", err)
		return err
	}
	// headers
	err = m.From(from.String())
	if err != nil {
		s.Logger.Errorw("failed to set mail header 'From'", "error", err)
		return err
	}
	err = m.To(to.String())
	if err != nil {
		s.Logger.Errorw("failed to set mail header 'To'", "error", err)
		return err
	}
	if headers := smtpConfig.Headers; headers != nil {
		for _, header := range headers {
			key := header.Key.MustGet()
			value := header.Value.MustGet()
			m.SetGenHeader(
				mail.Header(key.String()),
				value.String(),
			)
		}
	}
	m.Subject("Configuration Test")
	m.SetBodyString("text/html",
		`<i>This is a test email to verify the SMTP configuration.</i>`,
	)
	// setup client
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
	username, err := smtpConfig.Username.Get()
	if err != nil {
		s.Logger.Errorw("failed to get smtp username", "error", err)
		return err
	}
	password, err := smtpConfig.Password.Get()
	if err != nil {
		s.Logger.Errorw("failed to get smtp password", "error", err)
		return err
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
	// send mail
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
		err = mc.DialAndSendWithContext(g, m)

		// Check if it's an authentication error and try PLAIN auth
		if err != nil && (strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "534 ") ||
			strings.Contains(err.Error(), "538 ") ||
			strings.Contains(err.Error(), "CRAM-MD5") ||
			strings.Contains(err.Error(), "authentication failed")) {
			s.Logger.Warnw("CRAM-MD5 authentication failed, trying PLAIN auth", "error", err)
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
			err = mc.DialAndSendWithContext(g, m)
		}
	} else {
		// No credentials provided, try without authentication (e.g., local postfix)
		mc, _ = mail.NewClient(smtpHost.String(), emailOptions...)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(g, m)

		// If no-auth fails and we get an auth-related error, log it appropriately
		if err != nil && (strings.Contains(err.Error(), "530 ") ||
			strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "authentication required") ||
			strings.Contains(err.Error(), "AUTH")) {
			s.Logger.Warnw("Server requires authentication but no credentials provided", "error", err)
		}
	}
	if err != nil {
		s.Logger.Errorw("failed to send test email", "error", err)
		if m.HasSendError() {
			s.Logger.Errorw("failed to send test email", "error", m.SendError())
			return m.SendError()
		}
		return err
	}
	s.AuditLogAuthorized(ae)

	return nil
}

// GetByNameAndCompanyID gets a SMTP configuration by name
func (s *SMTPConfiguration) GetByNameAndCompanyID(
	ctx context.Context,
	session *model.Session,
	name *vo.String127,
	companyID *uuid.UUID, // is nullable
	options *repository.SMTPConfigurationOption,
) (*model.SMTPConfiguration, error) {
	ae := NewAuditEvent("SMTPConfiguration.GetByNameAndCompanyID", session)
	if name != nil {
		ae.Details["name"] = name.String()
	}
	if companyID != nil {
		ae.Details["companyId"] = companyID
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get
	config, err := s.SMTPConfigurationRepository.GetByNameAndCompanyID(
		ctx,
		name,
		companyID,
		options,
	)
	if err != nil {
		s.Logger.Errorw("failed to get SMTP configuration", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return config, nil
}

// UpdateByID updates a SMTP configuration by ID
func (s *SMTPConfiguration) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.SMTPConfiguration,
) error {
	ae := NewAuditEvent("SMTPConfiguration.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get current
	current, err := s.SMTPConfigurationRepository.GetByID(
		ctx,
		id,
		&repository.SMTPConfigurationOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("SMTP configuration not found", "error", id)
		return err
	}
	if err != nil {
		s.Logger.Errorw("failed to update SMTP configuration", "error", err)
		return err
	}
	// update config - if a field is present and not null, update it
	if v, err := incoming.Name.Get(); err == nil {
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		// check uniqueness
		name := incoming.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			s.SMTPConfigurationRepository.DB,
			"smtp_configurations",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			s.Logger.Errorw("failed to check SMTP uniqueness", "error", err)
			return err
		}
		if !isOK {
			s.Logger.Debugw("smtp configuration name is already taken", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		current.Name.Set(v)

	}
	if v, err := incoming.Host.Get(); err == nil {
		current.Host.Set(v)
	}
	if v, err := incoming.Port.Get(); err == nil {
		current.Port.Set(v)
	}
	if v, err := incoming.Username.Get(); err == nil {
		current.Username.Set(v)
	}
	if v, err := incoming.Password.Get(); err == nil {
		current.Password.Set(v)
	}
	if v, err := incoming.IgnoreCertErrors.Get(); err == nil {
		current.IgnoreCertErrors.Set(v)
	}
	if err := incoming.Validate(); err != nil {
		s.Logger.Errorw("failed to update SMTP configuration", "error", err)
		return err
	}
	// update
	err = s.SMTPConfigurationRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		s.Logger.Errorw("failed to update SMTP configuration", "error", err)
		return err
	}
	s.AuditLogAuthorized(ae)

	return nil
}

// AddHeader adds a header to a SMTP configuration
func (s *SMTPConfiguration) AddHeader(
	ctx context.Context,
	session *model.Session,
	smtpID *uuid.UUID,
	header *model.SMTPHeader,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("SMTPConfiguration.AddHeader", session)
	ae.Details["id"] = smtpID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// ensure config exists
	_, err = s.SMTPConfigurationRepository.GetByID(
		ctx,
		smtpID,
		&repository.SMTPConfigurationOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Debugw("SMTP configuration not found", "error", smtpID)
		return nil, errs.Wrap(err)
	}
	if err != nil {
		s.Logger.Errorw("failed to add header to SMTP configuration", "error", err)
		return nil, errs.Wrap(err)
	}
	header.SmtpID.Set(*smtpID)
	// validate header
	if err := header.Validate(); err != nil {
		s.Logger.Errorw("failed to validate SMTP header", "error", err)
		return nil, errs.Wrap(err)
	}
	// save header to configuration
	headerID, err := s.SMTPConfigurationRepository.AddHeader(
		ctx,
		header,
	)
	if err != nil {
		s.Logger.Errorw("failed to add header to SMTP configuration", "error", err)
		return nil, errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)

	return headerID, nil
}

// RemoveHeader removes a header from a SMTP configuration
func (s *SMTPConfiguration) RemoveHeader(
	ctx context.Context,
	session *model.Session,
	smtpID *uuid.UUID,
	headerID *uuid.UUID,
) error {
	ae := NewAuditEvent("SMTPConfiguration.RemoveHeader", session)
	ae.Details["smtpId"] = smtpID.String()
	ae.Details["headerId"] = headerID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get config, ensure config exists
	_, err = s.SMTPConfigurationRepository.GetByID(
		ctx,
		smtpID,
		&repository.SMTPConfigurationOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Debugw("SMTP configuration not found", "error", smtpID)
		return err
	}
	if err != nil {
		s.Logger.Errorw("failed to remove header from SMTP configuration", "error", err)
		return err
	}
	// remove header
	err = s.SMTPConfigurationRepository.RemoveHeader(
		ctx,
		headerID,
	)
	if err != nil {
		s.Logger.Errorw("failed to remove header from SMTP configuration", "error", err)
		return err
	}
	s.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes a SMTP configuration by ID
// including all headers attached to it
func (s *SMTPConfiguration) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("SMTPConfiguration.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// delete the relation from the campaign templates
	err = s.CampaignTemplateService.RemoveSmtpBySmtpID(
		ctx,
		session,
		id,
	)
	if err != nil {
		s.Logger.Errorw("failed to remove SMTP configuration relation from campaign templates",
			"error", err,
		)
		return err
	}
	// delete config
	err = s.SMTPConfigurationRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		s.Logger.Errorw("failed to delete SMTP configuration", "error", err)
		return err
	}
	s.AuditLogAuthorized(ae)

	return nil
}

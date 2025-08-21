package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Company is the Company service
type Company struct {
	Common
	DomainService            *Domain
	PageService              *Page
	EmailService             *Email
	SMTPConfigurationService *SMTPConfiguration
	APISenderService         *APISender
	RecipientService         *Recipient
	RecipientGroupService    *RecipientGroup
	CampaignService          *Campaign
	CampaignTemplate         *CampaignTemplate
	AllowDenyService         *AllowDeny
	WebhookService           *Webhook
	CompanyRepository        *repository.Company
}

// Create creates a company
func (s *Company) Create(
	ctx context.Context,
	session *model.Session,
	company *model.Company,
) (*model.Company, error) {
	ae := NewAuditEvent("Company.Create", session)
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
	// parse request
	name, err := company.Name.Get()
	if err != nil {
		s.Logger.Debugw("failed to get company name", "error", err)
		return nil, errs.Wrap(err)
	}
	// check if company name is unique, let a TOCTOU error
	// happen as a generic error, this is easier than checking
	// all database types specific unique contraint errors
	_, err = s.CompanyRepository.GetByName(
		ctx,
		name.String(),
	)
	// we expect not to find a company with this name
	if err != nil {
		// something went wrong
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Logger.Errorw("failed to check unique company name", "error", err)
			return nil, errs.Wrap(err)
		}
	}
	// if there is no error, then the company name is already taken
	if err == nil {
		// company name is already taken
		s.Logger.Debugw("company name is already taken", "error", name.String())
		return nil, validate.WrapErrorWithField(errors.New("not unique"), "name")
	}
	// create company
	createdCompanyID, err := s.CompanyRepository.Insert(
		ctx,
		company,
	)
	if err != nil {
		s.Logger.Errorw("failed to create company", "error", err)
		return nil, errs.Wrap(err)
	}
	createdCompany, err := s.CompanyRepository.GetByID(
		ctx,
		createdCompanyID,
	)
	if err != nil {
		s.Logger.Errorw("failed to get created company", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = createdCompanyID.String()
	s.AuditLogAuthorized(ae)

	return createdCompany, nil
}

// GetAll gets all companies with pagination
func (s *Company) GetAll(
	ctx context.Context,
	session *model.Session,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Company], error) {
	result := model.NewEmptyResult[model.Company]()
	ae := NewAuditEvent("Company.GetAll", session)
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
	result, err = s.CompanyRepository.GetAll(
		ctx,
		queryArgs,
	)
	if err != nil {
		s.Logger.Errorw("failed to get companies", "error", err)
		return nil, errs.Wrap(err)
	}
	return result, nil
}

// GetByID gets a company by ID
func (s *Company) GetByID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.Company, error) {
	ae := NewAuditEvent("Company.GetByID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
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
	company, err := s.CompanyRepository.GetByID(
		ctx,
		companyID,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early, this is not an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		s.Logger.Errorw("failed to get company by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return company, nil
}

// Update updates a company by ID
func (s *Company) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	company *model.Company,
) error {
	ae := NewAuditEvent("Company.UpdateByID", session)
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
	current, err := s.CompanyRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		return err
	}
	name, err := company.Name.Get()
	if err != nil {
		s.Logger.Debugw("failed to get company name", "error", err)
		return err
	}
	// check if company name is unique, let a TOCTOU error
	// happen as a generic error, this is easier than checking
	// all database types specific unique contraint errors
	_, err = s.CompanyRepository.GetByName(
		ctx,
		name.String(),
	)
	// we expect not to find a company with this name
	// so any error is an actual error
	if err != nil {
		// something went wrong
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Logger.Debugw("failed to get existing company name", "error", current.Name)
			return err
		}
	}
	// if there is no error, then the company name is already taken
	if err == nil {
		s.Logger.Debugw("company name is already taken", "error", name.String())
		return validate.WrapErrorWithField(errors.New("not unique"), "name")
	}
	// update changed fields
	if v, err := company.Name.Get(); err == nil {
		current.Name.Set(v)
	}
	// validate
	if err := company.Validate(); err != nil {
		s.Logger.Errorw("failed to validate company", "error", err)
		return err
	}

	// update company
	err = s.CompanyRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		s.Logger.Errorw("failed to update company by id", "error", err)
		return err
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID deletes a company by ID
func (s *Company) DeleteByID(
	g *gin.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (int, error) {
	ae := NewAuditEvent("Company.DeleteByID", session)
	ae.Details["companyId"] = companyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return 0, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}
	// deleting a company starts a big chain of deletion where all things related
	// to the company is deleted
	// delete domains owned by the company, this deletes assets owned by the domains
	affectedDomains, err := s.DomainService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.DomainOption{},
	)
	if err != nil {
		s.Logger.Errorw("error",
			"failed get domains that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, domain := range affectedDomains.Rows {
		domainID := domain.ID.MustGet()
		err = s.DomainService.DeleteByID(
			g,
			session,
			&domainID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete domains related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete pages, this also cancels campaings and remove relations that use them
	affectedPages, err := s.PageService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.PageOption{},
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get pages that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, page := range affectedPages.Rows {
		pageID := page.ID.MustGet()
		err = s.PageService.DeleteByID(
			g,
			session,
			&pageID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete domains related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete emails, this also removes attachments
	affectedEmails, err := s.EmailService.GetByCompanyID(
		g,
		session,
		companyID,
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get email that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, email := range affectedEmails.Rows {
		emailID := email.ID.MustGet()
		err = s.EmailService.DeleteByID(
			g,
			session,
			&emailID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete emails related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete api senders
	affectedApiSenders, err := s.APISenderService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.APISenderOption{},
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get api sender that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, apiSender := range affectedApiSenders.Rows {
		emailID := apiSender.ID.MustGet()
		err = s.APISenderService.DeleteByID(
			g,
			session,
			&emailID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete api senders related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete groups
	affectedGroups, err := s.RecipientGroupService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.RecipientGroupOption{},
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get recipient groups that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, group := range affectedGroups {
		groupID := group.ID.MustGet()
		err = s.RecipientGroupService.DeleteByID(
			g,
			session,
			&groupID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete recipient groups related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete recipients
	affectedRecipients, err := s.RecipientService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.RecipientOption{},
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get recipients that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, recipient := range affectedRecipients.Rows {
		recpID := recipient.ID.MustGet()
		err = s.RecipientService.DeleteByID(
			g,
			session,
			&recpID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete recipients related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete webhooks
	affectedWebhooks, err := s.WebhookService.GetByCompanyID(
		g,
		session,
		companyID,
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get webhooks that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, webhook := range affectedWebhooks {
		recpID := webhook.ID.MustGet()
		err = s.WebhookService.DeleteByID(
			g,
			session,
			&recpID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete webhooks related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}

	// delete allow deny
	affectedAllowDenies, err := s.AllowDenyService.GetByCompanyID(
		g,
		session,
		companyID,
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get allow denies that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, allowDeny := range affectedAllowDenies.Rows {
		recpID := allowDeny.ID.MustGet()
		err = s.AllowDenyService.DeleteByID(
			g,
			session,
			&recpID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete allow denies related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete templates
	affectedTemplates, err := s.CampaignTemplate.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.CampaignTemplateOption{},
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get campaign templates that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, campaignTemplate := range affectedTemplates.Rows {
		cid := campaignTemplate.ID.MustGet()
		err = s.CampaignTemplate.DeleteByID(
			g,
			session,
			&cid,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete campaign template related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}
	// delete campaigns
	affectedCampaigns, err := s.CampaignService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.CampaignOption{},
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get campaigns that should be deleted due to company deletion",
			"error", err,
		)
		return 0, errs.Wrap(err)
	}
	for _, campaigns := range affectedCampaigns.Rows {
		cid := campaigns.ID.MustGet()
		err = s.CampaignService.DeleteByID(
			g,
			session,
			&cid,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete campaigns related to company", "error", err)
			return 0, errs.Wrap(err)
		}
	}

	// finally delete the company
	affectedRows, err := s.CompanyRepository.DeleteByID(
		g,
		companyID,
	)
	if err != nil {
		s.Logger.Errorw("failed to delete company by id", "error", err)
		return affectedRows, errs.Wrap(err)
	}
	if affectedRows == 0 {
		return affectedRows, nil
	}
	s.AuditLogAuthorized(ae)
	return affectedRows, nil
}

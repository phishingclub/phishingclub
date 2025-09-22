package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// CampaignTemplate is a campaign template service
type CampaignTemplate struct {
	Common
	CampaignTemplateRepository *repository.CampaignTemplate
	CampaignRepository         *repository.Campaign
	IdentifierRepository       *repository.Identifier
}

// Create creates a new campaign template
func (c *CampaignTemplate) Create(
	ctx context.Context,
	session *model.Session,
	campaignTemplate *model.CampaignTemplate,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("CampaignTemplate.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := campaignTemplate.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	name := campaignTemplate.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		c.CampaignRepository.DB,
		"campaign_templates",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		c.Logger.Errorw("failed to check campaign template uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		c.Logger.Debugw("campagin template name is already taken", "error", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// if no urlIdentifierID is set, get the id of the name 'id'
	if !campaignTemplate.URLIdentifierID.IsSpecified() || campaignTemplate.URLIdentifierID.IsNull() {
		urlIdentifier, err := c.IdentifierRepository.GetByName(ctx, "id")
		if err != nil {
			c.Logger.Errorw("failed to get url identifier by name", "error", err)
			return nil, errs.Wrap(err)
		}
		campaignTemplate.URLIdentifierID = urlIdentifier.ID
	}
	// if no cookieIdentifierID is set, get the id of the name 'session'
	if !campaignTemplate.StateIdentifierID.IsSpecified() || campaignTemplate.StateIdentifierID.IsNull() {
		stateIdentifier, err := c.IdentifierRepository.GetByName(ctx, "p")
		if err != nil {
			c.Logger.Errorw("failed to get state identifier by name", "error", err)
			return nil, errs.Wrap(err)
		}
		campaignTemplate.StateIdentifierID = stateIdentifier.ID
	}
	// if no path set to ''
	if !campaignTemplate.URLPath.IsSpecified() || campaignTemplate.URLPath.IsNull() {
		campaignTemplate.URLPath = nullable.NewNullableWithValue(*vo.NewURLPathMust(""))
	}
	// validate
	if err := campaignTemplate.Validate(); err != nil {
		c.Logger.Errorw("failed to validate campaign template", "error", err)
		return nil, errs.Wrap(err)
	}
	// create
	id, err := c.CampaignTemplateRepository.Insert(ctx, campaignTemplate)
	if err != nil {
		c.Logger.Errorw("failed to create campaign template", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	c.AuditLogAuthorized(ae)

	return id, nil
}

// GetByID gets a campaign template by id
func (c *CampaignTemplate) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.CampaignTemplateOption,
) (*model.CampaignTemplate, error) {
	ae := NewAuditEvent("CampaignTemplate.GetById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get the campaign template
	tmpl, err := c.CampaignTemplateRepository.GetByID(ctx, id, options)
	if err != nil {
		c.Logger.Errorw("wailed to get campaign template by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return tmpl, nil
}

// GetByCompanyID gets a campaign templates by company id
func (c *CampaignTemplate) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.CampaignTemplateOption,
) (*model.Result[model.CampaignTemplate], error) {
	result := model.NewEmptyResult[model.CampaignTemplate]()
	ae := NewAuditEvent("CampaignTemplate.GetByCompanyID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get the campaign template
	result, err = c.CampaignTemplateRepository.GetAllByCompanyID(ctx, companyID, options)
	if err != nil {
		c.Logger.Errorw("failed to get campaign templates by company id", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAll gets all campaign templates
func (c *CampaignTemplate) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	pagination *vo.Pagination,
	options *repository.CampaignTemplateOption,
) (*model.Result[model.CampaignTemplate], error) {
	result := model.NewEmptyResult[model.CampaignTemplate]()
	ae := NewAuditEvent("CampaignTemplate.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get all campaign templates
	result, err = c.CampaignTemplateRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get all campaign templates", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// RemoveAPISenderByAPISenderID removes the api sender id ID from a templates
// this makes the templates unusable until a domain has been added.
func (c *CampaignTemplate) removeAPISenderIDBySenderID(
	ctx context.Context,
	session *model.Session,
	apiSenderID *uuid.UUID,
) error {
	ae := NewAuditEvent("CampaignTemplate.RemoveAPISenderIDBySenderID", session)
	if apiSenderID != nil {
		ae.Details["apiSenderId"] = apiSenderID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// all active campaigns that use this template ID which is becoming unusable due to
	// the domain being removed will be set to close on the next schedule tick.
	templatesAffected, err := c.CampaignTemplateRepository.GetByAPISenderID(
		ctx,
		apiSenderID,
		&repository.CampaignTemplateOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaign templates", "error", err)
		return err
	}
	templateIDs := []*uuid.UUID{}
	for _, t := range templatesAffected {
		id := t.ID.MustGet()
		templateIDs = append(templateIDs, &id)
	}
	campaignsAffected, err := c.CampaignRepository.GetByTemplateIDs(ctx, templateIDs)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaigns by template IDs", "error", err)
		return err
	}

	for _, campaign := range campaignsAffected {
		if !campaign.IsActive() {
			continue
		}
		err := campaign.Close()
		if err != nil {
			c.Logger.Errorw("failed to close to campagin", "error", err)
		}
		campaignID := campaign.ID.MustGet()
		err = c.CampaignRepository.UpdateByID(
			ctx,
			&campaignID,
			campaign,
		)
		if err != nil {
			c.Logger.Errorw("failed to update closed campagin", "error", err)
		}
	}
	// remove the domain id from the templates
	err = c.CampaignTemplateRepository.RemoveAPISenderIDFromAll(ctx, apiSenderID)
	if err != nil {
		c.Logger.Errorw("failed to remove domain ID from all campaign templates", "error", err)
		return err
	}
	return nil
}

// RemoveDomainByDomainID removes the domain ID from a template
// this makes the template unusable until a domain has been added.
func (c *CampaignTemplate) RemoveDomainByDomainID(
	ctx context.Context,
	session *model.Session,
	domainID *uuid.UUID,
) error {
	ae := NewAuditEvent("CampaignTemplate.RemoveDomainByDomainID", session)
	if domainID != nil {
		ae.Details["domainID"] = domainID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// all active campaigns that use this template ID which is becoming unusable due to
	// the domain being removed will be set to close on the next schedule tick.
	templatesAffected, err := c.CampaignTemplateRepository.GetByDomainID(
		ctx,
		domainID,
		&repository.CampaignTemplateOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaign templates", "error", err)
		return err
	}
	templateIDs := []*uuid.UUID{}
	for _, t := range templatesAffected {
		id := t.ID.MustGet()
		templateIDs = append(templateIDs, &id)
	}
	campaignsAffected, err := c.CampaignRepository.GetByTemplateIDs(ctx, templateIDs)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaigns", "error", err)
		return err
	}

	for _, campaign := range campaignsAffected {
		if !campaign.IsActive() {
			continue
		}
		err := campaign.Close()
		if err != nil {
			c.Logger.Errorw("failed to close campaign", "error", err)
		}
		campaignID := campaign.ID.MustGet()
		err = c.CampaignRepository.UpdateByID(
			ctx,
			&campaignID,
			campaign,
		)
		if err != nil {
			c.Logger.Errorw("failed to update closed campaign", "error", err)
		}
	}
	// remove the domain id from the templates
	err = c.CampaignTemplateRepository.RemoveDomainIDFromAll(ctx, domainID)
	if err != nil {
		c.Logger.Errorw("failed to remove domain ID from all campaign templates", "error", err)
		return err
	}
	return nil
}

// RemoveSmtpBySmtpID removes the smtp configuration ID from a template
// this makes the template unusable until a domain has been added.
func (c *CampaignTemplate) RemoveSmtpBySmtpID(
	ctx context.Context,
	session *model.Session,
	smtpID *uuid.UUID,
) error {
	ae := NewAuditEvent("CampaignTemplate.RemoveSmtpBySmtpID", session)
	ae.Details["smtpId"] = smtpID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// all active campaigns that use this template ID which is becoming unusable due to
	// the domain being removed will be set to close on the next schedule tick.
	// templatesAffected, err := c.CampaignTemplateRepository.GetBySmtpID(
	templatesAffected, err := c.CampaignTemplateRepository.GetBySmtpID(
		ctx,
		smtpID,
		&repository.CampaignTemplateOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaign templates", "error", err)
		return err
	}
	templateIDs := []*uuid.UUID{}
	for _, t := range templatesAffected {
		id := t.ID.MustGet()
		templateIDs = append(templateIDs, &id)
	}
	campaignsAffected, err := c.CampaignRepository.GetByTemplateIDs(ctx, templateIDs)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaigns", "error", err)
		return err
	}

	for _, campaign := range campaignsAffected {
		if !campaign.IsActive() {
			continue
		}
		err := campaign.Close()
		if err != nil {
			c.Logger.Errorw("failed to close campaign", "error", err)
		}
		campaignID := campaign.ID.MustGet()
		err = c.CampaignRepository.UpdateByID(
			ctx,
			&campaignID,
			campaign,
		)
		if err != nil {
			c.Logger.Errorw("failed to update closed campaign", "error", err)
		}
	}
	// remove the domain id from the templates
	err = c.CampaignTemplateRepository.RemoveSmtpIDFromAll(ctx, smtpID)
	if err != nil {
		c.Logger.Errorw("failed to remove domain ID from all campaign templates", "error", err)
		return err
	}
	return nil
}

// RemovePageByPageID removes the page ID from a template
// this makes the template unusable until a domain has been added.
func (c *CampaignTemplate) RemovePagesByPageID(
	ctx context.Context,
	session *model.Session,
	pageID *uuid.UUID,
) error {
	ae := NewAuditEvent("CampaignTemplate.RemovePagesByPageID", session)
	ae.Details["pageId"] = pageID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// all active campaigns that use this template ID which is becoming unusable due to
	// the page being removed will be set to close on the next schedule tick.
	templatesAffected, err := c.CampaignTemplateRepository.GetByPageID(
		ctx,
		pageID,
		&repository.CampaignTemplateOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaign templates", "error", err)
		return err
	}
	templateIDs := []*uuid.UUID{}
	for _, t := range templatesAffected {
		id := t.ID.MustGet()
		templateIDs = append(templateIDs, &id)
	}
	campaignsAffected, err := c.CampaignRepository.GetByTemplateIDs(ctx, templateIDs)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaigns", "error", err)
		return err
	}

	for _, campaign := range campaignsAffected {
		if !campaign.IsActive() {
			continue
		}
		err := campaign.Close()
		if err != nil {
			c.Logger.Errorw("failed to close campagin", "error", err)
		}
		campaignID := campaign.ID.MustGet()
		err = c.CampaignRepository.UpdateByID(
			ctx,
			&campaignID,
			campaign,
		)
		if err != nil {
			c.Logger.Errorw("failed to update closed campaign", "error", err)
		}
	}
	// remove the domain id from the templates
	err = c.CampaignTemplateRepository.RemovePageIDFromAll(ctx, pageID)
	if err != nil {
		c.Logger.Errorw("failed to remove page ID from all campaign templates", "error", err)
		return err
	}
	return nil
}

// UpdateByID updates a campaign template by id
func (c *CampaignTemplate) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	campaignTemplate *model.CampaignTemplate,
) error {
	ae := NewAuditEvent("CampaignTemplate.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	/* TODO consider to reintroduce this, but only stop updates towards templates that are used in scheduled or
	   	    not stopped/closed campaigns

	// if this template is used by a campaign, we cannot update it as it has been used for scheduling
	campaignCount, err := c.CampaignRepository.GetCampaignCountByTemplateID(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("failed to get campaign by campaign template id","error ,err)
		return err
	}
	if campaignCount > 0 {
		c.Logger.Error("cannot update campaign template as it is used by a campaign")
		s := "campaign"
		if campaignCount > 1 {
			s = "campaigns"
		}
		return validate.WrapErrorWithField(
			fmt.Errorf("template used by %d %s", campaignCount, s),
			"cant update",
		)
	}
	*/

	// get the campaign template and change values
	incoming, err := c.CampaignTemplateRepository.GetByID(ctx, id, nil)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("campaign template not found", "error", err)
		return err
	}
	if err != nil {
		c.Logger.Errorw("failed to update campaign template by id", "error", err)
		return err
	}
	// update the campaign CampaignTemplate
	if v, err := campaignTemplate.Name.Get(); err == nil {
		// check uniqueness
		var companyID *uuid.UUID
		if cid, err := campaignTemplate.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := campaignTemplate.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			c.CampaignRepository.DB,
			"campaign_templates",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			c.Logger.Errorw("failed to check campaign template uniqueness", "error", err)
			return err
		}
		if !isOK {
			c.Logger.Debugw("campagin template name is already taken", "error", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		incoming.Name.Set(v)
	}
	if campaignTemplate.DomainID.IsSpecified() {
		if v, err := campaignTemplate.DomainID.Get(); err == nil {
			incoming.DomainID.Set(v)
		} else {
			incoming.DomainID.SetNull()
		}
	}
	if campaignTemplate.SMTPConfigurationID.IsSpecified() {
		if v, err := campaignTemplate.SMTPConfigurationID.Get(); err == nil {
			incoming.SMTPConfigurationID.Set(v)
			incoming.APISenderID.SetNull()
		} else {
			incoming.SMTPConfigurationID.SetNull()
		}
	}
	if campaignTemplate.APISenderID.IsSpecified() {
		if v, err := campaignTemplate.APISenderID.Get(); err == nil {
			incoming.APISenderID.Set(v)
			incoming.SMTPConfigurationID.SetNull()
		} else {
			incoming.APISenderID.SetNull()
		}
	}
	if campaignTemplate.EmailID.IsSpecified() {
		if v, err := campaignTemplate.EmailID.Get(); err == nil {
			incoming.EmailID.Set(v)
		} else {
			incoming.EmailID.SetNull()
		}
	}
	if campaignTemplate.BeforeLandingPageID.IsSpecified() {
		if v, err := campaignTemplate.BeforeLandingPageID.Get(); err == nil {
			incoming.BeforeLandingPageID.Set(v)
			incoming.BeforeLandingProxyID.SetNull() // clear proxy if page is set
		} else {
			incoming.BeforeLandingPageID.SetNull()
		}
	}
	if campaignTemplate.BeforeLandingProxyID.IsSpecified() {
		if v, err := campaignTemplate.BeforeLandingProxyID.Get(); err == nil {
			incoming.BeforeLandingProxyID.Set(v)
			incoming.BeforeLandingPageID.SetNull() // clear page if proxy is set
		} else {
			incoming.BeforeLandingProxyID.SetNull()
		}
	}
	if campaignTemplate.LandingPageID.IsSpecified() {
		if v, err := campaignTemplate.LandingPageID.Get(); err == nil {
			incoming.LandingPageID.Set(v)
			incoming.LandingProxyID.SetNull() // clear proxy if page is set
		} else {
			incoming.LandingPageID.SetNull()
		}
	}
	if campaignTemplate.LandingProxyID.IsSpecified() {
		if v, err := campaignTemplate.LandingProxyID.Get(); err == nil {
			incoming.LandingProxyID.Set(v)
			incoming.LandingPageID.SetNull() // clear page if proxy is set
		} else {
			incoming.LandingProxyID.SetNull()
		}
	}
	if campaignTemplate.AfterLandingPageID.IsSpecified() {
		if v, err := campaignTemplate.AfterLandingPageID.Get(); err == nil {
			incoming.AfterLandingPageID.Set(v)
			incoming.AfterLandingProxyID.SetNull() // clear proxy if page is set
		} else {
			incoming.AfterLandingPageID.SetNull()
		}
	}
	if campaignTemplate.AfterLandingProxyID.IsSpecified() {
		if v, err := campaignTemplate.AfterLandingProxyID.Get(); err == nil {
			incoming.AfterLandingProxyID.Set(v)
			incoming.AfterLandingPageID.SetNull() // clear page if proxy is set
		} else {
			incoming.AfterLandingProxyID.SetNull()
		}
	}
	if campaignTemplate.AfterLandingPageRedirectURL.IsSpecified() {
		if v, err := campaignTemplate.AfterLandingPageRedirectURL.Get(); err == nil {
			incoming.AfterLandingPageRedirectURL.Set(v)
		} else {
			incoming.AfterLandingPageRedirectURL.SetNull()
		}
	}
	if v, err := campaignTemplate.URLIdentifierID.Get(); err == nil {
		incoming.URLIdentifierID.Set(v)
	}
	if v, err := campaignTemplate.StateIdentifierID.Get(); err == nil {
		incoming.StateIdentifierID.Set(v)
	}
	if v, err := campaignTemplate.URLPath.Get(); err == nil {
		incoming.URLPath.Set(v)
	}
	// validate
	if err := incoming.Validate(); err != nil {
		c.Logger.Errorw("failed to validate campaign template", "error", err)
		return err
	}
	err = c.CampaignTemplateRepository.UpdateByID(
		ctx,
		id,
		incoming,
	)
	if err != nil {
		c.Logger.Errorw("failed to update campaign template by id", "error", err)
		return err
	}
	c.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes a campaign template by id
func (c *CampaignTemplate) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("CampaignTemplate.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// all active campaigns that use this template ID which is becoming unusable due to
	// the domain being removed will be set to close on the next schedule tick.
	campaignsAffected, err := c.CampaignRepository.GetByTemplateIDs(
		ctx,
		[]*uuid.UUID{id},
	)
	if err != nil {
		c.Logger.Errorw(
			"failed to get campaign affected by template deletion",
			"error", err,
		)
		return err
	}
	for _, campaign := range campaignsAffected {
		if !campaign.IsActive() {
			continue
		}
		err := campaign.Close()
		if err != nil {
			c.Logger.Errorw("failed to close campaign", "error", err)
		}
		campaignID := campaign.ID.MustGet()
		err = c.CampaignRepository.UpdateByID(
			ctx,
			&campaignID,
			campaign,
		)
		if err != nil {
			c.Logger.Errorw("failed to update closed campaign", "error", err)
		}
	}
	// remove the campaign template id from campaigns
	err = c.CampaignRepository.RemoveCampaignTemplateIDFromCampaigns(
		ctx,
		id,
	)
	// delete the campaign template
	err = c.CampaignTemplateRepository.DeleteByID(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to delete campaign template by id", "error", err)
		return err
	}
	c.AuditLogAuthorized(ae)
	return nil
}

// RemoveProxiesByProxyID removes the Proxy ID from templates
func (c *CampaignTemplate) RemoveProxiesByProxyID(
	ctx context.Context,
	session *model.Session,
	proxyID *uuid.UUID,
) error {
	ae := NewAuditEvent("CampaignTemplate.RemoveProxiesByProxyID", session)
	ae.Details["proxyId"] = proxyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// get all templates that use this proxy
	templatesAffected, err := c.CampaignTemplateRepository.GetByProxyID(
		ctx,
		proxyID,
		&repository.CampaignTemplateOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get affected campaign templates", "error", err)
		return err
	}

	// get all campaigns using these templates and close active ones
	templateIDs := []*uuid.UUID{}
	for _, t := range templatesAffected {
		id := t.ID.MustGet()
		templateIDs = append(templateIDs, &id)
	}

	if len(templateIDs) > 0 {
		campaignsAffected, err := c.CampaignRepository.GetByTemplateIDs(ctx, templateIDs)
		if err != nil {
			c.Logger.Errorw("failed to get affected campaigns by template IDs", "error", err)
			return err
		}

		for _, campaign := range campaignsAffected {
			if !campaign.IsActive() {
				continue
			}
			err := campaign.Close()
			if err != nil {
				c.Logger.Errorw("failed to close campaign", "error", err)
			}
			campaignID := campaign.ID.MustGet()
			err = c.CampaignRepository.UpdateByID(
				ctx,
				&campaignID,
				campaign,
			)
			if err != nil {
				c.Logger.Errorw("failed to update closed campaign", "error", err)
			}
		}
	}

	// remove the Proxy id from the templates
	err = c.CampaignTemplateRepository.RemoveProxyIDFromAll(ctx, proxyID)
	if err != nil {
		c.Logger.Errorw("failed to remove Proxy ID from all campaign templates", "error", err)
		return err
	}
	return nil
}

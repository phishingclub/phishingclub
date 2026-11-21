package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// Page is a Page service
type Page struct {
	Common
	PageRepository          *repository.Page
	CampaignRepository      *repository.Campaign
	CampaignTemplateService *CampaignTemplate
	TemplateService         *Template
	DomainRepository        *repository.Domain
	ProgramRepository       *repository.Program
	// CompanyRepository refuses new data in a company that is being deleted
	CompanyRepository *repository.Company
}

// Create creates a new page
func (p *Page) Create(
	ctx context.Context,
	session *model.Session,
	page *model.Page,
) (_ *uuid.UUID, err error) {
	ae := NewAuditEvent("Page.Create", session)
	defer p.auditDone(ae, &err)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	var companyID *uuid.UUID
	if cid, err := page.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	unlockCompany, err := refuseDeletingCompany(ctx, p.CompanyRepository, companyID)
	if err != nil {
		return nil, err
	}
	defer unlockCompany()

	// validate data
	if err := page.Validate(); err != nil {
		p.Logger.Errorw("failed to validate page", "error", err)
		return nil, errs.Wrap(err)
	}
	// validate template content
	if content, err := page.Content.Get(); err == nil {
		if err := p.TemplateService.ValidatePageTemplate(content.String()); err != nil {
			p.Logger.Errorw("failed to validate page template", "error", err)
			return nil, validate.WrapErrorWithField(errors.New("invalid template: "+err.Error()), "content")
		}
	}
	// check uniqueness
	name := page.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		p.PageRepository.DB,
		"pages",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		p.Logger.Errorw("failed to check page uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		p.Logger.Debugw("page name is already taken", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// create page
	id, err := p.PageRepository.Insert(
		ctx,
		page,
	)
	if err != nil {
		p.Logger.Errorw("failed to create page", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	p.AuditLogAuthorized(ae)

	return id, nil
}

// GetAll gets pages
func (p *Page) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.PageOption,
) (*model.Result[model.Page], error) {
	result := model.NewEmptyResult[model.Page]()
	ae := NewAuditEvent("Page.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = p.PageRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		p.Logger.Errorw("failed to get pages", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// GetByID gets a page by ID
func (p *Page) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.PageOption,
) (*model.Page, error) {
	ae := NewAuditEvent("Page.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get page
	page, err := p.PageRepository.GetByID(
		ctx,
		id,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not a an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		p.Logger.Errorw("failed to get page by ID", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log on read

	return page, nil
}

// GetByCompanyID gets a page by company ID
func (p *Page) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.PageOption,
) (*model.Result[model.Page], error) {
	result := model.NewEmptyResult[model.Page]()
	ae := NewAuditEvent("Page.GetByCompanyID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get pages
	result, err = p.PageRepository.GetAllByCompanyID(
		ctx,
		companyID,
		&repository.PageOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not a an error
		return result, errs.Wrap(err)
	}
	if err != nil {
		p.Logger.Errorw("failed to get page by company ID", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit log on read

	return result, nil
}

// UpdateByID updates a page by ID
func (p *Page) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	page *model.Page,
) (err error) {
	ae := NewAuditEvent("Page.UpdateByID", session)
	defer p.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get current
	current, err := p.PageRepository.GetByID(
		ctx,
		id,
		&repository.PageOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		p.Logger.Debugw("failed to update page by ID", "error", err)
		return err
	}
	if err != nil {
		p.Logger.Errorw("failed to update page by ID", "error", err)
		return err
	}
	// update page - if a field is present and not null, update it
	if v, err := page.Name.Get(); err == nil {
		// check uniqueness
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := page.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			p.PageRepository.DB,
			"pages",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			p.Logger.Errorw("failed to check page uniqueness", "error", err)
			return err
		}
		if !isOK {
			p.Logger.Debugw("page name is already taken", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		current.Name.Set(v)
	}
	if v, err := page.Content.Get(); err == nil {
		current.Content.Set(v)
	}

	// validate template content
	if content, err := current.Content.Get(); err == nil {
		if err := p.TemplateService.ValidatePageTemplate(content.String()); err != nil {
			p.Logger.Errorw("failed to validate page template", "error", err)
			return validate.WrapErrorWithField(errors.New("invalid template: "+err.Error()), "content")
		}
	}
	// update page
	err = p.PageRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		p.Logger.Errorw("failed to update page by ID", "error", err)
		return err
	}
	ae.Details["id"] = id.String()
	p.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes a page by ID
func (p *Page) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("Page.DeleteByID", session)
	addDeleteOrigin(ctx, ae)
	defer p.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// a campaign or program that is not closed needs its deny and evasion
	// page, so the page must be removed from it first
	if err := p.checkPageNotUsedByOpen(ctx, id); err != nil {
		return err
	}
	// clear the page from the closed campaigns and programs using it as deny or
	// evasion page. a deny page goes together with the lists and the evasion
	// page that need it, so no campaign or program is left with filtering but
	// no deny page
	if err := p.CampaignRepository.RemovePageFromFilters(ctx, id); err != nil {
		p.Logger.Errorw("failed to remove page from campaigns", "error", err)
		return err
	}
	if p.ProgramRepository != nil {
		if err := p.ProgramRepository.RemovePageFromProgramFilters(ctx, id); err != nil {
			p.Logger.Errorw("failed to remove page from programs", "error", err)
			return err
		}
	}
	// delete the relation from campaign templates
	err = p.CampaignTemplateService.RemovePagesByPageID(
		ctx,
		session,
		id,
	)
	if err != nil {
		p.Logger.Errorw("failed to remove page ID relations from campaign templates", "error", err)
		return err
	}
	// delete page
	err = p.PageRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		p.Logger.Errorw("failed to delete page by ID", "error", err)
		return err
	}
	p.AuditLogAuthorized(ae)

	return nil
}

// pageInUseNamesMax is how many campaigns and programs a refused page delete
// names
const pageInUseNamesMax = 5

// OpenFilterPageUsers returns up to pageInUseNamesMax campaigns and programs
// that are not closed and use the page as deny or evasion page, each named as
// "campaign X" or "program X", and how many there are in total. A page with
// any of them can not be deleted.
func (p *Page) OpenFilterPageUsers(ctx context.Context, id *uuid.UUID) ([]string, int64, error) {
	campaignNames, campaignCount, err := p.CampaignRepository.GetOpenNamesByFilterPage(ctx, id, pageInUseNamesMax)
	if err != nil {
		p.Logger.Errorw("failed to get campaigns using the page", "error", err)
		return nil, 0, errs.Wrap(err)
	}
	var programNames []string
	var programCount int64
	if p.ProgramRepository != nil {
		programNames, programCount, err = p.ProgramRepository.GetOpenNamesByFilterPage(ctx, id, pageInUseNamesMax)
		if err != nil {
			p.Logger.Errorw("failed to get programs using the page", "error", err)
			return nil, 0, errs.Wrap(err)
		}
	}
	names := make([]string, 0, pageInUseNamesMax)
	for _, n := range campaignNames {
		if len(names) < pageInUseNamesMax {
			names = append(names, "campaign "+n)
		}
	}
	for _, n := range programNames {
		if len(names) < pageInUseNamesMax {
			names = append(names, "program "+n)
		}
	}
	return names, campaignCount + programCount, nil
}

// checkPageNotUsedByOpen returns a validation error naming the campaigns and
// programs that are not closed and use the page as deny or evasion page
func (p *Page) checkPageNotUsedByOpen(ctx context.Context, id *uuid.UUID) error {
	names, total, err := p.OpenFilterPageUsers(ctx, id)
	if err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	list := strings.Join(names, ", ")
	if more := total - int64(len(names)); more > 0 {
		list = fmt.Sprintf("%s and %d more", list, more)
	}
	return errs.NewValidationError(errors.Errorf(
		"page is used as deny or evasion page by: %s. Remove it from them first",
		list,
	))
}

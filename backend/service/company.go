package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-errors/errors"

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
	ProgramService           *Program
	AllowDenyService         *AllowDeny
	WebhookService           *Webhook
	ScriptService            *Script
	AssetService             *Asset
	AttachmentService        *Attachment
	CompanyRepository        *repository.Company
	ReportInboxRepository    *repository.ReportInbox
	// the report delivery config and logs of a company are deleted with it
	CompanyReportConfigRepository *repository.CompanyReportConfig
	// CampaignDefaultsRepository removes the campaign defaults of a deleted company
	CampaignDefaultsRepository *repository.CampaignDefaults
	ReportSendLogRepository    *repository.ReportSendLog
	// the SCIM config of a company is deleted first when it is deleted
	CompanyScimConfigRepository *repository.CompanyScimConfig
	// CancelReportInboxPoll stops a running poll of a reporting inbox that is
	// deleted with its company
	CancelReportInboxPoll func(uuid.UUID)
	// deletes finishes the deletes of the companies marked as being deleted
	deletes deletePass
}

// Create creates a company
func (s *Company) Create(
	ctx context.Context,
	session *model.Session,
	company *model.Company,
) (_ *model.Company, err error) {
	ae := NewAuditEvent("Company.Create", session)
	defer s.auditDone(ae, &err)
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
	for _, c := range result.Rows {
		s.applyDeleteFailure(c)
	}
	return result, nil
}

// applyDeleteFailure fills why the last delete of a company stopped
func (s *Company) applyDeleteFailure(c *model.Company) {
	if id, err := c.ID.Get(); err == nil {
		c.DeleteFailure = s.deletes.failure(id)
	}
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
	s.applyDeleteFailure(company)
	// no audit on read
	return company, nil
}

// Update updates a company by ID
func (s *Company) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	company *model.Company,
) (err error) {
	ae := NewAuditEvent("Company.UpdateByID", session)
	defer s.auditDone(ae, &err)
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
	if current.DeletingAt != nil {
		return validate.WrapErrorWithField(errCompanyDeleting, "company")
	}
	name, err := company.Name.Get()
	if err != nil {
		s.Logger.Debugw("failed to get company name", "error", err)
		return err
	}
	// check if company name is unique, excluding the current company
	// only check if the name has actually changed
	currentName := current.Name.MustGet().String()
	if name.String() != currentName {
		// check if company name is unique, let a TOCTOU error
		// happen as a generic error, this is easier than checking
		// all database types specific unique contraint errors
		existingCompany, err := s.CompanyRepository.GetByName(
			ctx,
			name.String(),
		)
		// we expect not to find a company with this name
		if err != nil {
			// something went wrong
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				s.Logger.Debugw("failed to get existing company name", "error", err)
				return err
			}
		}
		// if there is no error, then the company name is already taken
		// but we need to make sure it's not the same company we're updating
		if err == nil && existingCompany.ID.MustGet() != *id {
			s.Logger.Debugw("company name is already taken", "error", name.String())
			return validate.WrapErrorWithField(errors.New("not unique"), "name")
		}
	}
	// update changed fields
	if v, err := company.Name.Get(); err == nil {
		current.Name.Set(v)
	}
	if v, err := company.Comment.Get(); err == nil {
		current.Comment.Set(v)
	}
	if company.Color.IsSpecified() {
		current.Color = company.Color
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

// DeleteByID marks a company as being deleted and deletes it in the
// background with everything that belongs to it, see deleteCompanyNow. A
// company already marked is tried again at once. The background pass audits
// how the delete ends.
func (s *Company) DeleteByID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (_ int, err error) {
	ae := NewAuditEvent("Company.DeleteByID", session)
	defer s.auditDone(ae, &err)
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
	if _, err := s.CompanyRepository.GetByID(ctx, companyID); err != nil {
		return 0, errs.Wrap(err)
	}
	system, err := model.NewSystemSession()
	if err != nil {
		return 0, errs.Wrap(err)
	}
	// what the delete does not remove is refused before the mark, so the
	// background delete can always finish. The checks and the mark run under
	// the write lock, so no save adds a row in between.
	unlock, err := companyDeleteLock.mark(ctx, *companyID)
	if err != nil {
		return 0, err
	}
	// deferred, so a panic never leaves the lock held
	defer unlock()
	if err := s.refuseDeleteBlockers(ctx, companyID); err != nil {
		return 0, err
	}
	if err := s.CompanyRepository.MarkDeleting(ctx, companyID, time.Now()); err != nil {
		return 0, errs.Wrap(err)
	}
	unlock()
	s.deletes.clearFailure(*companyID)
	s.deletes.request(*companyID, requesterID(session))
	ae.Details["reason"] = "delete started"
	s.AuditLogAuthorized(ae)
	s.startDeletes(system, companyID)
	return 1, nil
}

// refuseDeleteBlockers refuses the delete of a company that holds rows its
// delete does not remove, and names them
func (s *Company) refuseDeleteBlockers(ctx context.Context, companyID *uuid.UUID) error {
	blockers, err := s.CompanyRepository.GetDeleteBlockers(ctx, companyID)
	if err != nil {
		return errs.Wrap(err)
	}
	// the domain that serves SCIM provisioning can not be deleted, see
	// Domain.deleteDomain. A read error fails closed.
	if s.DomainService != nil && s.DomainService.OptionRepository != nil {
		opt, err := s.DomainService.OptionRepository.GetByKey(ctx, data.OptionKeyScimDomain)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(err)
		}
		if err == nil && opt.Value.String() != "" {
			has, err := s.CompanyRepository.HasDomainNamed(ctx, companyID, opt.Value.String())
			if err != nil {
				return errs.Wrap(err)
			}
			if has {
				blockers = append(blockers, "the SCIM provisioning domain")
			}
		}
	}
	if len(blockers) > 0 {
		return errs.NewValidationError(fmt.Errorf(
			"the company still has %s. Move or delete them first", strings.Join(blockers, ", "),
		))
	}
	return nil
}

// FinishDeletesInBackground starts the pass that finishes the deletes of the
// companies marked as being deleted, for example one stopped by a restart.
// The task runner calls it on every tick.
func (s *Company) FinishDeletesInBackground(ctx context.Context, session *model.Session) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(NewAuditEvent("Company.FinishDeletes", session))
		return errs.ErrAuthorizationFailed
	}
	s.startDeletes(session, nil)
	return nil
}

// startDeletes starts the background delete pass, see deletePass
func (s *Company) startDeletes(session *model.Session, first *uuid.UUID) {
	s.deletes.start(s.Logger, first,
		func(ctx context.Context, skip []*uuid.UUID) ([]*uuid.UUID, error) {
			return s.CompanyRepository.GetIDsBeingDeleted(ctx, deletePassScanLimit, skip)
		},
		func(ctx context.Context, id *uuid.UUID) error {
			return s.finishDelete(ctx, session, id)
		},
	)
}

// finishDelete deletes one company marked as being deleted and audits it as
// done by the system for the user who asked. The mark never goes: when a row
// its delete does not remove blocks it, the reason is kept to show on the
// company and the delete is tried again, as after any other failure. A
// failure is audited at most once per deletePassFailureAuditEvery.
func (s *Company) finishDelete(ctx context.Context, session *model.Session, companyID *uuid.UUID) error {
	ae := NewAuditEvent("Company.DeleteByID", session)
	ae.Details["companyId"] = companyID.String()
	if by := s.deletes.requester(*companyID); by != "" {
		ae.Details["requestedBy"] = by
	}
	// the blockers are checked again before anything is removed. The mark
	// never goes, the delete waits until they are removed
	if _, err := s.CompanyRepository.GetByID(ctx, companyID); errors.Is(err, gorm.ErrRecordNotFound) {
		s.deletes.clearFailure(*companyID)
		return nil
	}
	if err := s.refuseDeleteBlockers(ctx, companyID); err != nil {
		var v errs.ValidationError
		if errors.As(err, &v) {
			// the mark stays, the reason is shown on the company and the
			// delete goes on once the row is removed
			s.deletes.stopped(*companyID, v.Err.Error())
			if s.deletes.auditFailure(*companyID) {
				ae.Details["reason"] = "delete waiting"
				s.AuditLogFailed(ae, err)
			}
		}
		return err
	}
	err := s.deleteCompanyNow(withDeleteOrigin(ctx, companyID, ae.Details["requestedBy"]), session, companyID)
	if err != nil {
		// the reason stays shown until the delete goes on
		s.deletes.stopped(*companyID, deleteWaitReason(err))
		if s.deletes.auditFailure(*companyID) {
			ae.Details["reason"] = "delete failed"
			s.AuditLogFailed(ae, err)
		}
		return err
	}
	s.deletes.clearFailure(*companyID)
	ae.Details["reason"] = "delete finished"
	s.AuditLogAuthorized(ae)
	return nil
}

// deleteCompanyNow deletes a company and everything that belongs to it. Every
// step can run again, so a delete that stops half way is finished by the next
// try. The caller checked the rows its delete does not remove.
func (s *Company) deleteCompanyNow(
	g context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) error {
	// the SCIM config goes first, so the identity provider can no longer
	// push recipients into the company while it is deleted
	if s.CompanyScimConfigRepository != nil {
		if err := s.CompanyScimConfigRepository.DeleteByCompanyID(g, companyID); err != nil {
			s.Logger.Errorw("failed to delete the SCIM config of the company", "error", err)
			return errs.Wrap(err)
		}
	}
	// delete the reporting inbox first, with its stored credentials, so nothing
	// keeps reading the company's mailbox or records reports while the
	// campaigns below are deleted
	if s.ReportInboxRepository != nil {
		inbox, inboxErr := s.ReportInboxRepository.GetByCompanyID(g, companyID)
		switch {
		case inboxErr == nil:
			inboxID := inbox.ID.MustGet()
			if err := s.ReportInboxRepository.DeleteByID(g, &inboxID); err != nil {
				s.Logger.Errorw("failed to delete reporting inbox related to company", "error", err)
				return errs.Wrap(err)
			}
			// stopped after the delete, so a poll planned meanwhile is either
			// stopped here or finds the inbox gone before it reads anything
			if s.CancelReportInboxPoll != nil {
				s.CancelReportInboxPoll(inboxID)
			}
		case !errors.Is(inboxErr, gorm.ErrRecordNotFound):
			s.Logger.Errorw("failed to get reporting inbox related to company", "error", inboxErr)
			return errs.Wrap(inboxErr)
		}
	}

	// the report delivery config points at a recipient group of the company,
	// so it goes before the groups
	if s.CompanyReportConfigRepository != nil && companyID != nil {
		if err := s.CompanyReportConfigRepository.DeleteByCompanyID(g, companyID); err != nil {
			s.Logger.Errorw("failed to delete report config related to company", "error", err)
			return errs.Wrap(err)
		}
	}
	if s.CampaignDefaultsRepository != nil && companyID != nil {
		if err := s.CampaignDefaultsRepository.DeleteByCompanyID(g, companyID); err != nil {
			s.Logger.Errorw("failed to delete campaign defaults related to company", "error", err)
			return errs.Wrap(err)
		}
	}

	// deleting a company starts a big chain of deletion where all things related
	// to the company is deleted
	// delete programs first, they own hidden campaigns and reference templates and
	// groups that are deleted below
	if s.ProgramService != nil {
		programIDs, err := s.ProgramService.ProgramRepository.GetIDsByCompanyID(g, companyID)
		if err != nil {
			s.Logger.Errorw("failed to get programs that should be deleted due to company deletion",
				"error", err,
			)
			return errs.Wrap(err)
		}
		for _, programID := range programIDs {
			if err := s.ProgramService.DeleteByIDNow(g, session, programID); err != nil {
				s.Logger.Errorw("failed to delete program due to company deletion",
					"programID", programID.String(),
					"error", err,
				)
				return errs.Wrap(err)
			}
		}
	}
	// delete campaigns next, before the pages, templates, groups and recipients
	// they use, so no open campaign holds one of them while it is deleted
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
		}
	}
	// the manual campaign stats of the company have no campaign, they go
	// with the company
	if err := s.CampaignService.CampaignRepository.DeleteManualStatsByCompanyID(g, companyID); err != nil {
		s.Logger.Errorw("failed to delete manual campaign stats related to company", "error", err)
		return errs.Wrap(err)
	}
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
		}
	}
	// delete the company shared assets, these have no domain so the domain
	// deletion above does not cover them
	err = s.AssetService.DeleteAllByCompanyID(
		g,
		session,
		companyID,
	)
	if err != nil {
		s.Logger.Errorw("failed to delete shared assets related to company", "error", err)
		return errs.Wrap(err)
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
		}
	}
	// the attachments of the company are not tied to an email, they go with
	// their files
	if s.AttachmentService != nil {
		if err := s.AttachmentService.DeleteAllByCompanyID(g, session, companyID); err != nil {
			s.Logger.Errorw("failed to delete attachments related to company", "error", err)
			return errs.Wrap(err)
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
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
		return errs.Wrap(err)
	}
	for _, group := range affectedGroups {
		groupID := group.ID.MustGet()
		err = s.RecipientGroupService.deleteGroupNow(
			g,
			&groupID,
		)
		if err == nil {
			s.RecipientGroupService.forgetDelete(groupID)
		}
		if err != nil {
			s.Logger.Errorw("failed to delete recipient groups related to company", "error", err)
			return errs.Wrap(err)
		}
	}
	// delete recipients, a page at a time so a large company is never read
	// at once. Each page is deleted, so the next read starts at the top again
	// each person is removed with a few short writes. The pacer pauses
	// between people now and then, outside the program lifecycle lock, so
	// tracking and other writers are not kept waiting through a large company.
	var pacer repository.WritePacer
	// a recipient read twice was not removed, the delete stops instead of
	// reading the same page forever and is tried again later
	removed := map[uuid.UUID]bool{}
	for {
		page, err := s.RecipientService.GetByCompanyID(
			g,
			session,
			companyID,
			&repository.RecipientOption{QueryArgs: &vo.QueryArgs{Limit: companyDeleteRecipientPage}},
		)
		if err != nil {
			s.Logger.Errorw(
				"failed get recipients that should be deleted due to company deletion",
				"error", err,
			)
			return errs.Wrap(err)
		}
		if len(page.Rows) == 0 {
			break
		}
		for _, recipient := range page.Rows {
			recpID := recipient.ID.MustGet()
			if removed[recpID] {
				return errs.Wrap(errors.New("a recipient of the company was not removed"))
			}
			removed[recpID] = true
			if err := s.RecipientService.DeleteByID(g, session, &recpID); err != nil {
				s.Logger.Errorw("failed to delete recipients related to company", "error", err)
				return errs.Wrap(err)
			}
			if err := pacer.Pause(g); err != nil {
				return errs.Wrap(err)
			}
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
		}
	}

	// delete scripts
	affectedScripts, err := s.ScriptService.GetByCompanyID(
		g,
		session,
		companyID,
	)
	if err != nil {
		s.Logger.Errorw(
			"failed get scripts that should be deleted due to company deletion",
			"error", err,
		)
		return errs.Wrap(err)
	}
	for _, script := range affectedScripts {
		scriptID := script.ID.MustGet()
		err = s.ScriptService.DeleteByID(
			g,
			session,
			&scriptID,
		)
		if err != nil {
			s.Logger.Errorw("failed to delete scripts related to company", "error", err)
			return errs.Wrap(err)
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
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
		return errs.Wrap(err)
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
			return errs.Wrap(err)
		}
	}
	// the report send logs point at the company, they go right before it so a
	// report sent while the rest was deleted is removed too
	if s.ReportSendLogRepository != nil && companyID != nil {
		if err := s.ReportSendLogRepository.DeleteByCompanyID(g, companyID); err != nil {
			s.Logger.Errorw("failed to delete report send logs related to company", "error", err)
			return errs.Wrap(err)
		}
	}

	// finally delete the company
	if _, err := s.CompanyRepository.DeleteByID(g, companyID); err != nil {
		s.Logger.Errorw("failed to delete company by id", "error", err)
		return errs.Wrap(err)
	}
	return nil
}

// companyDeleteRecipientPage is how many recipients a company delete reads
// at a time
const companyDeleteRecipientPage = 500

// errExportNotBuilt is the failure of an export whose file was not built
var errExportNotBuilt = errors.New("export file was not built")

// AuthorizeExport checks the session may export company data and audits the
// export. A nil companyID is the export of shared data that belongs to no
// company, with the global programs. The event counts the programs the export
// lists and how many of them get an events file, an anonymous program is
// listed without one. A refusal is written at once. When it returns no error
// the caller builds the file and then calls done once, with true when the
// file was built and sent, which writes the event as a success, and false
// otherwise, which writes it as failed.
func (c *Company) AuthorizeExport(ctx context.Context, session *model.Session, companyID *uuid.UUID) (done func(built bool), err error) {
	ae := NewAuditEvent("Company.Export", session)
	defer c.auditDone(ae, &err)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	} else {
		ae.Details["shared"] = true
	}
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	programs, err := c.CampaignService.CampaignRepository.GetProgramsForExport(ctx, companyID)
	if err != nil {
		c.Logger.Errorw("failed to count programs for export", "error", err)
		return nil, errs.Wrap(err)
	}
	withEvents := 0
	for _, p := range programs {
		if !p.IsAnonymous {
			withEvents++
		}
	}
	ae.Details["programs"] = len(programs)
	ae.Details["programsWithEvents"] = withEvents
	var once sync.Once
	return func(built bool) {
		once.Do(func() {
			if built {
				c.AuditLogAuthorized(ae)
				return
			}
			c.AuditLogFailed(ae, errExportNotBuilt)
		})
	}, nil
}

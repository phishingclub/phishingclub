package service

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// CampaignDefaults is the service for the new campaign form defaults. A nil
// companyID is the global default that every company inherits from.
type CampaignDefaults struct {
	Common
	CampaignDefaultsRepository *repository.CampaignDefaults
	// CompanyRepository checks the company exists and is not being deleted
	CompanyRepository *repository.Company
}

// campaignDefaultsScope names the scope in the audit log
func campaignDefaultsScope(companyID *uuid.UUID) string {
	if companyID == nil {
		return "global"
	}
	return companyID.String()
}

// authorize checks the session may manage campaign defaults
func (s *CampaignDefaults) authorize(session *model.Session, ae *AuditEvent) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	return nil
}

// getOrNil returns the row of the scope, nil when none is stored
func (s *CampaignDefaults) getOrNil(ctx context.Context, companyID *uuid.UUID) (*model.CampaignDefaults, error) {
	row, err := s.CampaignDefaultsRepository.GetByCompanyID(ctx, companyID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		s.Logger.Errorw("failed to get campaign defaults", "error", err)
		return nil, errs.Wrap(err)
	}
	return row, nil
}

// GetByCompanyID returns the stored defaults of a company, or of the global
// default when companyID is nil. It returns nil when none are stored.
func (s *CampaignDefaults) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.CampaignDefaults, error) {
	ae := NewAuditEvent("CampaignDefaults.GetByCompanyID", session)
	ae.Details["scope"] = campaignDefaultsScope(companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	// no audit on read
	return s.getOrNil(ctx, companyID)
}

// GetResolved returns the defaults the new campaign form uses for a company,
// the company value of each field when set, else the global value. A nil
// companyID returns the global values only.
func (s *CampaignDefaults) GetResolved(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.ResolvedCampaignDefaults, error) {
	ae := NewAuditEvent("CampaignDefaults.GetResolved", session)
	ae.Details["scope"] = campaignDefaultsScope(companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	global, err := s.getOrNil(ctx, nil)
	if err != nil {
		return nil, err
	}
	var company *model.CampaignDefaults
	if companyID != nil {
		company, err = s.getOrNil(ctx, companyID)
		if err != nil {
			return nil, err
		}
	}
	// no audit on read
	return model.ResolveCampaignDefaults(company, global), nil
}

// Upsert stores the defaults of a company, or of the global default when
// companyID is nil
func (s *CampaignDefaults) Upsert(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	incoming *model.CampaignDefaults,
) (_ *model.CampaignDefaults, err error) {
	ae := NewAuditEvent("CampaignDefaults.Upsert", session)
	defer s.auditDone(ae, &err)
	ae.Details["scope"] = campaignDefaultsScope(companyID)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	if err := incoming.Validate(); err != nil {
		return nil, err
	}
	unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, companyID)
	if err != nil {
		return nil, err
	}
	defer unlockCompany()
	// checked under the delete lock, so a company deleted meanwhile is not missed
	if companyID != nil && s.CompanyRepository != nil {
		if _, err := s.CompanyRepository.GetByID(ctx, companyID); err != nil {
			return nil, errs.Wrap(err)
		}
	}
	stored, err := s.CampaignDefaultsRepository.Upsert(ctx, companyID, incoming)
	if err != nil {
		s.Logger.Errorw("failed to save campaign defaults", "error", err)
		return nil, errs.Wrap(err)
	}
	if stored.ID != nil {
		ae.Details["id"] = stored.ID.String()
	}
	s.AuditLogAuthorized(ae)
	return stored, nil
}

// DeleteByCompanyID removes the defaults of a company, or of the global
// default when companyID is nil
func (s *CampaignDefaults) DeleteByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("CampaignDefaults.DeleteByCompanyID", session)
	defer s.auditDone(ae, &err)
	ae.Details["scope"] = campaignDefaultsScope(companyID)
	if err := s.authorize(session, ae); err != nil {
		return err
	}
	if err := s.CampaignDefaultsRepository.DeleteByCompanyID(ctx, companyID); err != nil {
		s.Logger.Errorw("failed to delete campaign defaults", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

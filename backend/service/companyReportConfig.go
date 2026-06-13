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
	"gorm.io/gorm"
)

// CompanyReportConfig is the service for report delivery configuration. a nil
// companyID refers to the global default config used as a fallback.
type CompanyReportConfig struct {
	Common
	CompanyReportConfigRepository *repository.CompanyReportConfig
	ReportSendLogRepository       *repository.ReportSendLog
}

// reportConfigScope returns a human label for the config scope for audit logging
func reportConfigScope(companyID *uuid.UUID) string {
	if companyID == nil {
		return "global"
	}
	return companyID.String()
}

// GetByCompanyID returns the report config for the given company
func (s *CompanyReportConfig) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.CompanyReportConfig, error) {
	ae := NewAuditEvent("CompanyReportConfig.GetByCompanyID", session)
	ae.Details["scope"] = reportConfigScope(companyID)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get
	config, err := s.CompanyReportConfigRepository.GetByCompanyID(ctx, companyID)
	if err != nil {
		s.Logger.Errorw("failed to get report config by company id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return config, nil
}

// Upsert creates or updates the report delivery configuration for the given company
func (s *CompanyReportConfig) Upsert(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	incoming *model.CompanyReportConfig,
) (*model.CompanyReportConfig, error) {
	ae := NewAuditEvent("CompanyReportConfig.Upsert", session)
	ae.Details["scope"] = reportConfigScope(companyID)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// delivery fields are not required at save time: a company config may enable
	// delivery while inheriting unset fields from the global default, and the
	// global config is itself only a set of defaults. completeness is enforced on
	// the effective (merged) config when a report is actually sent.
	// a nil companyID targets the global default config. enabling and on-finish
	// delivery are per company decisions, so they are never stored on the global
	// config which only supplies default fields.
	if companyID == nil {
		incoming.CompanyID = nullable.NewNullNullable[uuid.UUID]()
		incoming.Enabled = false
		incoming.SendOnFinish = false
	} else {
		incoming.CompanyID = nullable.NewNullableWithValue(*companyID)
	}
	// check if a config already exists for this scope
	existing, err := s.CompanyReportConfigRepository.GetByCompanyID(ctx, companyID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("failed to look up existing report config", "error", err)
		return nil, errs.Wrap(err)
	}
	if existing != nil {
		existingID, idErr := existing.ID.Get()
		if idErr != nil {
			s.Logger.Errorw("report config has no id", "error", idErr)
			return nil, errs.Wrap(idErr)
		}
		if err := s.CompanyReportConfigRepository.UpdateByID(ctx, &existingID, incoming); err != nil {
			s.Logger.Errorw("failed to update report config", "error", err)
			return nil, errs.Wrap(err)
		}
		ae.Details["id"] = existingID.String()
		ae.Details["action"] = "update"
		s.AuditLogAuthorized(ae)
		return s.CompanyReportConfigRepository.GetByID(ctx, &existingID)
	}
	// create
	id, err := s.CompanyReportConfigRepository.Insert(ctx, incoming)
	if err != nil {
		s.Logger.Errorw("failed to insert report config", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	ae.Details["action"] = "insert"
	s.AuditLogAuthorized(ae)

	return s.CompanyReportConfigRepository.GetByID(ctx, id)
}

// DeleteByCompanyID removes the report configuration for the given company
func (s *CompanyReportConfig) DeleteByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) error {
	ae := NewAuditEvent("CompanyReportConfig.DeleteByCompanyID", session)
	ae.Details["scope"] = reportConfigScope(companyID)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// delete
	if err := s.CompanyReportConfigRepository.DeleteByCompanyID(ctx, companyID); err != nil {
		s.Logger.Errorw("failed to delete report config", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)

	return nil
}

// ListLogByCompanyID returns the report delivery log for a company using pagination
func (s *CompanyReportConfig) ListLogByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.ReportSendLogOption,
) (*model.Result[model.ReportSendLog], error) {
	ae := NewAuditEvent("CompanyReportConfig.ListLogByCompanyID", session)
	ae.Details["scope"] = reportConfigScope(companyID)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// no audit on read
	return s.ReportSendLogRepository.GetAllByCompanyID(ctx, companyID, options)
}

package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/remotebrowser"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// ReportTemplate is the report template service
type ReportTemplate struct {
	Common
	ReportTemplateRepository *repository.ReportTemplate
}

// Create creates a report template
func (s *ReportTemplate) Create(
	ctx context.Context,
	session *model.Session,
	tmpl *model.ReportTemplate,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("ReportTemplate.Create", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if err := tmpl.Validate(); err != nil {
		s.Logger.Errorw("failed to validate report template", "error", err)
		return nil, errs.Wrap(err)
	}
	id, err := s.ReportTemplateRepository.Insert(ctx, tmpl)
	if err != nil {
		s.Logger.Errorw("failed to insert report template", "error", err)
		return nil, errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return id, nil
}

// GetByID gets a report template by id
func (s *ReportTemplate) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.ReportTemplate, error) {
	ae := NewAuditEvent("ReportTemplate.GetByID", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	tmpl, err := s.ReportTemplateRepository.GetByID(ctx, id, &repository.ReportTemplateOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		s.Logger.Errorw("failed to get report template by ID", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log on read
	return tmpl, nil
}

// GetAll gets report templates
func (s *ReportTemplate) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.ReportTemplateOption,
) (*model.Result[model.ReportTemplate], error) {
	ae := NewAuditEvent("ReportTemplate.GetAll", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	result, err := s.ReportTemplateRepository.GetAll(ctx, companyID, options)
	if err != nil {
		s.Logger.Errorw("failed to get report templates", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// UpdateByID updates a report template
func (s *ReportTemplate) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	tmpl *model.ReportTemplate,
) error {
	ae := NewAuditEvent("ReportTemplate.UpdateByID", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.ReportTemplateRepository.UpdateByID(ctx, id, tmpl); err != nil {
		s.Logger.Errorw("failed to update report template", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// WipeBrowserCache removes the auto-downloaded Chromium binary used for PDF generation.
func (s *ReportTemplate) WipeBrowserCache(
	session *model.Session,
) error {
	ae := NewAuditEvent("ReportTemplate.WipeBrowserCache", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := remotebrowser.WipeBrowserCache(); err != nil {
		s.Logger.Errorw("failed to wipe browser cache", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID deletes a report template
func (s *ReportTemplate) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("ReportTemplate.DeleteByID", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.ReportTemplateRepository.DeleteByID(ctx, id); err != nil {
		s.Logger.Errorw("failed to delete report template", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

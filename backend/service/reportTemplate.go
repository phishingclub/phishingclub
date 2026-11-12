package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/embedded"
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
	// the company of a report template is set on create and never changed by an
	// update, it decides which company the template applies to
	tmpl.CompanyID = nullable.Nullable[uuid.UUID]{}
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

// GetDefaultContent returns the built in default report template, the
// training one when isTraining is set.
func (s *ReportTemplate) GetDefaultContent(session *model.Session, isTraining bool) (string, error) {
	ae := NewAuditEvent("ReportTemplate.GetDefaultContent", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	// no audit log on read
	return defaultReportContent(isTraining), nil
}

func defaultReportContent(isTraining bool) string {
	if isTraining {
		return embedded.DefaultTrainingReportHTML
	}
	return embedded.DefaultReportHTML
}

// previousDefaultReportHashes are the SHA 256 hashes of the earlier built in
// default report templates, white space trimmed. An install keeps the default
// it was seeded with, so these still count as the default.
var previousDefaultReportHashes = map[bool]map[string]bool{
	false: {
		"eb5108aac0dc0252aa50b61d0094149f5104981a8754d1e4408bf3948a7b7092": true,
		"c5a46d34913b0bc5274789e7643926393351fa1ae62c5166e86144cfe408fef6": true,
		"fe019c52bccdd0bf838caff6f28b25836df57aa0880a7d09d5c1046c8ea3df0f": true,
	},
	true: {
		"6edcc3fc69e795baea771235538f657ec734ee3e526f1aaa72c07bca305f013b": true,
		"265f954f73b39be32287ca04d61cad79fb33672586b982032bc774a30334dbcf": true,
		"09a7feb1b0d390ed0cdb0272ad8745a3cc02aa0dbb1eb8db293dd1940bf7f26a": true,
	},
}

// isDefaultReportContent reports whether content is the built in default
// report template of its kind, the current one or an earlier one.
func isDefaultReportContent(content string, isTraining bool) bool {
	content = strings.TrimSpace(content)
	if content == strings.TrimSpace(defaultReportContent(isTraining)) {
		return true
	}
	sum := sha256.Sum256([]byte(content))
	return previousDefaultReportHashes[isTraining][hex.EncodeToString(sum[:])]
}

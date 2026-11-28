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
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// ReportTemplate is the report template service
type ReportTemplate struct {
	Common
	ReportTemplateRepository *repository.ReportTemplate
	// RemoteBrowserExecPath is the browser binary report previews render
	// with, found automatically when empty
	RemoteBrowserExecPath string
	// CompanyRepository refuses new data in a company that is being deleted
	CompanyRepository *repository.Company
}

// Create creates a report template
func (s *ReportTemplate) Create(
	ctx context.Context,
	session *model.Session,
	tmpl *model.ReportTemplate,
) (_ *uuid.UUID, err error) {
	ae := NewAuditEvent("ReportTemplate.Create", session)
	defer s.auditDone(ae, &err)
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
	unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, optionalID(tmpl.CompanyID))
	if err != nil {
		return nil, err
	}
	defer unlockCompany()
	id, err := s.ReportTemplateRepository.Insert(ctx, tmpl)
	if err != nil {
		s.Logger.Errorw("failed to insert report template", "error", err)
		return nil, errs.Wrap(err)
	}
	if id != nil {
		ae.Details["id"] = id.String()
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
) (err error) {
	ae := NewAuditEvent("ReportTemplate.UpdateByID", session)
	defer s.auditDone(ae, &err)
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
	// a program template stays a program template, and a campaign template
	// never becomes one
	tmpl.IsProgram = nullable.Nullable[bool]{}
	if tmpl.IsTrainingOn() {
		existing, err := s.ReportTemplateRepository.GetByID(ctx, id, &repository.ReportTemplateOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if existing.IsProgramOn() {
			return validate.WrapErrorWithField(errors.New("a program report template cannot be a training template"), "isTraining")
		}
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
) (err error) {
	ae := NewAuditEvent("ReportTemplate.WipeBrowserCache", session)
	defer s.auditDone(ae, &err)
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
) (err error) {
	ae := NewAuditEvent("ReportTemplate.DeleteByID", session)
	defer s.auditDone(ae, &err)
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

// GetDefaultContent returns the built in default report template of a kind
func (s *ReportTemplate) GetDefaultContent(session *model.Session, kind string) (string, error) {
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
	return defaultReportContentForKind(kind), nil
}

// defaultReportContentForKind returns the built in default report template of
// a kind, the simulation one for an unknown kind
func defaultReportContentForKind(kind string) string {
	switch kind {
	case model.ReportKindProgram:
		return embedded.DefaultProgramReportHTML
	case model.ReportKindTraining:
		return embedded.DefaultTrainingReportHTML
	}
	return embedded.DefaultReportHTML
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
	false: embedded.PreviousDefaultReportHashes,
	true:  embedded.PreviousDefaultTrainingReportHashes,
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

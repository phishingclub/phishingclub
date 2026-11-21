package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// GetCalendarEntries returns the programs that run between start and end for
// the dashboard calendar. Only names, dates and state are given, never who a
// program is sent to.
func (s *Program) GetCalendarEntries(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	start time.Time,
	end time.Time,
	options *repository.ProgramOption,
) (*model.Result[model.ProgramCalendarEntry], error) {
	ae := NewAuditEvent("Program.GetCalendarEntries", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if end.Before(start) {
		return nil, errs.NewValidationError(errors.New("the end must not be before the start"))
	}
	result, err := s.ProgramRepository.GetCalendarEntries(ctx, companyID, start, end, options)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	now := time.Now()
	for _, entry := range result.Rows {
		entry.SetStatus(now)
	}
	return result, nil
}

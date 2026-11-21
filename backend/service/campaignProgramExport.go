package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// ProgramEventsExport is the event list of one program in a company export.
// The sends are named by the program and the template, never by the hidden
// campaign. An anonymous program has no events in the export.
type ProgramEventsExport struct {
	ProgramName string
	Anonymous   bool
	// TemplateByCampaign maps a hidden campaign to the template it sends
	TemplateByCampaign map[uuid.UUID]string
	programID          *uuid.UUID
	repository         *repository.Campaign
}

// EachEvent calls fn with every event of the program, newest first when desc
// is set and oldest first otherwise, read one row at a time so the events of
// a large program are never held in memory. An anonymous program gives no
// events. The row passed to fn is reused, fn must copy what it keeps.
func (e *ProgramEventsExport) EachEvent(
	ctx context.Context,
	desc bool,
	fn func(*repository.ProgramEventExportRow) error,
) error {
	if e.Anonymous || e.programID == nil {
		return nil
	}
	return e.repository.EachProgramEventForExport(ctx, e.programID, desc, fn)
}

// GetProgramEventsExport returns the programs of exactly the company, or the
// global programs when companyID is nil, for the company export. The events
// of each program are read when they are written, see EachEvent. Events of an
// anonymous program are left out, it gives no per person data.
func (c *Campaign) GetProgramEventsExport(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) ([]*ProgramEventsExport, error) {
	ae := NewAuditEvent("Campaign.GetProgramEventsExport", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
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
	programs, err := c.CampaignRepository.GetProgramsForExport(ctx, companyID)
	if err != nil {
		c.Logger.Errorw("failed to get programs for export", "error", err)
		return nil, errs.Wrap(err)
	}
	out := make([]*ProgramEventsExport, 0, len(programs))
	for _, p := range programs {
		section := &ProgramEventsExport{
			ProgramName:        p.Name,
			Anonymous:          p.IsAnonymous,
			TemplateByCampaign: map[uuid.UUID]string{},
			repository:         c.CampaignRepository,
		}
		out = append(out, section)
		// while an Anonymize All runs the campaigns it has not reached still
		// name people, so the program is listed without its events the same
		// way as an anonymous one
		if p.IsAnonymous || p.Anonymizing || p.ID == nil {
			continue
		}
		section.programID = p.ID
		section.TemplateByCampaign, err = c.CampaignRepository.GetProgramTemplateNames(ctx, p.ID)
		if err != nil {
			c.Logger.Errorw("failed to get program templates for export", "error", err)
			return nil, errs.Wrap(err)
		}
	}
	// no audit log on read, the export itself is audited
	return out, nil
}

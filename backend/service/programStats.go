package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// programAttentionReason tells why a program that is not closed needs a look,
// empty when it is fine. A template that cannot be sent needs attention, and
// so does a running program whose groups are empty: nobody was ever enrolled
// or everyone left. A program counts as running once the membership pass
// marked it started, or once it is past its start by more than
// repository.ProgramStartGrace. A program whose people all completed is fine.
// A running program whose sends do not go out needs attention too, see
// programStalledReason. repository needsAttention filters the list by the
// same rule. usable maps each template id to whether it can be sent, a
// missing id is a deleted template.
func programAttentionReason(
	program *model.Program,
	sum *repository.ProgramListSummary,
	usable map[uuid.UUID]bool,
	now time.Time,
) string {
	for _, tid := range sum.StepTemplateIDs {
		if !usable[tid] {
			return "A template cannot be sent. Check its domain, sender and pages."
		}
	}
	running := program.StartedAt != nil
	if start, err := program.StartAt.Get(); err == nil && !start.After(now.Add(-repository.ProgramStartGrace)) {
		running = true
	}
	if running && sum.People == 0 {
		return "The groups are empty, so nobody is enrolled."
	}
	return programStalledReason(program, sum, now)
}

// programStalledReason tells when a started program that is not paused does
// not send: sends failed within repository.ProgramFailWindow while none went
// out, or a queued send not tried yet waits more than repository.ProgramStallGrace past
// its send time, also when nobody is active any more, as the last send of a
// program that does not loop completes the person when it is queued. Or the
// next send of an active person is more than that overdue, the advance of an
// active person failed repository.ProgramStallFailures times in a row, or an
// active person has no next send while nothing is queued. The due and send
// times are drawn inside the sending days, hours and skip ranges, so a
// weekend, a holiday or the delay of a new member is not a stall. repository
// stalled matches the same programs.
func programStalledReason(program *model.Program, sum *repository.ProgramListSummary, now time.Time) string {
	paused := program.PausedAt.IsSpecified() && !program.PausedAt.IsNull()
	if program.StartedAt == nil || paused {
		return ""
	}
	limit := now.Add(-repository.ProgramStallGrace)
	var late *time.Time
	if sum.NextDueAt != nil && sum.NextDueAt.Before(limit) {
		late = sum.NextDueAt
	}
	if sum.OldestQueuedAt != nil && sum.OldestQueuedAt.Before(limit) && (late == nil || sum.OldestQueuedAt.Before(*late)) {
		late = sum.OldestQueuedAt
	}
	// an anonymous program hides when a person is sent, so its reasons give
	// no times
	anonymous := program.IsAnonymousOn()
	if late != nil && anonymous {
		return "Sends are overdue. Check the sender and sending hours."
	}
	if late != nil {
		days := int(now.Sub(*late) / (24 * time.Hour))
		unit := "days"
		if days == 1 {
			unit = "day"
		}
		return fmt.Sprintf("Sends are %d %s overdue. Check the sender and sending hours.", days, unit)
	}
	failSince := now.Add(-repository.ProgramFailWindow)
	if sum.NewestFailedAt != nil && sum.NewestFailedAt.After(failSince) &&
		(sum.NewestSentAt == nil || !sum.NewestSentAt.After(failSince)) {
		if anonymous {
			return "Sends failed and none went out. Check the sender."
		}
		return "Sends failed and none went out in the last 7 days. Check the sender."
	}
	if sum.Failing > 0 {
		return "Planning the next send keeps failing for some people. Check the templates and the sender."
	}
	// a failed send is never tried again, so only sends not tried yet count as
	// planned. OldestQueuedAt is set when there is one.
	if sum.Unplanned > 0 && sum.OldestQueuedAt == nil {
		return "Active people have no next send planned. Check the program schedule."
	}
	return ""
}

// GetStats counts the programs per state for the dashboard. Without a company
// it counts every company, like the dashboard campaign counts. Running
// programs that need attention are counted apart from the active ones, and a
// paused program that has not started counts as scheduled, as the program
// list shows them.
func (s *Program) GetStats(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.ProgramsStatView, error) {
	ae := NewAuditEvent("Program.GetStats", session)
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
	// the counts use the same rules as the status filter of the program list
	view, err := s.ProgramRepository.CountByStatus(ctx, companyID, time.Now())
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return view, nil
}

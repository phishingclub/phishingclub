package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// programScheduleKey returns the settings that decide when a person is sent
// next, as one comparable text
func programScheduleKey(p *model.Program) string {
	return fmt.Sprint(
		p.IntervalOrDefault(),
		p.SpreadOrDefault(),
		p.ConstraintWeekDays,
		p.ConstraintStartTime,
		p.ConstraintEndTime,
		p.Location().String(),
		p.BlackoutDates,
	)
}

// programParkedAtEnd reports whether a person waits at the program end, which
// the scheduler does when their next send falls at or after the end
func programParkedAtEnd(e *database.ProgramEnrollment, end *time.Time) bool {
	return end != nil && e.NextDueAt != nil && e.NextDueAt.Equal(*end)
}

// replanAfterEdit applies a stored edit to the people of a started program
// that is not closed. Completed people who now have steps left, or every
// completed person when the program now loops, are made active again. When the
// timing settings changed, the next send of every active person who is not due
// right now is drawn again. A paused program keeps the new times on hold until
// it is resumed. The caller holds the lifecycle lock.
func (s *Program) replanAfterEdit(
	ctx context.Context,
	before *model.Program,
	id *uuid.UUID,
	editsSteps bool,
) error {
	after, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return errs.Wrap(err)
	}
	if after.IsClosed() || after.StartedAt == nil {
		return nil
	}
	loopChanged := after.LoopModeOrDefault() != before.LoopModeOrDefault()
	scheduleChanged := programScheduleKey(before) != programScheduleKey(after)
	reactivate := editsSteps || loopChanged
	// a person whose send fell at or after the old end waits with the old end
	// as their next send. When the end moves later or is cleared they are
	// drawn again over the spread instead of all being sent at the old end
	oldEnd := before.EndAtTime()
	newEnd := after.EndAtTime()
	endMovedLater := oldEnd != nil && (newEnd == nil || newEnd.After(*oldEnd))
	if !reactivate && !scheduleChanged && !endMovedLater {
		return nil
	}
	enrollments, err := s.ProgramRepository.GetEnrollmentsByProgramID(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	now := time.Now()
	completed := []*database.ProgramEnrollment{}
	redraw := []*database.ProgramEnrollment{}
	for _, e := range enrollments {
		switch {
		case e.Status == model.ProgramEnrollmentCompleted && reactivate:
			completed = append(completed, e)
		case e.Status == model.ProgramEnrollmentActive &&
			(scheduleChanged || (endMovedLater && programParkedAtEnd(e, oldEnd))) &&
			// a person due now is planned by the tick, one waiting after a failed
			// attempt keeps the wait
			e.NextDueAt != nil && e.NextDueAt.After(now) && e.AdvanceFailures == 0:
			redraw = append(redraw, e)
		}
	}
	if len(completed) == 0 && len(redraw) == 0 {
		return nil
	}
	reactivated := []*database.ProgramEnrollment{}
	if len(completed) > 0 {
		reactivated, err = s.completedWithStepsLeft(ctx, after, completed)
		if err != nil {
			return err
		}
	}
	for _, e := range reactivated {
		roundStart := now
		if e.RoundStartAt != nil {
			roundStart = programNextRoundStart(after, *e.RoundStartAt, now)
		}
		due := programPickSendTimeAfter(after, roundStart, now, programMinNextSend(after, e.LastSendAt), s.rng())
		if err := s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
			"status":           model.ProgramEnrollmentActive,
			"completed_at":     nil,
			"round_start_at":   roundStart,
			"next_due_at":      due,
			"advance_failures": 0,
		}); err != nil {
			return errs.Wrap(err)
		}
	}
	for _, e := range redraw {
		roundStart := now
		if e.RoundStartAt != nil {
			roundStart = *e.RoundStartAt
			// after a send the stored round is one old interval after the round
			// of that send, so it moves with the new interval
			if e.LastSendAt != nil {
				roundStart = programNextRoundStart(after, roundStart.Add(-before.IntervalOrDefault()), now)
			}
		}
		if roundStart.Add(after.SpreadOrDefault()).Before(now) {
			roundStart = now
		}
		due := programPickSendTimeAfter(after, roundStart, now, programMinNextSend(after, e.LastSendAt), s.rng())
		if err := s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
			"round_start_at": roundStart,
			"next_due_at":    due,
		}); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// completedWithStepsLeft returns the completed people who have a scheduled
// step left in their current loop, or all of them when the program loops. The
// deliveries are read in one query by recipient. A person whose sends are only
// known by pseudonym may be returned too, the next tick finds the sends by
// pseudonym and completes them again without a send.
func (s *Program) completedWithStepsLeft(
	ctx context.Context,
	program *model.Program,
	completed []*database.ProgramEnrollment,
) ([]*database.ProgramEnrollment, error) {
	steps := program.Steps
	if len(steps) == 0 {
		return nil, nil
	}
	if program.LoopModeOrDefault() == model.ProgramLoopLoop {
		return completed, nil
	}
	programID := program.ID.MustGet()
	ids := make([]*uuid.UUID, 0, len(completed))
	for _, e := range completed {
		ids = append(ids, e.RecipientID)
	}
	var recipientIDs []*uuid.UUID
	// a short list is read by recipient, a long one in one read of the program
	if len(ids) <= programPageFactsMax {
		recipientIDs = ids
	}
	deliveries, err := s.ProgramRepository.GetDeliveries(ctx, &programID, recipientIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	given := deliveriesByPersonLoop(deliveries)
	out := []*database.ProgramEnrollment{}
	for _, e := range completed {
		delivered := deliveredSteps(steps, given[programPersonLoop{recipient: *e.RecipientID, loop: e.LoopCount}])
		if len(undeliveredSteps(steps, delivered)) > 0 {
			out = append(out, e)
		}
	}
	return out, nil
}

// resolveMembershipOnResume runs the group membership check of one program
// before its held sends are let out. People back in the groups are handled as
// on a tick. People missing from the groups right now lose their held sends, a
// step whose send is cancelled is offered again when they come back.
func (s *Program) resolveMembershipOnResume(ctx context.Context, id *uuid.UUID, now time.Time) {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		s.Logger.Errorw("failed to load program on resume", "programID", id.String(), "error", err)
		return
	}
	members, err := s.resolveMembership(ctx, program)
	if err != nil {
		s.Logger.Errorw("failed to resolve membership on resume", "programID", id.String(), "error", err)
		return
	}
	existing, err := s.ProgramRepository.GetEnrollmentsByProgramID(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get enrollments on resume", "programID", id.String(), "error", err)
		return
	}
	s.enrollJoiners(ctx, program, members, existing, now)
	s.exitLeavers(ctx, id, members, existing, now)
	memberSet := make(map[uuid.UUID]bool, len(members))
	for _, r := range members {
		memberSet[r.ID.MustGet()] = true
	}
	gone := []*uuid.UUID{}
	for _, e := range existing {
		if e.Status == model.ProgramEnrollmentActive && !memberSet[*e.RecipientID] {
			gone = append(gone, e.RecipientID)
		}
	}
	if err := s.ProgramRepository.CancelUnsentForProgramRecipients(ctx, id, gone); err != nil {
		s.Logger.Errorw("failed to cancel held sends of people who left", "programID", id.String(), "error", err)
	}
}

// respreadHeldSends moves the sends held by a pause so they keep the program
// window. A send whose time passed during the pause gets a new time drawn over
// the spread from now, the same as a person whose round came due during the
// pause. A time still ahead that is now outside the window, for example on a
// new blackout day, moves to the next window opening.
func (s *Program) respreadHeldSends(ctx context.Context, program *model.Program, now time.Time) error {
	programID := program.ID.MustGet()
	held, err := s.ProgramRepository.GetHeldSends(ctx, &programID)
	if err != nil {
		return errs.Wrap(err)
	}
	if len(held) == 0 {
		return nil
	}
	moves := map[time.Time][]*uuid.UUID{}
	for _, h := range held {
		if h.SendAt == nil {
			continue
		}
		var target time.Time
		if h.SendAt.After(now) {
			target = programNextWindowSlot(program, *h.SendAt)
		} else {
			target = programPickSendTimeAfter(program, now, now, time.Time{}, s.rng())
		}
		if target.Equal(*h.SendAt) {
			continue
		}
		target = target.UTC()
		id := h.ID
		moves[target] = append(moves[target], &id)
	}
	for target, ids := range moves {
		if err := s.ProgramRepository.SetSendAt(ctx, ids, target); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

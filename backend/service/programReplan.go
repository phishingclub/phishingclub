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

// programPersonGone reports whether a completed person is missing from the
// groups for longer than the grace period. Such a person is not made active
// again by an edit, the membership pass does that when they come back.
func programPersonGone(e *database.ProgramEnrollment, now time.Time) bool {
	return e.MissingSince != nil && now.Sub(*e.MissingSince) >= programMissingGrace
}

// replanAfterEdit applies a stored edit to the people of a program, see
// applyEditToPeople
func (s *Program) replanAfterEdit(
	ctx context.Context,
	before *model.Program,
	id *uuid.UUID,
	editsSteps bool,
) error {
	_, err := s.applyEditToPeople(ctx, before, id, editsSteps)
	return err
}

// applyEditToPeople applies a stored edit to the people of a started program
// that is not closed and returns how many queued sends it cancelled. before is
// the program as it was before the edit, with its steps when editsSteps is
// set. The queued sends of the steps the edit removed are cancelled first and
// their people planned again, see cancelRemovedStepSends. Then the people are
// planned again for the other changes, see replanPeople.
func (s *Program) applyEditToPeople(
	ctx context.Context,
	before *model.Program,
	id *uuid.UUID,
	editsSteps bool,
) (int, error) {
	after, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return 0, errs.Wrap(err)
	}
	if after.IsClosed() || after.StartedAt == nil {
		return 0, nil
	}
	cancelled := 0
	planned := map[uuid.UUID]bool{}
	if editsSteps {
		cancelled, planned, err = s.cancelRemovedStepSends(ctx, before, after)
		if err != nil {
			return cancelled, err
		}
		// other writers get the database between the two parts
		if cancelled > 0 {
			if err := repository.PauseBetweenWrites(ctx); err != nil {
				return cancelled, err
			}
		}
	}
	return cancelled, s.replanPeople(ctx, before, after, editsSteps, planned)
}

// replanPeople plans the people of a program again after an edit. Completed
// people who now have steps left, or every completed person when the program
// now loops, are made active again, except the people gone from the groups
// for longer than the grace period. When the timing settings changed, the next
// send of every active person who is not due right now is drawn again. A
// paused program keeps the new times on hold until it is resumed. People in
// skip were planned already by the edit and are left alone. The caller must
// not hold the lifecycle lock: the people are read without it and written in
// chunks, each under the lock for its own write only and only while the
// program is not closed or being closed or deleted. A person the scheduler
// advanced in between keeps the newer plan of the scheduler only in part, the
// newest send check of the advance keeps them from being sent too soon. A
// chunk that fails leaves the people after it with their old plan, which is
// valid, and the error is returned.
func (s *Program) replanPeople(
	ctx context.Context,
	before *model.Program,
	after *model.Program,
	editsSteps bool,
	skip map[uuid.UUID]bool,
) error {
	programID := after.ID.MustGet()
	id := &programID
	loopChanged := after.LoopModeOrDefault() != before.LoopModeOrDefault()
	scheduleChanged := programScheduleKey(before) != programScheduleKey(after)
	// a person whose send fell at or after the old end waits with the old end
	// as their next send. When the end moves later or is cleared they are
	// drawn again over the spread instead of all being sent at the old end
	oldEnd := before.EndAtTime()
	newEnd := after.EndAtTime()
	endMovedLater := oldEnd != nil && (newEnd == nil || newEnd.After(*oldEnd))
	// a completed person whose last send an earlier end cancelled has a
	// template left, so an end moved later plans them again too
	reactivate := editsSteps || loopChanged || endMovedLater
	if !reactivate && !scheduleChanged && !endMovedLater {
		return nil
	}
	enrollments, err := s.ProgramRepository.GetEnrollmentPlansByProgramID(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	now := time.Now()
	completed := []*database.ProgramEnrollment{}
	redraw := []*database.ProgramEnrollment{}
	for _, e := range enrollments {
		switch {
		case skip[*e.ID]:
			// planned by the edit already
		case e.Status == model.ProgramEnrollmentCompleted && reactivate && !programPersonGone(e, now):
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
	reactivatePlans := make([]repository.EnrollmentPlan, 0, len(reactivated))
	for _, e := range reactivated {
		roundStart := now
		if e.RoundStartAt != nil {
			roundStart = programNextRoundStart(after, *e.RoundStartAt, now)
		}
		due := programPickSendTimeAfter(after, roundStart, now, programMinNextSend(after, e.LastSendAt), s.rng())
		reactivatePlans = append(reactivatePlans, repository.EnrollmentPlan{ID: e.ID, RoundStartAt: roundStart, NextDueAt: due})
	}
	redrawPlans := make([]repository.EnrollmentPlan, 0, len(redraw))
	for _, e := range redraw {
		roundStart := now
		if e.RoundStartAt != nil {
			roundStart = *e.RoundStartAt
			// after a send the stored round is one old interval after the round
			// of that send, so it moves with the new interval
			if e.LastSendAt != nil {
				roundStart = programNextRoundStart(after, programAddInterval(before, roundStart, -before.IntervalOrDefault()), now)
			}
		}
		if programAddInterval(after, roundStart, after.SpreadOrDefault()).Before(now) {
			roundStart = now
		}
		due := programPickSendTimeAfter(after, roundStart, now, programMinNextSend(after, e.LastSendAt), s.rng())
		redrawPlans = append(redrawPlans, repository.EnrollmentPlan{ID: e.ID, RoundStartAt: roundStart, NextDueAt: due})
	}
	notClosed := func(p *model.Program) bool { return !p.IsClosed() && !p.IsStopping() }
	done, err := s.writeInChunks(ctx, id, len(reactivatePlans), notClosed, func(start, end int) error {
		return s.ProgramRepository.PlanEnrollments(ctx, reactivatePlans[start:end], map[string]any{
			"status":           model.ProgramEnrollmentActive,
			"completed_at":     nil,
			"advance_failures": 0,
		})
	})
	if err != nil || !done {
		return err
	}
	if len(reactivatePlans) > 0 && len(redrawPlans) > 0 {
		if err := repository.PauseBetweenWrites(ctx); err != nil {
			return err
		}
	}
	_, err = s.writeInChunks(ctx, id, len(redrawPlans), notClosed, func(start, end int) error {
		return s.ProgramRepository.PlanEnrollments(ctx, redrawPlans[start:end], nil)
	})
	return err
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

// programResumePlan is what a resume reads and plans before it takes the
// lifecycle lock, see resumeMembers
type programResumePlan struct {
	// listed is the program as read before the lock
	listed *model.Program
	// members are the people in the groups of listed
	members []*uuid.UUID
	// existing are the enrollments of the program
	existing []*database.ProgramEnrollment
	// joiners are the new enrollments of the members who have none
	joiners []*database.ProgramEnrollment
}

// resumeMembers reads and plans the group membership of a program for a
// resume, without the lifecycle lock: the members, the enrollments and the
// new people. A paused program enrolls nobody, so the enrollments read here
// still hold under the lock. resolveMembershipOnResume refuses the plan when
// the program changed in between. It returns nil when the program has not
// started. A failed read is returned, the resume then stops and the program
// stays paused, as held sends must not be let out to people who left.
func (s *Program) resumeMembers(ctx context.Context, id *uuid.UUID, now time.Time) (*programResumePlan, error) {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithRecipientGroups: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if program.StartedAt == nil {
		return nil, nil
	}
	members, err := s.resolveMembership(ctx, program)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	existing, err := s.ProgramRepository.GetEnrollmentPlansByProgramID(ctx, id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &programResumePlan{
		listed:   program,
		members:  members,
		existing: existing,
		joiners:  s.planJoiners(program, members, enrollmentsByRecipient(existing), now),
	}, nil
}

// programResumeMembership is what the membership check of a resume found, see
// resolveMembershipOnResume
type programResumeMembership struct {
	// gone are the active people missing from the groups now, they lose their
	// held sends
	gone []*uuid.UUID
	// leavers are the people to mark missing or exit
	leavers programLeavers
	// returners are the new people and the people back in the groups, written
	// in chunks after the lock is let go
	returners programReturners
}

// resolveMembershipOnResume runs the group membership check of one program
// before its held sends are let out, from the plan read before the lock.
// People back in the groups are handled as on a tick. People missing from
// the groups right now lose their held sends, a step whose send is cancelled
// is offered again when they come back. The new people, the people back in
// the groups and the people who left are only found here,
// finishResumeMembership writes them and cancels the sends after the lock is
// let go and before the held sends are let out.
// It reports changed, and writes nothing, when the program was edited or its
// groups changed since the plan was read, the caller then plans again. The
// caller holds the lifecycle lock.
func (s *Program) resolveMembershipOnResume(
	ctx context.Context,
	id *uuid.UUID,
	plan *programResumePlan,
	now time.Time,
) (programResumeMembership, bool, error) {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return programResumeMembership{}, false, errs.Wrap(err)
	}
	if plan == nil {
		// the program started after the plan was read
		return programResumeMembership{}, program.StartedAt != nil, nil
	}
	if !sameGroupIDs(plan.listed, program) || !sameTime(plan.listed.UpdatedAt, program.UpdatedAt) {
		return programResumeMembership{}, true, nil
	}
	returners := s.planReturners(program, plan.members, plan.existing, now)
	returners.joiners = plan.joiners
	memberSet := memberIDSet(plan.members)
	// a completed person may still have a held last send, which must not go
	// out to someone who left during the pause
	gone := []*uuid.UUID{}
	for _, e := range plan.existing {
		held := e.Status == model.ProgramEnrollmentActive || e.Status == model.ProgramEnrollmentCompleted
		if held && !memberSet[*e.RecipientID] {
			gone = append(gone, e.RecipientID)
		}
	}
	return programResumeMembership{
		gone:      gone,
		leavers:   planLeavers(plan.members, plan.existing, now),
		returners: returners,
	}, false, nil
}

// finishResumeMembership writes the new people, the people back in the groups
// and the people who left that resolveMembershipOnResume found, in chunks
// while allowed reports true, see writeReturners and writeLeavers, and
// cancels the unsent sends of the people missing now and of everyone who
// left the program before, see cancelGoneSends. A request that ends does not
// stop the writes. It reports whether every chunk was written. The caller
// must not hold the lifecycle lock.
func (s *Program) finishResumeMembership(
	ctx context.Context,
	id *uuid.UUID,
	m programResumeMembership,
	now time.Time,
	allowed func(*model.Program) bool,
) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	if m.returners.program != nil {
		done, err := s.writeReturners(ctx, m.returners, allowed)
		if err != nil || !done {
			return done, err
		}
	}
	done, err := s.writeLeavers(ctx, id, m.leavers, now, allowed)
	if err != nil || !done {
		return done, err
	}
	// both cancels check allowed under the lock before each chunk, so a person
	// an edit of the groups brings back during the resume keeps the held send.
	// A person who leaves a group without a program edit during the resume
	// still gets the held send, the next membership pass handles them
	done, err = s.cancelPeopleSends(ctx, id, m.gone, false, allowed)
	if err != nil || !done {
		return done, err
	}
	return s.cancelGoneSends(ctx, id, allowed)
}

// heldSendMoves returns the new times of the sends held by a pause, so they
// keep the program window. A send whose time passed during the pause gets a
// new time drawn over the spread from now, the same as a person whose round
// came due during the pause. A time still ahead that is now outside the
// window, for example on a new blackout day, moves to the next window
// opening. A send that keeps its time is left out.
func (s *Program) heldSendMoves(ctx context.Context, program *model.Program, now time.Time) ([]repository.SendMove, error) {
	programID := program.ID.MustGet()
	held, err := s.ProgramRepository.GetHeldSends(ctx, &programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	moves := make([]repository.SendMove, 0, len(held))
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
		moves = append(moves, repository.SendMove{ID: h.ID, SendAt: target.UTC()})
	}
	return moves, nil
}

// removedStepIDs returns the ids of the steps in before that are not in after.
// A step whose template changed is stored as a new step, so its old id is
// removed.
func removedStepIDs(before []*model.ProgramStep, after []*model.ProgramStep) []*uuid.UUID {
	kept := make(map[uuid.UUID]bool, len(after))
	for _, st := range after {
		kept[st.ID.MustGet()] = true
	}
	out := []*uuid.UUID{}
	for _, st := range before {
		id := st.ID.MustGet()
		if !kept[id] {
			out = append(out, &id)
		}
	}
	return out
}

// cancelRemovedStepSends cancels every send of the steps an edit removed that
// is neither sent nor cancelled, also a send held by a pause or moved to the
// next window opening, so a removed template is never sent again. The sends
// are cancelled in chunks, each under the lifecycle lock for its own write
// only. The people who lost a send are then planned again, see
// planAfterCancelledSends. It returns how many sends it cancelled and the
// enrollments it planned.
func (s *Program) cancelRemovedStepSends(
	ctx context.Context,
	before *model.Program,
	after *model.Program,
) (int, map[uuid.UUID]bool, error) {
	removed := removedStepIDs(before.Steps, after.Steps)
	if len(removed) == 0 {
		return 0, nil, nil
	}
	programID := after.ID.MustGet()
	sends, err := s.ProgramRepository.GetUnsentSendsOfSteps(ctx, &programID, removed)
	if err != nil {
		return 0, nil, errs.Wrap(err)
	}
	if len(sends) == 0 {
		return 0, nil, nil
	}
	ids := make([]*uuid.UUID, 0, len(sends))
	for i := range sends {
		ids = append(ids, &sends[i].ID)
	}
	notClosed := func(p *model.Program) bool { return !p.IsClosed() && !p.IsStopping() }
	cancelled := 0
	done, err := s.writeInChunks(ctx, &programID, len(ids), notClosed, func(start, end int) error {
		n, err := s.ProgramRepository.CancelSends(ctx, ids[start:end])
		cancelled += n
		return err
	})
	if err != nil || !done {
		return cancelled, nil, err
	}
	if err := repository.PauseBetweenWrites(ctx); err != nil {
		return cancelled, nil, err
	}
	planned, err := s.planAfterCancelledSends(ctx, before, after, removed, sends, time.Now())
	return cancelled, planned, err
}

// planAfterCancelledSends makes the people whose send of a removed step was
// cancelled due again at about the time that send would have gone out, the
// cancelled send time or now when it has passed, moved into the window. The
// next tick then gives them one of the templates they have left. An active
// person gets back the round of the cancelled send, so no round is skipped. A
// completed person with a template left in the round is made active again, a
// completed person with none left, or gone from the groups for longer than
// the grace period, stays completed with one send fewer. A
// person waiting after a failed attempt keeps the wait, and a person already
// due earlier is left as is. It returns the enrollments it planned.
func (s *Program) planAfterCancelledSends(
	ctx context.Context,
	before *model.Program,
	after *model.Program,
	removed []*uuid.UUID,
	sends []repository.ProgramStepUnsentSend,
	now time.Time,
) (map[uuid.UUID]bool, error) {
	programID := after.ID.MustGet()
	enrollments, err := s.ProgramRepository.GetEnrollmentPlansByProgramID(ctx, &programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	byRecipient := make(map[uuid.UUID]*database.ProgramEnrollment, len(enrollments))
	byPseudonym := map[uuid.UUID]*database.ProgramEnrollment{}
	for _, e := range enrollments {
		if e.RecipientID != nil {
			byRecipient[*e.RecipientID] = e
		}
		if e.AnonymizedID != nil {
			byPseudonym[*e.AnonymizedID] = e
		}
	}
	// the earliest cancelled send time of each person, never before now
	sendAt := map[uuid.UUID]time.Time{}
	people := []*database.ProgramEnrollment{}
	for _, sd := range sends {
		var e *database.ProgramEnrollment
		if sd.RecipientID != nil {
			e = byRecipient[*sd.RecipientID]
		}
		if e == nil && sd.AnonymizedID != nil {
			e = byPseudonym[*sd.AnonymizedID]
		}
		if e == nil {
			continue
		}
		at := now
		if sd.SendAt != nil && sd.SendAt.After(now) {
			at = *sd.SendAt
		}
		t, ok := sendAt[*e.ID]
		if !ok {
			people = append(people, e)
		}
		if !ok || at.Before(t) {
			sendAt[*e.ID] = at
		}
	}
	removedSet := make(map[uuid.UUID]bool, len(removed))
	for _, id := range removed {
		removedSet[*id] = true
	}
	activePlans := []repository.EnrollmentPlan{}
	completed := []*database.ProgramEnrollment{}
	for _, e := range people {
		switch {
		case e.Status == model.ProgramEnrollmentActive && e.AdvanceFailures == 0:
			due := programNextWindowSlot(after, sendAt[*e.ID])
			if e.NextDueAt != nil && !e.NextDueAt.After(due) {
				continue
			}
			roundStart := now
			if e.RoundStartAt != nil {
				roundStart = *e.RoundStartAt
				// the advance that planned the cancelled send moved the round one
				// interval on, it is moved back so the replacement keeps the round
				if e.LastStepID != nil && removedSet[*e.LastStepID] {
					roundStart = programAddInterval(before, roundStart, -before.IntervalOrDefault())
				}
			}
			activePlans = append(activePlans, repository.EnrollmentPlan{ID: e.ID, RoundStartAt: roundStart, NextDueAt: due})
		case e.Status == model.ProgramEnrollmentCompleted && !programPersonGone(e, now):
			completed = append(completed, e)
		}
	}
	reactivatePlans := []repository.EnrollmentPlan{}
	if len(completed) > 0 {
		reactivated, err := s.completedWithStepsLeft(ctx, after, completed)
		if err != nil {
			return nil, err
		}
		for _, e := range reactivated {
			// completing a person leaves the round of their last send
			roundStart := now
			if e.RoundStartAt != nil {
				roundStart = *e.RoundStartAt
			}
			reactivatePlans = append(reactivatePlans, repository.EnrollmentPlan{
				ID: e.ID, RoundStartAt: roundStart, NextDueAt: programNextWindowSlot(after, sendAt[*e.ID]),
			})
		}
	}
	planned := map[uuid.UUID]bool{}
	notClosed := func(p *model.Program) bool { return !p.IsClosed() && !p.IsStopping() }
	done, err := s.writeInChunks(ctx, &programID, len(activePlans), notClosed, func(start, end int) error {
		for _, plan := range activePlans[start:end] {
			planned[*plan.ID] = true
		}
		return s.ProgramRepository.PlanEnrollments(ctx, activePlans[start:end], nil)
	})
	if err != nil || !done {
		return planned, err
	}
	if len(activePlans) > 0 && len(reactivatePlans) > 0 {
		if err := repository.PauseBetweenWrites(ctx); err != nil {
			return planned, err
		}
	}
	_, err = s.writeInChunks(ctx, &programID, len(reactivatePlans), notClosed, func(start, end int) error {
		for _, plan := range reactivatePlans[start:end] {
			planned[*plan.ID] = true
		}
		return s.ProgramRepository.PlanEnrollments(ctx, reactivatePlans[start:end], map[string]any{
			"status":           model.ProgramEnrollmentActive,
			"completed_at":     nil,
			"advance_failures": 0,
		})
	})
	return planned, err
}

package service

import (
	"context"
	"errors"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// programMembershipInterval throttles the enrollment resolution phase so the
// group membership diff does not run on every ten second tick.
const programMembershipInterval = 60 * time.Second

// programMissingGrace is how long a person may be missing from the program
// groups before the enrollment is exited, so a short group glitch, like a SCIM
// sync that briefly empties a group, does not cancel their sends
const programMissingGrace = time.Hour

// returnPlan plans the enrollment of a person who left and came back. They
// keep their loop, history and round, unless the round is over, then a new
// round starts now.
func (s *Program) returnPlan(
	program *model.Program,
	e *database.ProgramEnrollment,
	now time.Time,
) repository.EnrollmentPlan {
	roundStart := now
	if e.RoundStartAt != nil && !e.RoundStartAt.Add(program.SpreadOrDefault()).Before(now) {
		roundStart = *e.RoundStartAt
	}
	due := programPickSendTimeAfter(program, roundStart, now, programMinNextSend(program, e.LastSendAt), s.rng())
	return repository.EnrollmentPlan{ID: e.ID, RoundStartAt: roundStart, NextDueAt: due}
}

// resolveEnrollments diffs each active program's live group membership against
// its enrollments, enrolling joiners and exiting leavers. The lifecycle lock is
// taken for one program at a time, so a pause, close or edit waits for one
// program only. The caller must not hold the lock.
func (s *Program) resolveEnrollments(ctx context.Context) error {
	programs, err := s.ProgramRepository.GetActivePrograms(ctx, time.Now())
	if err != nil {
		return errs.Wrap(err)
	}
	for _, program := range programs {
		if err := s.resolveListedProgram(ctx, program); err != nil {
			s.Logger.Errorw("failed to resolve program enrollments", "programID", program.ID.MustGet().String(), "error", err)
			programTickStatsFrom(ctx).fail(err)
			continue
		}
		// the tick enrolled the members, a failed enrollment after Start is
		// done
		s.clearActionFailureOf(program.ID.MustGet(), programActionStart)
	}
	s.finishPendingReplans(ctx)
	return nil
}

// programReplanPassLimit is how many programs with an edit whose people are
// not all planned again one membership pass finishes
const programReplanPassLimit = 5

// finishPendingReplans plans the people again of the programs whose edit was
// cut short, for example by a restart or a busy database, see
// finishPendingReplan. A program whose edit this process plans now is left
// alone. A failure is logged and the mark stays, the next pass tries again.
// The caller must not hold the lifecycle lock.
func (s *Program) finishPendingReplans(ctx context.Context) {
	ids, err := s.ProgramRepository.GetIDsWithReplanPending(ctx, programReplanPassLimit)
	if err != nil {
		s.Logger.Errorw("failed to list programs with an unfinished edit", "error", err)
		programTickStatsFrom(ctx).fail(err)
		return
	}
	for _, id := range ids {
		if !s.claimReplan(*id) {
			continue
		}
		err := func() error {
			// a panic must not keep the claim, or no pass plans this program again
			defer s.releaseReplan(*id)
			return s.finishPendingReplan(ctx, id)
		}()
		if err != nil {
			s.Logger.Errorw("failed to plan program people again after an edit", "programID", id.String(), "error", err)
			programTickStatsFrom(ctx).fail(err)
		}
	}
}

// finishPendingReplan plans the people of one program again the way an edit
// of its steps does, see replanPeople: completed people with a step left, or
// every completed person when the program loops, are made active again, and
// the people waiting at the end the program had before the edit are drawn
// again when the end moved later. The mark is then cleared, unless a newer
// edit set it again in between.
func (s *Program) finishPendingReplan(ctx context.Context, id *uuid.UUID) error {
	after, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return errs.Wrap(err)
	}
	mark := after.ReplanPendingAt
	if mark == nil {
		return nil
	}
	if after.StartedAt != nil && !after.IsClosed() && !after.IsStopping() {
		before := *after
		if after.ReplanOldEndAt != nil {
			before.EndAt = nullable.NewNullableWithValue(*after.ReplanOldEndAt)
		}
		if err := s.replanPeople(ctx, &before, after, true, nil); err != nil {
			return err
		}
	}
	return errs.Wrap(s.ProgramRepository.ClearReplanPending(ctx, id, *mark))
}

// resolveListedProgram resolves one program from the active list, see
// resolveListedProgramWith. The members are read before the lock is taken.
func (s *Program) resolveListedProgram(ctx context.Context, listed *model.Program) error {
	members, err := s.resolveMembership(ctx, listed)
	if err != nil {
		return errs.Wrap(err)
	}
	return s.resolveListedProgramWith(ctx, listed, members)
}

// resolveListedProgramWith resolves one program with the members of the
// listed program already read. The enrollments are read and the new people
// stored before the lock is taken for the rest, the new people in chunks,
// each under the lock for its own write only and only while the program runs
// with the groups it was listed with. Under the lock the program is read
// again, so a program paused, closed or edited in the meantime is skipped or
// uses its current groups, and the people who return and the people who left
// are planned. Nothing else is written under the lock but the start mark. The
// people who return and the people who left are then written in chunks the
// same way as the new people, see writeReturners and writeLeavers. The writes
// change a person only in the status they were planned from, so a person the
// scheduler moved on in between is left alone. Last the unsent sends of
// everyone who left are cancelled, see cancelGoneSends.
func (s *Program) resolveListedProgramWith(ctx context.Context, listed *model.Program, members []*uuid.UUID) error {
	programID := listed.ID.MustGet()
	existing, err := s.ProgramRepository.GetEnrollmentPlansByProgramID(ctx, &programID)
	if err != nil {
		return errs.Wrap(err)
	}
	joiners := s.planJoiners(listed, members, enrollmentsByRecipient(existing), time.Now())
	running := s.runsWithGroupsOf(ctx, listed)
	done, err := s.writeInChunks(ctx, &programID, len(joiners), running,
		func(start, end int) error {
			if err := s.ProgramRepository.InsertEnrollments(ctx, joiners[start:end]); err != nil {
				return err
			}
			programTickStatsFrom(ctx).add(&programID, 0, 0, end-start, 0)
			return nil
		})
	if err != nil || !done {
		return err
	}
	if listed.StartedAt == nil {
		done, err = s.redrawFirstSends(ctx, listed, existing, running)
		if err != nil || !done {
			return err
		}
	}
	// the people just stored are not new on the pass under the lock
	existing = append(existing, joiners...)
	var program *model.Program
	var returners programReturners
	var leavers programLeavers
	if err := s.withLifecycleLock(ctx, func() error {
		now := time.Now()
		// the steps tell whether a completed person who comes back has one left
		current, err := s.ProgramRepository.GetByID(ctx, &programID, &repository.ProgramOption{
			WithSteps:           true,
			WithRecipientGroups: true,
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if current.StatusAt(now) != model.ProgramStatusActive {
			return nil
		}
		if !sameGroupIDs(listed, current) {
			if members, err = s.resolveMembership(ctx, current); err != nil {
				return errs.Wrap(err)
			}
			if existing, err = s.ProgramRepository.GetEnrollmentPlansByProgramID(ctx, &programID); err != nil {
				return errs.Wrap(err)
			}
		}
		// only planned here, the writes are made in chunks after the lock is let go
		returners = s.planReturners(current, members, existing, now)
		returners.joiners = s.planJoiners(current, members, enrollmentsByRecipient(existing), now)
		s.markStarted(ctx, current, now)
		leavers = planLeavers(members, existing, now)
		program = current
		return nil
	}); err != nil {
		return err
	}
	if program == nil {
		return nil
	}
	running = s.runsWithGroupsOf(ctx, program)
	done, err = s.writeReturners(ctx, returners, running)
	if err != nil || !done {
		return err
	}
	if _, err := s.writeLeavers(ctx, &programID, leavers, time.Now(), running); err != nil {
		return err
	}
	_, err = s.cancelGoneSends(ctx, &programID, nil)
	return err
}

// runsWithGroupsOf returns a check for writeInChunks that a program still runs
// with the groups of listed. A change of the groups, a pause, a close or a
// change to a draft stops the writes, the next pass plans from the new state.
// The groups are read again for each chunk, a failed read stops the writes.
func (s *Program) runsWithGroupsOf(ctx context.Context, listed *model.Program) func(*model.Program) bool {
	return func(p *model.Program) bool {
		if !programRunsAt(p, time.Now()) {
			return false
		}
		id := p.ID.MustGet()
		groupIDs, err := s.ProgramRepository.GetRecipientGroupIDs(ctx, &id)
		if err != nil {
			s.Logger.Warnw("failed to read the program groups, stopping the enrollment writes", "programID", id.String(), "error", err)
			return false
		}
		return sameGroupIDs(listed, &model.Program{RecipientGroupIDs: nullable.NewNullableWithValue(groupIDs)})
	}
}

// redrawFirstSends draws the first send again, from now, of the active people
// in existing who were never sent. They are people stored by a first pass that
// stopped before the program was marked started, for example because the
// program was made a draft or paused in between. Their first send was drawn
// then and may be long past, without a new draw all of them would be sent at
// once. The plans are written in chunks while the program runs and is not yet
// marked started. It reports whether every chunk was written.
func (s *Program) redrawFirstSends(
	ctx context.Context,
	program *model.Program,
	existing []*database.ProgramEnrollment,
	running func(*model.Program) bool,
) (bool, error) {
	now := time.Now()
	plans := []repository.EnrollmentPlan{}
	for _, e := range existing {
		if e.Status != model.ProgramEnrollmentActive || e.LastSendAt != nil {
			continue
		}
		due := programPickSendTime(program, now, now, s.rng())
		plans = append(plans, repository.EnrollmentPlan{ID: e.ID, RoundStartAt: now, NextDueAt: due})
	}
	programID := program.ID.MustGet()
	firstPass := func(p *model.Program) bool { return p.StartedAt == nil && running(p) }
	return s.writeInChunks(ctx, &programID, len(plans), firstPass, func(start, end int) error {
		return s.ProgramRepository.PlanEnrollments(ctx, plans[start:end], nil, model.ProgramEnrollmentActive)
	})
}

// markStarted stores when a program was first enrolled, once
func (s *Program) markStarted(ctx context.Context, program *model.Program, now time.Time) {
	if program.StartedAt != nil {
		return
	}
	programID := program.ID.MustGet()
	if err := s.ProgramRepository.SetFields(ctx, &programID, map[string]any{"started_at": now.UTC()}); err != nil {
		s.Logger.Errorw("failed to mark program started", "error", err)
	}
}

// sameGroupIDs reports whether two reads of a program list the same groups
func sameGroupIDs(a *model.Program, b *model.Program) bool {
	ids := func(p *model.Program) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		if !p.RecipientGroupIDs.IsSpecified() || p.RecipientGroupIDs.IsNull() {
			return out
		}
		for _, id := range p.RecipientGroupIDs.MustGet() {
			if id != nil {
				out[*id] = true
			}
		}
		return out
	}
	x, y := ids(a), ids(b)
	if len(x) != len(y) {
		return false
	}
	for id := range x {
		if !y[id] {
			return false
		}
	}
	return true
}

// enrollNowUnlocked enrolls a running program's members right away, for a
// caller that does not hold the lifecycle lock. The new people are stored in
// chunks, each under the lock for its own write only and
// only while the program still runs, so a pause, close or delete in between
// stops the rest. The people who return or left are written the same way
// after them, see resolveListedProgramWith. A failure is logged and the next
// tick enrolls whoever is missing.
func (s *Program) enrollNowUnlocked(ctx context.Context, id *uuid.UUID) error {
	// a client that disconnects must not stop the enrollment half way
	ctx = context.WithoutCancel(ctx)
	err := s.enrollStartedProgram(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to enroll program members, the next tick retries", "programID", id.String(), "error", err)
		_ = s.withLifecycleLock(ctx, func() error {
			s.lastMembershipAt = time.Time{}
			return nil
		})
	}
	return err
}

// enrollInBackground runs enrollNowUnlocked on its own goroutine, so a
// request that starts a large program answers at once. At most one
// enrollment of a program runs at a time, a call while one runs asks it to run
// once more when it ends. A failure is shown on the program, audited at most
// once per programFailureAuditEvery and retried by the next tick, see
// enrollNowUnlocked.
func (s *Program) enrollInBackground(id *uuid.UUID) {
	key := *id
	s.stoppingMu.Lock()
	if s.enrolling == nil {
		s.enrolling = map[uuid.UUID]bool{}
	}
	if _, running := s.enrolling[key]; running {
		s.enrolling[key] = true
		s.stoppingMu.Unlock()
		return
	}
	s.enrolling[key] = false
	s.stoppingMu.Unlock()
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		system, sessionErr := model.NewSystemSession()
		for {
			// a panic ends this run as a failure, the audit and the failure
			// are written the same way
			err := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						s.Logger.Errorw("recovered from panic while enrolling program members",
							"programID", key.String(), "panic", r, "stack", string(debug.Stack()))
						err = errProgramBackgroundPanic
					}
				}()
				return s.enrollNowUnlocked(context.Background(), &key)
			}()
			if sessionErr == nil {
				s.finishAction(key, programActionStart, system, "Program.Start", err, true)
			}
			s.stoppingMu.Lock()
			if !s.enrolling[key] {
				delete(s.enrolling, key)
				s.stoppingMu.Unlock()
				return
			}
			s.enrolling[key] = false
			s.stoppingMu.Unlock()
		}
	}()
}

// programRunsAt reports whether a program sends and enrolls at the given time
func programRunsAt(program *model.Program, now time.Time) bool {
	return program.StatusAt(now) == model.ProgramStatusActive && !program.IsStopping()
}

// enrollStartedProgram is the work of enrollNowUnlocked
func (s *Program) enrollStartedProgram(ctx context.Context, id *uuid.UUID) error {
	now := time.Now()
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return errs.Wrap(err)
	}
	if !programRunsAt(program, now) {
		return nil
	}
	members, err := s.resolveMembership(ctx, program)
	if err != nil {
		return errs.Wrap(err)
	}
	return s.resolveListedProgramWith(ctx, program, members)
}

// writeInChunks writes n items in chunks of repository.ProgramWriteChunk. Each
// chunk takes the lifecycle lock for its own write only, after the program is
// read again, and the rest is not written once allowed reports false or the
// program is gone. Between two chunks it waits after the lock is let go, see
// repository.PauseAfterWrite, so other writers get in. It reports whether every chunk was
// written. A chunk that fails, or a context that ends during the wait, stops
// the rest and returns the error.
func (s *Program) writeInChunks(
	ctx context.Context,
	id *uuid.UUID,
	n int,
	allowed func(*model.Program) bool,
	write func(start, end int) error,
) (bool, error) {
	for start := 0; start < n; start += repository.ProgramWriteChunk {
		end := min(start+repository.ProgramWriteChunk, n)
		stopped := false
		began := time.Now()
		err := s.withLifecycleLock(ctx, func() error {
			program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
			if errors.Is(err, gorm.ErrRecordNotFound) {
				stopped = true
				return nil
			}
			if err != nil {
				return errs.Wrap(err)
			}
			if !allowed(program) {
				stopped = true
				return nil
			}
			return write(start, end)
		})
		if err != nil {
			return false, err
		}
		if stopped {
			return false, nil
		}
		// other writers get the database between two chunks, never while the
		// lock is held
		if end < n {
			if err := repository.PauseAfterWrite(ctx, time.Since(began)); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// programReturners is what a membership pass changes for the members, see
// planReturners. The writes are made after the lifecycle lock is let go, see
// writeReturners.
type programReturners struct {
	// program is the program the plan was made from, with its steps
	program *model.Program
	// joiners are the new enrollments of the members who have none
	joiners []*database.ProgramEnrollment
	// returning are the plans of the exited or stopped people who came back
	returning []repository.EnrollmentPlan
	// back are the active and completed people back within the grace period,
	// their missing mark is cleared
	back []*uuid.UUID
	// backCompleted are the completed people of back, they are made active
	// again when a step of their loop is not delivered
	backCompleted []*database.ProgramEnrollment
}

// planReturners finds the members who are back in the groups. The missing
// mark of the people back within the grace period is cleared, and the people
// who return after they exited go on. Members with no enrollment are left
// out. It writes nothing. program must hold its steps.
func (s *Program) planReturners(
	program *model.Program,
	members []*uuid.UUID,
	existing []*database.ProgramEnrollment,
	now time.Time,
) programReturners {
	existingByRecipient := enrollmentsByRecipient(existing)
	out := programReturners{program: program}
	for _, member := range members {
		e, ok := existingByRecipient[*member]
		if !ok {
			continue
		}
		// back in the groups: the missing mark is cleared, nothing else changes
		if (e.Status == model.ProgramEnrollmentActive || e.Status == model.ProgramEnrollmentCompleted) && e.MissingSince != nil {
			out.back = append(out.back, e.ID)
		}
		if e.Status == model.ProgramEnrollmentCompleted && e.MissingSince != nil {
			out.backCompleted = append(out.backCompleted, e)
		}
		// a returning member keeps their loop, history and round. A stopped
		// person is only found here when a close stopped half way and the
		// program was resumed, they go on the same way
		if e.Status == model.ProgramEnrollmentExited || e.Status == model.ProgramEnrollmentStopped {
			out.returning = append(out.returning, s.returnPlan(program, e, now))
		}
	}
	return out
}

// writeReturners writes planReturners and the new people in chunks while
// allowed reports true, see writeInChunks. The caller must not hold the
// lifecycle lock. Each write changes a person only in the status it was
// planned from, so a plan made from an older read is safe. The completed
// people back in the groups are made active again when a step of their loop
// is not delivered, for example because their last send was cancelled while
// they were gone. A send still waiting in an open campaign counts as
// delivered, so a person back within the grace period stays completed. The
// missing mark is cleared last, so a person whose reactivation was not
// written keeps it and the next pass finds them again. It reports whether
// every chunk was written.
func (s *Program) writeReturners(
	ctx context.Context,
	r programReturners,
	allowed func(*model.Program) bool,
) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	programID := r.program.ID.MustGet()
	stats := programTickStatsFrom(ctx)
	done, err := s.writeInChunks(ctx, &programID, len(r.joiners), allowed, func(start, end int) error {
		if err := s.ProgramRepository.InsertEnrollments(ctx, r.joiners[start:end]); err != nil {
			return err
		}
		stats.add(&programID, 0, 0, end-start, 0)
		return nil
	})
	if err != nil || !done {
		return done, err
	}
	done, err = s.writeInChunks(ctx, &programID, len(r.returning), allowed, func(start, end int) error {
		if err := s.ProgramRepository.PlanEnrollments(ctx, r.returning[start:end], map[string]any{
			"status":        model.ProgramEnrollmentActive,
			"exited_at":     nil,
			"missing_since": nil,
		}, model.ProgramEnrollmentExited, model.ProgramEnrollmentStopped); err != nil {
			return err
		}
		stats.add(&programID, 0, 0, end-start, 0)
		return nil
	})
	if err != nil || !done {
		return done, err
	}
	reactivated := map[uuid.UUID]bool{}
	if len(r.backCompleted) > 0 {
		left, err := s.completedWithStepsLeft(ctx, r.program, r.backCompleted)
		if err != nil {
			return false, err
		}
		plans := make([]repository.EnrollmentPlan, 0, len(left))
		for _, e := range left {
			plans = append(plans, s.returnPlan(r.program, e, time.Now()))
			reactivated[*e.ID] = true
		}
		done, err = s.writeInChunks(ctx, &programID, len(plans), allowed, func(start, end int) error {
			return s.ProgramRepository.PlanEnrollments(ctx, plans[start:end], map[string]any{
				"status":           model.ProgramEnrollmentActive,
				"completed_at":     nil,
				"missing_since":    nil,
				"advance_failures": 0,
			}, model.ProgramEnrollmentCompleted)
		})
		if err != nil || !done {
			return done, err
		}
	}
	// the people made active above had their mark cleared by that write
	cleared := make([]*uuid.UUID, 0, len(r.back))
	for _, id := range r.back {
		if !reactivated[*id] {
			cleared = append(cleared, id)
		}
	}
	return s.writeInChunks(ctx, &programID, len(cleared), allowed, func(start, end int) error {
		return s.ProgramRepository.UpdateEnrollmentsFields(ctx, cleared[start:end], map[string]any{
			"missing_since": nil,
		}, model.ProgramEnrollmentActive, model.ProgramEnrollmentCompleted)
	})
}

// enrollmentsByRecipient indexes enrollments by their person
func enrollmentsByRecipient(existing []*database.ProgramEnrollment) map[uuid.UUID]*database.ProgramEnrollment {
	out := make(map[uuid.UUID]*database.ProgramEnrollment, len(existing))
	for _, e := range existing {
		out[*e.RecipientID] = e
	}
	return out
}

// planJoiners returns a new enrollment, with its first send drawn, for every
// member who has none
func (s *Program) planJoiners(
	program *model.Program,
	members []*uuid.UUID,
	existingByRecipient map[uuid.UUID]*database.ProgramEnrollment,
	now time.Time,
) []*database.ProgramEnrollment {
	programID := program.ID.MustGet()
	// members found on the first pass are the starting audience, anyone who
	// joins later waits the new member delay before their first send
	delay := time.Duration(0)
	if v, err := program.NewMemberDelayMinutes.Get(); err == nil && program.StartedAt != nil {
		delay = time.Duration(v) * time.Minute
	}
	joiners := []*database.ProgramEnrollment{}
	for _, member := range members {
		rid := *member
		if _, ok := existingByRecipient[rid]; ok {
			continue
		}
		roundStart := now.Add(delay)
		due := programPickSendTime(program, roundStart, now, s.rng())
		e := &database.ProgramEnrollment{
			ProgramID:    &programID,
			RecipientID:  &rid,
			RoundStartAt: &roundStart,
			NextDueAt:    &due,
			Status:       model.ProgramEnrollmentActive,
		}
		// a person of an anonymous program gets the pseudonym of their sends
		// now, so the scheduler does not write it one person at a time. It is
		// random and kept only on the enrollment, which is cleared on close, as
		// a pseudonym made at the first send is
		if program.IsAnonymousOn() {
			pseudonym := uuid.New()
			e.AnonymizedID = &pseudonym
		}
		joiners = append(joiners, e)
	}
	return joiners
}

// programLeavers is what a membership pass changes for the people no longer
// in the groups, see planLeavers
type programLeavers struct {
	// missing are the enrollments to mark missing
	missing []*uuid.UUID
	// exit are the active enrollments to exit
	exit []*uuid.UUID
	// gone are the people whose unsent sends are to be cancelled: the people
	// exited and the completed people missing for longer than the grace
	gone []*uuid.UUID
}

// planLeavers finds the active and completed enrollments no longer in
// membership. They are marked missing first. An active person is exited only
// after the grace period, so a short group glitch does not cancel their sends.
// A completed person keeps their status, after the grace period their unsent
// send is cancelled.
func planLeavers(members []*uuid.UUID, existing []*database.ProgramEnrollment, now time.Time) programLeavers {
	memberSet := memberIDSet(members)
	out := programLeavers{}
	for _, e := range existing {
		if e.Status != model.ProgramEnrollmentActive && e.Status != model.ProgramEnrollmentCompleted {
			continue
		}
		if memberSet[*e.RecipientID] {
			continue
		}
		if e.MissingSince == nil {
			out.missing = append(out.missing, e.ID)
			continue
		}
		if now.Sub(*e.MissingSince) < programMissingGrace {
			continue
		}
		if e.Status == model.ProgramEnrollmentActive {
			out.exit = append(out.exit, e.ID)
		}
		out.gone = append(out.gone, e.RecipientID)
	}
	return out
}

// programExitFields are the columns an exit writes
func programExitFields(now time.Time) map[string]any {
	return map[string]any{
		"status":        model.ProgramEnrollmentExited,
		"exited_at":     now.UTC(),
		"next_due_at":   nil,
		"missing_since": nil,
	}
}

// writeLeavers writes planLeavers in chunks, for a caller that does not hold
// the lifecycle lock. Each chunk takes the lock for its own write only and
// only while allowed reports true, so a pause, close or edit waits for one
// chunk only. The status of each person is checked by the write itself. The
// sends are not cancelled here. It reports whether every chunk was written.
func (s *Program) writeLeavers(
	ctx context.Context,
	programID *uuid.UUID,
	leavers programLeavers,
	now time.Time,
	allowed func(*model.Program) bool,
) (bool, error) {
	done, err := s.writeInChunks(ctx, programID, len(leavers.missing), allowed, func(start, end int) error {
		return s.ProgramRepository.UpdateEnrollmentsFields(ctx, leavers.missing[start:end], map[string]any{
			"missing_since": now.UTC(),
		}, model.ProgramEnrollmentActive, model.ProgramEnrollmentCompleted)
	})
	if err != nil || !done {
		return done, err
	}
	return s.writeInChunks(ctx, programID, len(leavers.exit), allowed, func(start, end int) error {
		if err := s.ProgramRepository.UpdateEnrollmentsFields(ctx, leavers.exit[start:end], programExitFields(now), model.ProgramEnrollmentActive); err != nil {
			return err
		}
		programTickStatsFrom(ctx).add(programID, 0, 0, 0, end-start)
		return nil
	})
}

// programGoneCancelLimit is how many people who left a program have their
// unsent sends cancelled by one membership pass or resume. A longer backlog is
// cancelled over the next passes.
const programGoneCancelLimit = 5000

// cancelGoneSends cancels the unsent sends of the people who left a program,
// exited or completed and missing for longer than the grace period, up to
// programGoneCancelLimit people, in chunks while allowed reports true, see
// cancelPeopleSends. A nil allowed lets every chunk be written. Every
// membership pass and resume runs it, so a cancel that failed or was cut short
// before is done again. It reports whether every chunk was written. A request
// that ends does not stop it. The caller must not hold the lifecycle lock.
func (s *Program) cancelGoneSends(ctx context.Context, programID *uuid.UUID, allowed func(*model.Program) bool) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	ids, err := s.ProgramRepository.GetGoneWithUnsentSends(ctx, programID, time.Now().Add(-programMissingGrace), programGoneCancelLimit)
	if err != nil {
		return false, errs.Wrap(err)
	}
	return s.cancelPeopleSends(ctx, programID, ids, true, allowed)
}

// cancelPeopleSends cancels the not yet sent sends of the given people of a
// program, for a caller that does not hold the lifecycle lock. The people are
// written in chunks, each under the lock for its own write only and only
// while allowed reports true for the program read under that lock, so a
// pause, close or edit waits for one chunk only. A nil allowed lets every
// chunk be written: the sends are then cancelled also when the program is
// paused or being closed, as the people are no longer in its groups. With
// onlyGone a chunk cancels only the people who still left the program when it
// is written, exited or completed and missing for longer than the grace
// period, so a person who came back in between keeps their sends. A program
// that is gone stops the rest. It reports whether every chunk was written. A
// request that ends does not stop it, a person left with a send would get it
// once the program sends again.
func (s *Program) cancelPeopleSends(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	onlyGone bool,
	allowed func(*model.Program) bool,
) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	if allowed == nil {
		allowed = func(*model.Program) bool { return true }
	}
	return s.writeInChunks(ctx, programID, len(recipientIDs), allowed,
		func(start, end int) error {
			ids := recipientIDs[start:end]
			if onlyGone {
				var err error
				ids, err = s.ProgramRepository.GetGoneRecipients(ctx, programID, ids, time.Now().Add(-programMissingGrace))
				if err != nil {
					return errs.Wrap(err)
				}
			}
			return s.ProgramRepository.CancelUnsentForProgramRecipients(ctx, programID, ids)
		})
}

// memberIDSet returns the member ids as a set
func memberIDSet(members []*uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(members))
	for _, id := range members {
		out[*id] = true
	}
	return out
}

// resolveMembership returns the deduped ids of the SCIM active recipients of a
// program's assigned groups. It mirrors the campaign schedule resolution. Only
// ids are read, so a group of any size costs one light query.
func (s *Program) resolveMembership(
	ctx context.Context,
	program *model.Program,
) ([]*uuid.UUID, error) {
	members := []*uuid.UUID{}
	if !program.RecipientGroupIDs.IsSpecified() || program.RecipientGroupIDs.IsNull() {
		return members, nil
	}
	seen := map[uuid.UUID]bool{}
	for _, groupID := range program.RecipientGroupIDs.MustGet() {
		ids, err := s.ProgramRepository.GetGroupMemberIDs(ctx, groupID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, id := range ids {
			if seen[*id] {
				continue
			}
			seen[*id] = true
			members = append(members, id)
		}
	}
	return members, nil
}

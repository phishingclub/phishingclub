package service

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// programMembershipInterval throttles the enrollment resolution phase so the
// group membership diff does not run on every ten second tick.
const programMembershipInterval = 60 * time.Second

// programAdvanceLimit bounds how many due enrollments one tick advances, so a
// large program drains over several ticks like the send batch does.
const programAdvanceLimit = 1000

// programMissingGrace is how long a person may be missing from the program
// groups before the enrollment is exited, so a short group glitch, like a SCIM
// sync that briefly empties a group, does not cancel their sends
const programMissingGrace = time.Hour

// programFailureBackoff is how long a person waits after the first failed
// attempt to advance them. Each failure in a row doubles it, up to
// programFailureBackoffMax, so one broken person never blocks the due scan.
const programFailureBackoff = time.Hour

// programFailureBackoffMax is the longest wait after failed attempts
const programFailureBackoffMax = 24 * time.Hour

// programRetentionInterval throttles the removal of old identity links
const programRetentionInterval = 10 * time.Minute

// programIdentityRetention is how long a send of an anonymous program keeps
// its link to the person. After that only the pseudonym is left.
const programIdentityRetention = 14 * 24 * time.Hour

// programSent is the send just planned for a person
type programSent struct {
	at     time.Time
	stepID uuid.UUID
}

// templateUsable reports whether a template can be sent. The answer is kept in
// cache, when given, for the rest of the tick. A missing template is unusable.
func (s *Program) templateUsable(ctx context.Context, templateID uuid.UUID, cache map[uuid.UUID]bool) (bool, error) {
	if cache != nil {
		if v, ok := cache[templateID]; ok {
			return v, nil
		}
	}
	usable, err := s.ProgramRepository.GetTemplateUsability(ctx, []uuid.UUID{templateID})
	if err != nil {
		return false, errs.Wrap(err)
	}
	v := usable[templateID]
	if cache != nil {
		cache[templateID] = v
	}
	return v, nil
}

// pickSendableStep picks a step from the candidates whose template can be sent
// and whose step campaign is open. A step with a broken template is skipped for
// this pick only, it stays undelivered. blocked reports whether such a step was
// skipped.
func (s *Program) pickSendableStep(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	candidates []*model.ProgramStep,
	loopCount int,
	rng *rand.Rand,
	campaignCache map[string]*model.Campaign,
	usableCache map[uuid.UUID]bool,
) (*model.ProgramStep, *model.Campaign, bool, error) {
	blocked := false
	for len(candidates) > 0 {
		pick := programPickStep(program, candidates, rng)
		usable, err := s.templateUsable(ctx, pick.CampaignTemplateID.MustGet(), usableCache)
		if err != nil {
			return nil, nil, blocked, err
		}
		if !usable {
			blocked = true
			candidates = removeStep(candidates, pick)
			continue
		}
		c, err := s.getOrCreateStepCampaign(ctx, session, program, pick, loopCount, campaignCache)
		if err != nil {
			return nil, nil, blocked, errs.Wrap(err)
		}
		if c.IsActive() {
			return pick, c, blocked, nil
		}
		s.Logger.Warnw("program step campaign is closed, skipping the step",
			"programID", program.ID.MustGet().String(),
			"stepID", pick.ID.MustGet().String(),
		)
		candidates = removeStep(candidates, pick)
	}
	return nil, nil, blocked, nil
}

// postponeEnrollment moves a person's next send one day later, keeping their
// round and loop, used when none of their next steps can be sent now
func (s *Program) postponeEnrollment(
	ctx context.Context,
	program *model.Program,
	e *database.ProgramEnrollment,
	now time.Time,
) error {
	// one line per person per day, at debug level, as the program page already
	// warns about a template that cannot be sent
	s.Logger.Debugw("no program step can be sent to the person now, trying again in a day",
		"programID", program.ID.MustGet().String(),
		"enrollmentID", e.ID.String(),
	)
	due := programNextWindowSlot(program, now.Add(programPostponeDelay))
	return s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
		"next_due_at": due,
	})
}

// returnEnrollment activates the enrollment of a person who left and came back.
// They keep their loop, history and round, unless the round is over, then a new
// round starts now.
func (s *Program) returnEnrollment(
	ctx context.Context,
	program *model.Program,
	e *database.ProgramEnrollment,
	now time.Time,
) error {
	roundStart := now
	if e.RoundStartAt != nil && !e.RoundStartAt.Add(program.SpreadOrDefault()).Before(now) {
		roundStart = *e.RoundStartAt
	}
	due := programPickSendTimeAfter(program, roundStart, now, programMinNextSend(program, e.LastSendAt), s.rng())
	return s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
		"status":         model.ProgramEnrollmentActive,
		"exited_at":      nil,
		"missing_since":  nil,
		"round_start_at": roundStart,
		"next_due_at":    due,
	})
}

// removeOldIdentityLinks removes the person link of every send of an anonymous
// program sent longer ago than the retention, keeping only the pseudonym. The
// enrollment still knows the pseudonym, so delivery history keeps working.
func (s *Program) removeOldIdentityLinks(ctx context.Context, now time.Time) {
	programIDs, err := s.ProgramRepository.GetAnonymousProgramIDs(ctx)
	if err != nil {
		s.Logger.Errorw("failed to list anonymous programs", "error", err)
		return
	}
	before := now.Add(-programIdentityRetention)
	for _, id := range programIDs {
		n, err := s.ProgramRepository.RemoveIdentityOfSendsBefore(ctx, id, before)
		if err != nil {
			s.Logger.Errorw("failed to remove old identity links", "programID", id.String(), "error", err)
			continue
		}
		if n > 0 {
			s.Logger.Debugw("removed old identity links", "programID", id.String(), "rows", n)
		}
	}
}

// openStepCampaign returns the newest campaign linked to a step and loop that
// is not closed, nil when there is none
func (s *Program) openStepCampaign(
	ctx context.Context,
	programID *uuid.UUID,
	stepID *uuid.UUID,
	loopIndex int,
) (*model.Campaign, error) {
	links, err := s.ProgramRepository.GetProgramCampaigns(ctx, programID, stepID, loopIndex)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, link := range links {
		c, err := s.CampaignRepository.GetByID(ctx, link.CampaignID, &repository.CampaignOption{
			IncludeProgramManaged: true,
		})
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if c.IsActive() {
			return c, nil
		}
	}
	return nil, nil
}

// campaignNameSuffix returns eight random hex characters that keep the name of
// a hidden campaign unique
func campaignNameSuffix() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
}

// Tick is the scheduler step, run by the task runner. It resolves enrollments
// from group membership at a throttled cadence, then advances every due
// recipient by scheduling their next step into a hidden campaign.
func (s *Program) Tick(ctx context.Context, session *model.Session) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		return errs.ErrAuthorizationFailed
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	now := time.Now()
	s.endProgramsDue(ctx, session, now)
	if now.Sub(s.lastMembershipAt) >= programMembershipInterval {
		s.lastMembershipAt = now
		if err := s.resolveEnrollments(ctx); err != nil {
			s.Logger.Errorw("failed to resolve program enrollments", "error", err)
		}
	}
	if now.Sub(s.lastRetentionAt) >= programRetentionInterval {
		s.lastRetentionAt = now
		s.removeOldIdentityLinks(ctx, now)
	}
	return s.advanceDue(ctx, session, now)
}

// endProgramsDue closes every program whose end date has passed, the same way
// an operator closes it. The caller holds the lifecycle lock.
func (s *Program) endProgramsDue(ctx context.Context, session *model.Session, now time.Time) {
	ids, err := s.ProgramRepository.GetIDsDueToEnd(ctx, now)
	if err != nil {
		s.Logger.Errorw("failed to list programs due to end", "error", err)
		return
	}
	for _, id := range ids {
		ae := NewAuditEvent("Program.Close", session)
		ae.Details["id"] = id.String()
		ae.Details["reason"] = "end date reached"
		// a draft has no start, it is closed as it is and never sends
		if program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{}); err == nil &&
			program.StatusAt(now) == model.ProgramStatusDraft {
			ae.Details["draft"] = true
			s.Logger.Infow("draft program passed its end date, closing it", "programID", id.String())
		}
		if err := s.closeProgram(ctx, session, id, "program end date reached"); err != nil {
			s.Logger.Errorw("failed to close program at its end date", "programID", id.String(), "error", err)
			// the close is tried again on every tick, the failure is audited at
			// most once per programCloseFailureAuditEvery
			if last, ok := s.closeFailureAuditAt[*id]; !ok || now.Sub(last) >= programCloseFailureAuditEvery {
				if s.closeFailureAuditAt == nil {
					s.closeFailureAuditAt = map[uuid.UUID]time.Time{}
				}
				s.closeFailureAuditAt[*id] = now
				ae.Details["result"] = "failed"
				s.AuditLogAuthorized(ae)
			}
			continue
		}
		delete(s.closeFailureAuditAt, *id)
		s.Logger.Infow("program closed at its end date", "programID", id.String())
		s.AuditLogAuthorized(ae)
	}
}

// programCloseFailureAuditEvery is how often a failing end date close of one
// program is written to the audit log
const programCloseFailureAuditEvery = time.Hour

// resolveEnrollments diffs each active program's live group membership against
// its enrollments, enrolling joiners and exiting leavers.
func (s *Program) resolveEnrollments(ctx context.Context) error {
	now := time.Now()
	programs, err := s.ProgramRepository.GetActivePrograms(ctx, now)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, program := range programs {
		programID := program.ID.MustGet()
		members, err := s.resolveMembership(ctx, program)
		if err != nil {
			s.Logger.Errorw("failed to resolve membership", "programID", programID.String(), "error", err)
			continue
		}
		existing, err := s.ProgramRepository.GetEnrollmentsByProgramID(ctx, &programID)
		if err != nil {
			s.Logger.Errorw("failed to get enrollments", "programID", programID.String(), "error", err)
			continue
		}
		s.enrollJoiners(ctx, program, members, existing, now)
		if program.StartedAt == nil {
			if err := s.ProgramRepository.SetFields(ctx, &programID, map[string]any{"started_at": now}); err != nil {
				s.Logger.Errorw("failed to mark program started", "error", err)
			}
		}
		s.exitLeavers(ctx, &programID, members, existing, now)
	}
	return nil
}

// enrollJoiners enrolls members with no enrollment, clears the missing mark of
// members back within the grace period and reactivates members who return
// after they exited.
func (s *Program) enrollJoiners(
	ctx context.Context,
	program *model.Program,
	members []*model.Recipient,
	existing []*database.ProgramEnrollment,
	now time.Time,
) {
	programID := program.ID.MustGet()
	existingByRecipient := map[uuid.UUID]*database.ProgramEnrollment{}
	for _, e := range existing {
		existingByRecipient[*e.RecipientID] = e
	}
	// members found on the first pass are the starting audience, anyone who
	// joins later waits the new member delay before their first send
	delay := time.Duration(0)
	if v, err := program.NewMemberDelayMinutes.Get(); err == nil && program.StartedAt != nil {
		delay = time.Duration(v) * time.Minute
	}
	joiners := []*database.ProgramEnrollment{}
	backIDs := []*uuid.UUID{}
	for _, r := range members {
		rid := r.ID.MustGet()
		e, ok := existingByRecipient[rid]
		if !ok {
			roundStart := now.Add(delay)
			due := programPickSendTime(program, roundStart, now, s.rng())
			joiners = append(joiners, &database.ProgramEnrollment{
				ProgramID:    &programID,
				RecipientID:  &rid,
				RoundStartAt: &roundStart,
				NextDueAt:    &due,
				Status:       model.ProgramEnrollmentActive,
			})
			continue
		}
		// back within the grace period: nothing changes
		if e.Status == model.ProgramEnrollmentActive && e.MissingSince != nil {
			backIDs = append(backIDs, e.ID)
		}
		// a returning member keeps their loop, history and round. A stopped
		// person is only found here when a close stopped half way and the
		// program was resumed, they go on the same way
		if e.Status == model.ProgramEnrollmentExited || e.Status == model.ProgramEnrollmentStopped {
			if err := s.returnEnrollment(ctx, program, e, now); err != nil {
				s.Logger.Errorw("failed to reactivate enrollment", "error", err)
			}
		}
	}
	if err := s.ProgramRepository.InsertEnrollments(ctx, joiners); err != nil {
		s.Logger.Errorw("failed to insert enrollments", "error", err)
	}
	if err := s.ProgramRepository.UpdateEnrollmentsFields(ctx, backIDs, map[string]any{
		"missing_since": nil,
	}); err != nil {
		s.Logger.Errorw("failed to clear missing enrollments", "error", err)
	}
}

// exitLeavers handles active enrollments no longer in membership. They are
// marked missing first and exited only after the grace period, so a short
// group glitch does not cancel their sends.
func (s *Program) exitLeavers(
	ctx context.Context,
	programID *uuid.UUID,
	members []*model.Recipient,
	existing []*database.ProgramEnrollment,
	now time.Time,
) {
	memberSet := map[uuid.UUID]bool{}
	for _, r := range members {
		memberSet[r.ID.MustGet()] = true
	}
	missingIDs := []*uuid.UUID{}
	exitIDs := []*uuid.UUID{}
	leaverIDs := []*uuid.UUID{}
	for _, e := range existing {
		if e.Status != model.ProgramEnrollmentActive {
			continue
		}
		if memberSet[*e.RecipientID] {
			continue
		}
		if e.MissingSince == nil {
			missingIDs = append(missingIDs, e.ID)
			continue
		}
		if now.Sub(*e.MissingSince) < programMissingGrace {
			continue
		}
		exitIDs = append(exitIDs, e.ID)
		leaverIDs = append(leaverIDs, e.RecipientID)
	}
	if err := s.ProgramRepository.UpdateEnrollmentsFields(ctx, missingIDs, map[string]any{
		"missing_since": now,
	}); err != nil {
		s.Logger.Errorw("failed to mark enrollments missing", "error", err)
	}
	if err := s.ProgramRepository.UpdateEnrollmentsFields(ctx, exitIDs, map[string]any{
		"status":        model.ProgramEnrollmentExited,
		"exited_at":     now,
		"next_due_at":   nil,
		"missing_since": nil,
	}); err != nil {
		s.Logger.Errorw("failed to exit enrollments", "error", err)
		leaverIDs = nil
	}
	// stop any not yet sent step from going out after they left, in one batch
	if len(leaverIDs) > 0 {
		if err := s.ProgramRepository.CancelUnsentForProgramRecipients(ctx, programID, leaverIDs); err != nil {
			s.Logger.Errorw("failed to cancel unsent step sends", "error", err)
		}
	}
}

// resolveMembership returns the deduped, SCIM active recipients of a program's
// assigned groups. It mirrors the campaign schedule resolution.
func (s *Program) resolveMembership(
	ctx context.Context,
	program *model.Program,
) ([]*model.Recipient, error) {
	recipients := []*model.Recipient{}
	dubMap := map[string]bool{}
	if !program.RecipientGroupIDs.IsSpecified() || program.RecipientGroupIDs.IsNull() {
		return recipients, nil
	}
	for _, groupID := range program.RecipientGroupIDs.MustGet() {
		// one load: a static group brings its junction recipients, a dynamic group
		// brings none here and is resolved by its filter below
		group, err := s.RecipientGroupRepository.GetByID(
			ctx,
			groupID,
			&repository.RecipientGroupOption{WithRecipients: true},
		)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		var recps []*model.Recipient
		isDynamic := group.IsDynamic.IsSpecified() && !group.IsDynamic.IsNull() && group.IsDynamic.MustGet()
		if isDynamic {
			result, err := s.RecipientGroupRepository.GetRecipientsByGroupID(
				ctx,
				groupID,
				&repository.RecipientOption{ExcludeSoftDeleted: true},
			)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			recps = result.Rows
		} else {
			recps = group.Recipients
		}
		for _, recp := range recps {
			if recp.ScimSoftDeletedAt != nil {
				continue
			}
			id := recp.ID.MustGet().String()
			if dubMap[id] {
				continue
			}
			dubMap[id] = true
			recipients = append(recipients, recp)
		}
	}
	return recipients, nil
}

// advanceDue schedules the next step for every enrollment that is due.
func (s *Program) advanceDue(ctx context.Context, session *model.Session, now time.Time) error {
	due, err := s.ProgramRepository.GetDueEnrollments(ctx, now, programAdvanceLimit)
	if err != nil {
		return errs.Wrap(err)
	}
	programCache := map[string]*model.Program{}
	// a fresh wave has most recipients on the same (step, loop), so their hidden
	// campaign is the same row, so it is cached for this tick and not read again
	// for each due recipient
	campaignCache := map[string]*model.Campaign{}
	// whether a template can be sent is read once per tick
	usableCache := map[uuid.UUID]bool{}
	for _, e := range due {
		programID := e.ProgramID
		program, ok := programCache[programID.String()]
		if !ok {
			p, err := s.ProgramRepository.GetByID(ctx, programID, &repository.ProgramOption{WithSteps: true})
			if err != nil {
				s.Logger.Errorw("failed to load program for advancement", "error", err)
				continue
			}
			programCache[programID.String()] = p
			program = p
		}
		// a paused, closed or not yet started program should not advance
		if program.StatusAt(now) != model.ProgramStatusActive {
			continue
		}
		if err := s.advanceEnrollment(ctx, session, program, e, now, campaignCache, usableCache); err != nil {
			s.delayFailedEnrollment(ctx, e, now, err)
			continue
		}
		if e.AdvanceFailures > 0 {
			if err := s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
				"advance_failures": 0,
			}); err != nil {
				s.Logger.Errorw("failed to reset enrollment failures", "error", err)
			}
		}
	}
	return nil
}

// programBackoff returns how long to wait after the given number of failed
// attempts in a row
func programBackoff(failures int) time.Duration {
	delay := programFailureBackoff
	for i := 1; i < failures && delay < programFailureBackoffMax; i++ {
		delay *= 2
	}
	return min(delay, programFailureBackoffMax)
}

// delayFailedEnrollment moves the next attempt for a person whose advance
// failed further out each time it fails, so the person is not retried on every
// tick and does not hold the places of others in the due scan
func (s *Program) delayFailedEnrollment(
	ctx context.Context,
	e *database.ProgramEnrollment,
	now time.Time,
	cause error,
) {
	failures := e.AdvanceFailures + 1
	retryAt := now.Add(programBackoff(failures))
	s.Logger.Errorw("failed to advance enrollment, trying again later",
		"error", cause,
		"programID", e.ProgramID.String(),
		"enrollmentID", e.ID.String(),
		"failures", failures,
		"retryAt", retryAt,
	)
	if err := s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
		"advance_failures": failures,
		"next_due_at":      retryAt,
	}); err != nil {
		s.Logger.Errorw("failed to delay failed enrollment", "error", err)
	}
}

// advanceEnrollment sends one recipient their next step and plans their next
// round. The step is one they have not had in the current loop, so an edit to
// the step list never repeats or skips a template for anyone. A step whose
// template cannot be sent is skipped for now and stays undelivered.
func (s *Program) advanceEnrollment(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	e *database.ProgramEnrollment,
	now time.Time,
	campaignCache map[string]*model.Campaign,
	usableCache map[uuid.UUID]bool,
) error {
	steps := program.Steps
	if len(steps) == 0 {
		return s.completeEnrollment(ctx, e, now)
	}
	deliveries, err := s.ProgramRepository.GetLoopDeliveries(ctx, e.ProgramID, e.RecipientID, e.AnonymizedID, e.LoopCount)
	if err != nil {
		return errs.Wrap(err)
	}
	delivered := deliveredSteps(steps, deliveries)
	loopCount := e.LoopCount
	candidates := undeliveredSteps(steps, delivered)
	sentBefore := len(steps) - len(candidates)
	if len(candidates) == 0 {
		if program.LoopModeOrDefault() != model.ProgramLoopLoop {
			return s.completeEnrollment(ctx, e, now)
		}
		// every step was given, start the next loop
		loopCount++
		sentBefore = 0
		candidates = steps
	}
	// the first pick of a loop avoids the template the person had last
	preferred, fallback := candidates, []*model.ProgramStep(nil)
	if sentBefore == 0 {
		preferred, fallback = programLoopOpeners(program, candidates, e.LastStepID)
	}
	rng := s.rng()
	step, campaign, blocked, err := s.pickSendableStep(ctx, session, program, preferred, loopCount, rng, campaignCache, usableCache)
	if err != nil {
		return err
	}
	if step == nil && len(fallback) > 0 {
		var fallbackBlocked bool
		step, campaign, fallbackBlocked, err = s.pickSendableStep(ctx, session, program, fallback, loopCount, rng, campaignCache, usableCache)
		if err != nil {
			return err
		}
		blocked = blocked || fallbackBlocked
	}
	roundStart := now
	if e.RoundStartAt != nil {
		roundStart = *e.RoundStartAt
	}
	if step == nil {
		// steps are left but their templates cannot be sent now, so the person
		// waits a day and keeps their round
		if blocked {
			return s.postponeEnrollment(ctx, program, e, now)
		}
		// every remaining step is closed and can never be sent in this loop, so
		// the loop is over for this recipient
		if program.LoopModeOrDefault() != model.ProgramLoopLoop {
			return s.completeEnrollment(ctx, e, now)
		}
		return s.planNextRound(ctx, program, e, roundStart, now, 0, loopCount+1, nil)
	}
	campaignID := campaign.ID.MustGet()
	recipientID := *e.RecipientID
	// the due time was drawn inside the window, a late tick snaps to the next opening
	sendAt := programNextWindowSlot(program, now)
	// nothing is sent at or after the end, the person waits for the program to
	// close then
	if end := program.EndAtTime(); end != nil && !sendAt.Before(*end) {
		return s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
			"next_due_at": *end,
		})
	}
	existing, err := s.ProgramRepository.GetPersonSend(ctx, &campaignID, &recipientID, e.AnonymizedID)
	if err != nil {
		return errs.Wrap(err)
	}
	if existing != nil && (existing.CancelledAt == nil || existing.SentAt != nil) {
		// already given in this campaign
		return s.planNextRound(ctx, program, e, roundStart, now, sentBefore+1, loopCount, nil)
	}
	// the send and the stored progress are two writes. When the progress was not
	// stored after a send, the person is due again with a stale last send, so the
	// newest send is checked before another one is added
	newest, err := s.ProgramRepository.GetNewestScheduledSend(ctx, e.ProgramID, e.RecipientID, e.AnonymizedID)
	if err != nil {
		return errs.Wrap(err)
	}
	if newest != nil && programMinNextSend(program, &newest.SendAt).After(sendAt) {
		s.Logger.Warnw("program person had a send too recently, planning again",
			"programID", e.ProgramID.String(),
			"enrollmentID", e.ID.String(),
		)
		return s.planNextRound(ctx, program, e, roundStart, now, sentBefore, loopCount,
			&programSent{at: newest.SendAt, stepID: newest.StepID})
	}
	switch {
	case existing != nil:
		// a cancelled send, for example of a person who left and came back, is
		// put back in the queue, as a campaign holds one row per person
		if err := s.ProgramRepository.ReviveCancelledSend(ctx, &existing.ID, sendAt); err != nil {
			return errs.Wrap(err)
		}
	default:
		if err := s.insertStepSend(ctx, campaign, e, recipientID, sendAt); err != nil {
			return err
		}
	}
	sent := &programSent{at: sendAt, stepID: step.ID.MustGet()}
	sentCount := sentBefore + 1
	// the last step of a program that does not loop completes the recipient
	if sentCount >= len(steps) && program.LoopModeOrDefault() != model.ProgramLoopLoop {
		return s.completeEnrollment(ctx, e, now)
	}
	return s.planNextRound(ctx, program, e, roundStart, now, sentCount, loopCount, sent)
}

// insertStepSend adds a scheduled send of a hidden campaign for a person. The
// enrollment may be nil only on a campaign that is not anonymous.
func (s *Program) insertStepSend(
	ctx context.Context,
	campaign *model.Campaign,
	e *database.ProgramEnrollment,
	recipientID uuid.UUID,
	sendAt time.Time,
) error {
	campaignID := campaign.ID.MustGet()
	if campaignAnonymous(campaign) && e == nil {
		return errs.Wrap(errors.New("an anonymous program send needs an enrollment"))
	}
	scheduledEvent := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED]
	campaignRecipient := &model.CampaignRecipient{
		RecipientID:    nullable.NewNullableWithValue(recipientID),
		CampaignID:     nullable.NewNullableWithValue(campaignID),
		SendAt:         nullable.NewNullableWithValue(sendAt),
		NotableEventID: nullable.NewNullableWithValue(*scheduledEvent),
	}
	if campaignAnonymous(campaign) {
		recipient, err := s.RecipientRepository.GetByID(ctx, &recipientID, &repository.RecipientOption{})
		if err == nil {
			s.CampaignService.applyAnonymousSnapshot(campaign, campaignRecipient, recipient)
		}
		pseudonym, err := s.enrollmentPseudonym(ctx, e)
		if err != nil {
			return errs.Wrap(err)
		}
		campaignRecipient.AnonymizedID = nullable.NewNullableWithValue(pseudonym)
	}
	allocator, err := s.CampaignService.newLureCodeAllocator(ctx, campaign, 1)
	if err != nil {
		return errs.Wrap(err)
	}
	if err := allocator.applyTo(campaignRecipient); err != nil {
		return errs.Wrap(err)
	}
	if err := s.CampaignService.insertScheduledRecipient(ctx, allocator, campaignRecipient); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// planNextRound stores the recipient's progress and draws the send time of their
// next round. The next send is never earlier than half an interval after the
// last one. sent is the send just planned, nil when none was.
func (s *Program) planNextRound(
	ctx context.Context,
	program *model.Program,
	e *database.ProgramEnrollment,
	roundStart time.Time,
	now time.Time,
	position int,
	loopCount int,
	sent *programSent,
) error {
	fields := map[string]any{}
	lastSend := e.LastSendAt
	if sent != nil {
		lastSend = &sent.at
		fields["last_send_at"] = sent.at
		fields["last_step_id"] = sent.stepID
	}
	next := programNextRoundStart(program, roundStart, now)
	due := programPickSendTimeAfter(program, next, now, programMinNextSend(program, lastSend), s.rng())
	fields["position"] = position
	fields["loop_count"] = loopCount
	fields["round_start_at"] = next
	fields["next_due_at"] = due
	return s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, fields)
}

// completeEnrollment marks a recipient as done with the program
func (s *Program) completeEnrollment(ctx context.Context, e *database.ProgramEnrollment, now time.Time) error {
	return s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
		"status":       model.ProgramEnrollmentCompleted,
		"completed_at": now,
		"next_due_at":  nil,
	})
}

// deliveredSteps returns the ids of the steps that count as given to a person
// in a loop. A send of a step that is no longer in the list counts for a step
// of the same template that is not given yet, so a template removed and added
// again in the same loop is not sent twice.
func deliveredSteps(steps []*model.ProgramStep, deliveries []repository.ProgramDelivery) map[uuid.UUID]bool {
	current := make(map[uuid.UUID]bool, len(steps))
	for _, st := range steps {
		current[st.ID.MustGet()] = true
	}
	out := map[uuid.UUID]bool{}
	removed := map[uuid.UUID]bool{}
	leftover := map[uuid.UUID]int{}
	for _, d := range deliveries {
		if current[d.StepID] {
			out[d.StepID] = true
			continue
		}
		if removed[d.StepID] || d.TemplateID == nil {
			continue
		}
		removed[d.StepID] = true
		leftover[*d.TemplateID]++
	}
	for _, st := range steps {
		id := st.ID.MustGet()
		if out[id] {
			continue
		}
		templateID := st.CampaignTemplateID.MustGet()
		if leftover[templateID] > 0 {
			leftover[templateID]--
			out[id] = true
		}
	}
	return out
}

// undeliveredSteps returns the steps not in delivered
func undeliveredSteps(steps []*model.ProgramStep, delivered map[uuid.UUID]bool) []*model.ProgramStep {
	out := make([]*model.ProgramStep, 0, len(steps))
	for _, st := range steps {
		if !delivered[st.ID.MustGet()] {
			out = append(out, st)
		}
	}
	return out
}

// removeStep returns steps without the given step
func removeStep(steps []*model.ProgramStep, step *model.ProgramStep) []*model.ProgramStep {
	out := make([]*model.ProgramStep, 0, len(steps))
	for _, st := range steps {
		if st != step {
			out = append(out, st)
		}
	}
	return out
}

// getOrCreateStepCampaign returns the hidden campaign for a step and loop, lazily
// creating it. It reuses the campaign send, tracking and reporting pipeline.
func (s *Program) getOrCreateStepCampaign(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	step *model.ProgramStep,
	loopIndex int,
	campaignCache map[string]*model.Campaign,
) (*model.Campaign, error) {
	programID := program.ID.MustGet()
	stepID := step.ID.MustGet()
	cacheKey := stepID.String() + ":" + strconv.Itoa(loopIndex)
	if campaignCache != nil {
		if c, ok := campaignCache[cacheKey]; ok {
			return c, nil
		}
	}
	remember := func(c *model.Campaign) *model.Campaign {
		if campaignCache != nil && c != nil {
			campaignCache[cacheKey] = c
		}
		return c
	}
	// the newest campaign of the step and loop that is not closed. When all are
	// closed a new one is made, the callers check the template can be sent first
	open, err := s.openStepCampaign(ctx, &programID, &stepID, loopIndex)
	if err != nil {
		return nil, err
	}
	if open != nil {
		return remember(open), nil
	}
	templateID := step.CampaignTemplateID.MustGet()
	// name is unique per company. Step id, loop index and a random suffix keep
	// it unique, also next to a campaign left behind by a failed create, and it
	// stays within the sixty four character limit
	name := "__program:" + stepID.String() + ":" + strconv.Itoa(loopIndex) + ":" + campaignNameSuffix()
	nameVO, err := vo.NewString64(name)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	isAnon := program.IsAnonymousOn()
	campaign := &model.Campaign{
		Name:        nullable.NewNullableWithValue(*nameVO),
		TemplateID:  nullable.NewNullableWithValue(templateID),
		ProgramID:   nullable.NewNullableWithValue(programID),
		IsAnonymous: nullable.NewNullableWithValue(isAnon),
	}
	if program.CompanyID.IsSpecified() && !program.CompanyID.IsNull() {
		campaign.CompanyID = nullable.NewNullableWithValue(program.CompanyID.MustGet())
	}
	campaignID, err := s.ProgramRepository.InsertStepCampaign(ctx, campaign)
	if err != nil {
		// another writer may have created the campaign for this step and loop
		if c, gErr := s.openStepCampaign(ctx, &programID, &stepID, loopIndex); gErr == nil && c != nil {
			return remember(c), nil
		}
		return nil, errs.Wrap(err)
	}
	if err := s.ProgramRepository.InsertProgramCampaign(ctx, &programID, &stepID, loopIndex, campaignID); err != nil {
		// a concurrent writer linked its own campaign first: drop this one and
		// use the linked one
		if dErr := s.CampaignRepository.DeleteByID(ctx, campaignID); dErr != nil {
			s.Logger.Errorw("failed to remove unlinked program campaign", "error", dErr)
		}
		if c, gErr := s.openStepCampaign(ctx, &programID, &stepID, loopIndex); gErr == nil && c != nil {
			return remember(c), nil
		}
		return nil, errs.Wrap(err)
	}
	if err := s.applyStepCampaignSettings(ctx, session, &programID, campaignID); err != nil {
		return nil, err
	}
	campaign.ID = nullable.NewNullableWithValue(*campaignID)
	// snapshot lure and training settings while the campaign holds no recipients
	s.CampaignService.snapshotLureSettings(ctx, session, campaign)
	reloaded, err := s.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{
		IncludeProgramManaged: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return remember(reloaded), nil
}

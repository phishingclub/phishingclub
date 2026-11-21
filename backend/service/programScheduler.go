package service

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"text/template"
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
	"gorm.io/gorm"
)

// programAdvanceLimit bounds how many due enrollments one tick advances, so a
// large program drains over several ticks like the send batch does. It is
// half the send batch of a tick, see Campaign.SendNextBatch, so a program that
// plans many people at once leaves room in every batch for the sends of other
// campaigns and does not build a queue they wait behind.
const programAdvanceLimit = 500

// programAdvanceBatch is how many due enrollments are advanced while the
// lifecycle lock is held. The lock is let go between batches, so a pause,
// close or edit waits for one batch only.
const programAdvanceBatch = 100

// programFailureBackoff is how long a person waits after the first failed
// attempt to advance them. Each failure in a row doubles it, up to
// programFailureBackoffMax, so one broken person never blocks the due scan.
const programFailureBackoff = time.Hour

// programFailureBackoffMax is the longest wait after failed attempts
const programFailureBackoffMax = 24 * time.Hour

// programCatchUpGrace is how late a person may be advanced and still be sent at
// the next window opening. A person later than that, for example after the
// server was down, gets a new time drawn over the spread.
const programCatchUpGrace = time.Hour

// programTickLagGrace is how long after a due time inside the sending hours a
// tick may come and still send at that due time. It covers the time between
// ticks, so a due time just before the hours close is not moved to the next
// opening.
const programTickLagGrace = time.Minute

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
// cache, when given, for the rest of the tick. A missing template is unusable,
// and so is one whose email has no content or does not parse, which the
// sender would hold, see templateEmailSendable.
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
	if v {
		if v, err = s.templateEmailSendable(ctx, templateID); err != nil {
			return false, err
		}
	}
	if cache != nil {
		cache[templateID] = v
	}
	return v, nil
}

// templateEmailSendable reports whether the email of a template has content
// that parses, the checks the sender makes before it sends. A template or
// email that is missing is left to the usable flag, which a removed email
// clears. Without the email service the check is skipped.
func (s *Program) templateEmailSendable(ctx context.Context, templateID uuid.UUID) (bool, error) {
	if s.CampaignService == nil || s.CampaignService.MailService == nil ||
		s.CampaignService.MailService.EmailRepository == nil || s.CampaignTemplateService == nil ||
		s.CampaignTemplateService.CampaignTemplateRepository == nil {
		return true, nil
	}
	t, err := s.CampaignTemplateService.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, errs.Wrap(err)
	}
	emailID, err := t.EmailID.Get()
	if err != nil {
		return true, nil
	}
	email, err := s.CampaignService.MailService.EmailRepository.GetByID(ctx, &emailID, &repository.EmailOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, errs.Wrap(err)
	}
	content, err := email.Content.Get()
	if err != nil {
		return false, nil
	}
	if _, err := template.New("email").Funcs(TemplateFuncs()).Parse(content.String()); err != nil {
		return false, nil
	}
	return true, nil
}

// pickSendableStep picks a step from the candidates whose template can be sent
// and whose step campaign is open. A step with a broken template is skipped for
// this pick only, it stays undelivered. blocked reports whether such a step was
// skipped. mixCounts, when not nil, are the sends of the person's colleagues
// per step, see programPickStepMixed.
func (s *Program) pickSendableStep(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	candidates []*model.ProgramStep,
	loopCount int,
	rng *rand.Rand,
	campaignCache map[string]*model.Campaign,
	usableCache map[uuid.UUID]bool,
	mixCounts map[uuid.UUID]int,
) (*model.ProgramStep, *model.Campaign, bool, error) {
	blocked := false
	for len(candidates) > 0 {
		pick := programPickStepMixed(program, candidates, rng, mixCounts)
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

// postponeEnrollment moves a person's next send to a time drawn over the
// spread from one day later, keeping their round and loop, used when none of
// their next steps can be sent now. The draw keeps the people postponed
// together from all being sent at the same window opening once fixed.
func (s *Program) postponeEnrollment(
	ctx context.Context,
	w *programWrites,
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
	due := programPickSendTimeAfter(program, now.Add(programPostponeDelay), now, time.Time{}, s.rng())
	return s.storeEnrollment(ctx, w, e, map[string]any{
		"next_due_at": due,
	})
}

// replanCancelledCampaignSends puts the people whose sends a close of a hidden
// campaign cancelled back on schedule, see planAfterCancelledSends: an active
// person is due again at about the time of the cancelled send, a completed
// person with the step now undelivered is made active again. Nothing changes
// for a program that is closed, being closed or deleted, or not started, where
// the sends were cancelled on purpose. A failure is logged.
func (s *Program) replanCancelledCampaignSends(
	ctx context.Context,
	campaign *model.Campaign,
	cancelled []*model.CampaignRecipient,
) {
	s.replanCancelledSendsAfter(ctx, campaign, cancelled, time.Time{})
}

// programHoldRetryDelay is the least time a person waits after the sender held
// a send of theirs. The scheduler skips a template it knows can not be sent,
// the wait keeps a cause it does not see from trying the same send on every
// tick.
const programHoldRetryDelay = time.Hour

// replanHeldSends puts the people whose sends the sender cancelled, as their
// template can not be sent now, back on schedule as if the step was not given.
// They are due again no sooner than programHoldRetryDelay, when the scheduler
// picks another step and offers the held one again once it can be sent.
func (s *Program) replanHeldSends(
	ctx context.Context,
	campaign *model.Campaign,
	cancelled []*model.CampaignRecipient,
) {
	s.replanCancelledSendsAfter(ctx, campaign, cancelled, time.Now().Add(programHoldRetryDelay))
}

// replanCancelledSendsAfter is replanCancelledCampaignSends where no person is
// due again before notBefore. A zero notBefore sets no bound.
func (s *Program) replanCancelledSendsAfter(
	ctx context.Context,
	campaign *model.Campaign,
	cancelled []*model.CampaignRecipient,
	notBefore time.Time,
) {
	if len(cancelled) == 0 {
		return
	}
	programID, err := campaign.ProgramID.Get()
	if err != nil {
		return
	}
	campaignID := campaign.ID.MustGet()
	ctx = context.WithoutCancel(ctx)
	fail := func(err error) {
		s.Logger.Errorw("failed to plan program people again after their sends were cancelled",
			"programID", programID.String(),
			"campaignID", campaignID.String(),
			"error", err,
		)
	}
	program, err := s.ProgramRepository.GetByID(ctx, &programID, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		fail(err)
		return
	}
	if program.IsClosed() || program.IsStopping() || program.StartedAt == nil {
		return
	}
	stepID, err := s.ProgramRepository.GetCampaignStepID(ctx, &campaignID)
	if err != nil {
		fail(err)
		return
	}
	sends := make([]repository.ProgramStepUnsentSend, 0, len(cancelled))
	for _, cr := range cancelled {
		id, err := cr.ID.Get()
		if err != nil {
			continue
		}
		send := repository.ProgramStepUnsentSend{ID: id}
		if v, err := cr.RecipientID.Get(); err == nil {
			send.RecipientID = &v
		}
		if v, err := cr.AnonymizedID.Get(); err == nil {
			send.AnonymizedID = &v
		}
		if v, err := cr.SendAt.Get(); err == nil {
			send.SendAt = &v
		}
		if !notBefore.IsZero() && (send.SendAt == nil || send.SendAt.Before(notBefore)) {
			at := notBefore
			send.SendAt = &at
		}
		sends = append(sends, send)
	}
	// the step of the campaign stands for the removed step: a person whose last
	// planned send was of it gets that round back
	if _, err := s.planAfterCancelledSends(ctx, program, program, []*uuid.UUID{stepID}, sends, time.Now()); err != nil {
		fail(err)
	}
}

// removeOldIdentityLinks removes the person link of every send of an anonymous
// program sent longer ago than the retention, keeping only the pseudonym. The
// enrollment still knows the pseudonym, so delivery history keeps working.
// The rows changed are written as one event per UTC month with the total of
// the month, once the month has passed, and no event for a month without
// changes.
// The programs are not named: they are anonymous, and when their rows changed
// would tell when their people were sent. A failure is written as its own
// event, see auditBackgroundFailure. A total not yet written is lost when the
// process stops.
func (s *Program) removeOldIdentityLinks(ctx context.Context, session *model.Session, now time.Time) {
	fail := func(failedIDs []string, err error) {
		fe := NewAuditEvent("Program.RemoveIdentityLinks", session)
		if len(failedIDs) > 0 {
			fe.Details["failedProgramIds"] = failedIDs
		}
		s.auditBackgroundFailure("identity", fe, err)
	}
	var rows int64
	defer func() {
		s.auditIdentityLinks(session, now, rows)
	}()
	programIDs, err := s.ProgramRepository.GetAnonymousProgramIDs(ctx)
	if err != nil {
		s.Logger.Errorw("failed to list anonymous programs", "error", err)
		fail(nil, err)
		return
	}
	before := now.Add(-programIdentityRetention)
	failedIDs := []string{}
	var firstErr error
	for _, id := range programIDs {
		n, err := s.ProgramRepository.RemoveIdentityOfSendsBefore(ctx, id, before)
		// a batch may be written before a later one fails
		rows += n
		// a device code entry of a send that no longer names its person would
		// still name the person and what was captured
		if err == nil {
			err = s.ProgramRepository.DeleteUnlinkedDeviceCodes(ctx, id)
		}
		if err != nil {
			s.Logger.Errorw("failed to remove old identity links", "programID", id.String(), "error", err)
			failedIDs = append(failedIDs, id.String())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		fail(failedIDs, firstErr)
	}
}

// auditIdentityLinks adds the rows changed by one identity removal pass to
// the total of the UTC month of now, and writes the total of an earlier month
// once a pass runs in a later month. A day total is not written, as with one
// anonymous program it would give that program's sends per day.
func (s *Program) auditIdentityLinks(session *model.Session, now time.Time, rows int64) {
	month := now.UTC().Format("2006-01")
	if s.identityAuditMonth == "" {
		s.identityAuditMonth = month
	}
	if month != s.identityAuditMonth {
		if s.identityAuditRows > 0 {
			ae := NewAuditEvent("Program.RemoveIdentityLinks", session)
			ae.Details["month"] = s.identityAuditMonth
			ae.Details["rows"] = s.identityAuditRows
			s.AuditLogAuthorized(ae)
		}
		s.identityAuditMonth = month
		s.identityAuditRows = 0
	}
	s.identityAuditRows += rows
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

// Tick is the scheduler step, run by the task runner. It closes programs past
// their end, resolves enrollments from group membership at a throttled
// cadence, then advances every due recipient by scheduling their next step
// into a hidden campaign. One event per tick names the named programs it
// changed with the counts, written only when something changed. An
// anonymous program is not named and not counted, see auditTick.
func (s *Program) Tick(ctx context.Context, session *model.Session) (err error) {
	ae := NewAuditEvent("Program.Advance", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	stats := &programTickStats{}
	ctx = withProgramTickStats(ctx, stats)
	defer func() {
		s.auditTick(ctx, ae, session, stats, err)
	}()
	now := time.Now()
	membershipDue, retentionDue := false, false
	if err := s.withLifecycleLock(ctx, func() error {
		membershipDue = now.Sub(s.lastMembershipAt) >= programMembershipInterval
		if membershipDue {
			s.lastMembershipAt = now
		}
		retentionDue = now.Sub(s.lastRetentionAt) >= programRetentionInterval
		if retentionDue {
			s.lastRetentionAt = now
		}
		return nil
	}); err != nil {
		return err
	}
	// the programs at their end are closed in the background, each close
	// takes the lifecycle lock for its short part only
	s.endProgramsDueInBackground(ctx, session)
	// the membership pass takes the lifecycle lock for one program at a time
	if membershipDue {
		if err := s.resolveEnrollments(ctx); err != nil {
			s.Logger.Errorw("failed to resolve program enrollments", "error", err)
			stats.fail(err)
		}
		// the backstops repair what a request that failed half way left, they
		// run without the lifecycle lock
		s.reconcileCampaignPause(ctx)
		if err := s.cancelRemovedStepBacklog(ctx); err != nil {
			s.Logger.Errorw("failed to cancel the sends of removed program templates", "error", err)
			stats.fail(err)
		}
		s.repairCampaignSettings(ctx)
		// a delete that stopped half way is finished in the background
		s.finishDeletesInBackground(ctx, session, nil)
	}
	// the identity removal only changes rows that are already sent, which no
	// lifecycle call changes, so it runs without the lock
	if retentionDue {
		s.removeOldIdentityLinks(ctx, session, now)
	}
	// the passes above can take long, so the sends are planned from the time
	// now and not from the start of the tick, or a send could land on a slot
	// that has passed
	return s.advanceDue(ctx, session, time.Now())
}

// programCloseRetryEvery is how long the scheduler waits before it tries a
// failed close of the same program at its end again
const programCloseRetryEvery = 10 * time.Minute

// endProgramsDue closes every program whose end date has passed, the same way
// an operator closes it, and finishes every close that began and stopped half
// way. A program whose close failed is tried again after
// programCloseRetryEvery, not on every tick. The caller must not hold the
// lifecycle lock.
func (s *Program) endProgramsDue(ctx context.Context, session *model.Session, now time.Time) {
	// the programs are read in batches, each program once per pass. The held
	// ones and the ones already tried are left out of the next batch.
	skip := s.closeHeldIDs(now)
	for len(skip) < programCloseSkipLimit {
		ids, err := s.ProgramRepository.GetIDsToClose(ctx, now, programCloseBatch, skip)
		if err != nil {
			s.Logger.Errorw("failed to list programs due to end", "error", err)
			return
		}
		for _, id := range ids {
			skip = append(skip, id)
			s.endProgramDue(ctx, session, id, now)
		}
		if len(ids) < programCloseBatch {
			return
		}
	}
}

// closeTakenByDelete reports a close that a delete of the program took over:
// the program is gone or marked as being deleted
func closeTakenByDelete(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	var v errs.ValidationError
	return errors.As(err, &v) && v.Err == errProgramStopping
}

// programCloseBatch is how many programs to close one read of the close pass
// returns, programCloseSkipLimit bounds how many one pass looks at
const (
	programCloseBatch     = 100
	programCloseSkipLimit = 2000
)

// endProgramDue closes one program whose end date has passed or whose close
// began, see endProgramsDue
func (s *Program) endProgramDue(ctx context.Context, session *model.Session, id *uuid.UUID, now time.Time) {
	ae := NewAuditEvent("Program.Close", session)
	ae.Details["id"] = id.String()
	ae.Details["reason"] = "end date reached"
	reason := "program end date reached"
	if program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{}); err == nil {
		ended := program.IsEndedBy(now)
		if !ended {
			ae.Details["reason"] = "close finished"
			reason = "program closed"
		}
		// a draft has no start, it is closed as it is and never sends
		if ended && program.StatusAt(now) == model.ProgramStatusDraft {
			ae.Details["draft"] = true
			s.Logger.Infow("draft program passed its end date, closing it", "programID", id.String())
		}
	}
	err := s.closeProgram(ctx, session, id, reason, true)
	// a close of the program is under way in a request, or its end was
	// moved later, it is not a failure
	if isProgramBusy(err) || errors.Is(err, errProgramNotEnded) {
		return
	}
	// a delete that began after the close was marked takes the program over,
	// the close is not a failure
	if closeTakenByDelete(err) {
		s.takeRequester(programActionClose, *id, true)
		return
	}
	if by := s.takeRequester(programActionClose, *id, err == nil); by != "" {
		ae.Details["requestedBy"] = by
	}
	if err != nil {
		s.Logger.Errorw("failed to close program", "programID", id.String(), "error", err)
		s.recordActionFailure(*id, programActionClose, err)
		// the failure is audited at most once per programFailureAuditEvery
		if s.closeFailed(*id, now) {
			s.AuditLogFailed(ae, err)
		}
		return
	}
	s.closeDone(*id)
	// a closed program needs nothing kept in memory
	s.forgetProgramState(*id, true)
	s.Logger.Infow("program closed", "programID", id.String())
	s.AuditLogAuthorized(ae)
}

// reconcileCampaignPause makes the pause mark of every hidden campaign match
// its program, so a pause, resume, close or delete that stopped between its
// writes never leaves a paused program sending or a running one held. A
// failure is logged, the next pass tries again.
func (s *Program) reconcileCampaignPause(ctx context.Context) {
	changed, err := s.ProgramRepository.ReconcileCampaignPause(ctx)
	if err != nil {
		s.Logger.Errorw("failed to match the pause of program campaigns", "error", err)
		programTickStatsFrom(ctx).fail(err)
		return
	}
	if changed > 0 {
		s.Logger.Warnw("program campaign pause did not match its program, fixed", "campaigns", changed)
	}
}

// programSettingsRepairLimit is how many programs one pass of
// repairCampaignSettings repairs
const programSettingsRepairLimit = 20

// repairCampaignSettings copies the settings of a program to its open hidden
// campaigns when they differ, for example because the copy after an edit
// failed. Each program takes the lifecycle lock for its own copy only, so it
// does not cross an edit, and is read again under it. A failure is logged,
// the next pass tries again. The caller must not hold the lifecycle lock.
func (s *Program) repairCampaignSettings(ctx context.Context) {
	ids, err := s.ProgramRepository.GetProgramsWithStaleCampaignSettings(ctx, programSettingsRepairLimit)
	if err != nil {
		s.Logger.Errorw("failed to find program campaigns with stale settings", "error", err)
		programTickStatsFrom(ctx).fail(err)
		return
	}
	for i, id := range ids {
		if i > 0 {
			if err := repository.PauseBetweenWrites(ctx); err != nil {
				return
			}
		}
		err := s.withLifecycleLock(ctx, func() error {
			program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return errs.Wrap(err)
			}
			if program.IsClosed() || program.IsStopping() {
				return nil
			}
			return s.syncProgramCampaignSettings(ctx, id)
		})
		if err != nil {
			s.Logger.Errorw("failed to repair program campaign settings", "programID", id.String(), "error", err)
			programTickStatsFrom(ctx).fail(err)
			continue
		}
		s.Logger.Warnw("program campaign settings did not match the program, fixed", "programID", id.String())
	}
}

// programRemovedStepCancelLimit is how many sends of removed steps one pass
// of cancelRemovedStepBacklog cancels. A longer backlog is cancelled over the
// next passes.
const programRemovedStepCancelLimit = 5000

// cancelRemovedStepBacklog cancels the sends, neither sent nor cancelled, of
// the steps removed from a program, up to programRemovedStepCancelLimit, and
// plans their people again, see cancelRemovedStepSendsOf. An edit does this
// right after it is stored, this pass finishes what an edit that failed half
// way left. The caller must not hold the lifecycle lock.
func (s *Program) cancelRemovedStepBacklog(ctx context.Context) error {
	sends, err := s.ProgramRepository.GetUnsentSendsOfRemovedSteps(ctx, programRemovedStepCancelLimit)
	if err != nil {
		return errs.Wrap(err)
	}
	byProgram := map[uuid.UUID][]repository.RemovedStepSend{}
	order := []uuid.UUID{}
	for _, sd := range sends {
		if _, ok := byProgram[sd.ProgramID]; !ok {
			order = append(order, sd.ProgramID)
		}
		byProgram[sd.ProgramID] = append(byProgram[sd.ProgramID], sd)
	}
	var failed error
	for i, programID := range order {
		if i > 0 {
			if err := repository.PauseBetweenWrites(ctx); err != nil {
				return err
			}
		}
		if err := s.cancelRemovedStepSendsOf(ctx, programID, byProgram[programID]); err != nil {
			s.Logger.Errorw("failed to cancel the sends of removed program templates", "programID", programID.String(), "error", err)
			failed = err
		}
	}
	return failed
}

// cancelRemovedStepSendsOf cancels the given sends of steps removed from one
// program, in chunks each under the lifecycle lock for its own write only,
// and then plans the people who lost a send again, see
// planAfterCancelledSends. A program being closed or deleted is left to that
// close or delete, which cancels every send.
func (s *Program) cancelRemovedStepSendsOf(ctx context.Context, programID uuid.UUID, rows []repository.RemovedStepSend) error {
	program, err := s.ProgramRepository.GetByID(ctx, &programID, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsStopping() {
		return nil
	}
	current := make(map[uuid.UUID]bool, len(program.Steps))
	for _, st := range program.Steps {
		current[st.ID.MustGet()] = true
	}
	ids := make([]*uuid.UUID, 0, len(rows))
	sends := make([]repository.ProgramStepUnsentSend, 0, len(rows))
	removed := []*uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for i := range rows {
		if current[rows[i].StepID] {
			continue
		}
		ids = append(ids, &rows[i].ID)
		sends = append(sends, rows[i].ProgramStepUnsentSend)
		if !seen[rows[i].StepID] {
			seen[rows[i].StepID] = true
			removed = append(removed, &rows[i].StepID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	notStopping := func(p *model.Program) bool { return !p.IsStopping() }
	cancelled := 0
	done, err := s.writeInChunks(ctx, &programID, len(ids), notStopping, func(start, end int) error {
		n, err := s.ProgramRepository.CancelSends(ctx, ids[start:end])
		cancelled += n
		return err
	})
	if err != nil || !done {
		return err
	}
	if cancelled > 0 {
		s.Logger.Warnw("cancelled the sends of removed program templates", "programID", programID.String(), "sends", cancelled)
	}
	if program.IsClosed() || program.StartedAt == nil {
		return nil
	}
	if err := repository.PauseBetweenWrites(ctx); err != nil {
		return err
	}
	_, err = s.planAfterCancelledSends(ctx, program, program, removed, sends, time.Now())
	return err
}

// advanceDue schedules the next step for every enrollment that is due, at
// most programAdvanceLimit per tick. Each batch takes the lifecycle lock and
// reads the programs and campaigns again, so a program paused or closed
// between batches does not advance.
func (s *Program) advanceDue(ctx context.Context, session *model.Session, now time.Time) error {
	// the template mixing counts are read once per program and tick
	mixes := map[uuid.UUID]*programMix{}
	for advanced := 0; advanced < programAdvanceLimit; {
		n, err := s.advanceDueBatchWith(ctx, session, now, mixes)
		if err != nil {
			return err
		}
		advanced += n
		if n < programAdvanceBatch {
			return nil
		}
	}
	return nil
}

// advanceDueBatch advances one batch of due enrollments under the lifecycle
// lock and returns how many were read
func (s *Program) advanceDueBatch(
	ctx context.Context,
	session *model.Session,
	now time.Time,
) (int, error) {
	return s.advanceDueBatchWith(ctx, session, now, map[uuid.UUID]*programMix{})
}

// advanceDueBatchWith is advanceDueBatch with the template mixing state of the
// earlier batches of the tick, by program, which it reads and updates
func (s *Program) advanceDueBatchWith(
	ctx context.Context,
	session *model.Session,
	now time.Time,
	mixes map[uuid.UUID]*programMix,
) (int, error) {
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return 0, errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	due, err := s.ProgramRepository.GetDueEnrollments(ctx, now, programAdvanceBatch)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	programCache := map[string]*model.Program{}
	// a fresh wave has most recipients on the same (step, loop), so their hidden
	// campaign is the same row, so it is cached for this batch and not read
	// again for each due recipient
	campaignCache := map[string]*model.Campaign{}
	// whether a template can be sent is read once per batch
	usableCache := map[uuid.UUID]bool{}
	// the sends of the batch people are read once per program
	sendsCache := map[uuid.UUID]*repository.ProgramPeopleSends{}
	// the template mixing state is updated once per program and batch
	mixSeen := map[uuid.UUID]bool{}
	w := &programWrites{recipients: map[uuid.UUID]*model.Recipient{}}
	// the people whose advance or writes failed
	failed := map[uuid.UUID]bool{}
	// the people of an anonymous program are read once per program and batch
	recipientsRead := map[uuid.UUID]bool{}
	for _, e := range due {
		programID := e.ProgramID
		program, ok := programCache[programID.String()]
		if !ok {
			p, err := s.ProgramRepository.GetByID(ctx, programID, &repository.ProgramOption{WithSteps: true})
			if err != nil {
				s.Logger.Errorw("failed to load program for advancement", "error", err)
				// the person waits like after a failed advance, so the batch
				// does not read them again
				s.delayFailedEnrollment(ctx, e, now, err)
				continue
			}
			programCache[programID.String()] = p
			program = p
		}
		// a paused, closed or not yet started program should not advance, nor
		// one that is past its end or being closed or deleted, endProgramsDue
		// closes it
		if program.StatusAt(now) != model.ProgramStatusActive || program.IsStopping() || program.IsEndedBy(now) {
			continue
		}
		sends, ok := sendsCache[*programID]
		if !ok {
			sends, err = s.ProgramRepository.GetPeopleSends(ctx, programID, programBatchPeople(due, *programID))
			if err != nil {
				// each person then reads their own sends
				s.Logger.Errorw("failed to read the sends of the program batch", "error", err)
				sends = nil
			}
			sendsCache[*programID] = sends
		}
		if !mixSeen[*programID] {
			mixSeen[*programID] = true
			mix, err := s.programMixFor(ctx, mixes[*programID], program, due, now)
			if err != nil {
				// the batch then picks at random
				s.Logger.Errorw("failed to read the template mix of the program batch", "error", err)
				mix = nil
			}
			mixes[*programID] = mix
		}
		mix := mixes[*programID]
		if program.IsAnonymousOn() && !recipientsRead[*programID] {
			recipientsRead[*programID] = true
			s.readBatchRecipients(ctx, w, due, *programID)
		}
		w.begin(e)
		if err := s.advanceEnrollmentWith(ctx, session, program, e, now, campaignCache, usableCache, sends, mix, w); err != nil {
			w.drop()
			failed[*e.ID] = true
			s.delayFailedEnrollment(ctx, e, now, err)
			continue
		}
		// an advance that works resets the failures in the same write
		if e.AdvanceFailures > 0 {
			if w.current.fields == nil {
				w.current.fields = map[string]any{}
			}
			w.current.fields["advance_failures"] = 0
		}
	}
	for id := range s.storeWrites(ctx, w, now) {
		failed[id] = true
	}
	// the sends stored with the batch write, a send stored right away is
	// counted where it is stored
	stats := programTickStatsFrom(ctx)
	for _, p := range w.people {
		if !failed[*p.e.ID] && (p.send != nil || p.reviveID != nil) {
			stats.add(p.e.ProgramID, 0, 1, 0, 0)
		}
	}
	mixFailed := programMixFailed(w, failed)
	for id := range mixSeen {
		mixes[id].settle(mixFailed)
	}
	return len(due), nil
}

// programMixFailed returns the people of a batch whose pick is taken back out
// of the mix counts: those whose writes failed, except the people whose send
// was stored right away, as that send exists and a new read counts it
func programMixFailed(w *programWrites, failed map[uuid.UUID]bool) map[uuid.UUID]bool {
	stored := map[uuid.UUID]bool{}
	for _, p := range w.people {
		if p.sendStored {
			stored[*p.e.ID] = true
		}
	}
	out := make(map[uuid.UUID]bool, len(failed))
	for id := range failed {
		if !stored[id] {
			out[id] = true
		}
	}
	return out
}

// readBatchRecipients reads the people of one program in the batch in one
// read, for the department and position an anonymous send keeps. A failure is
// logged and each send then reads its person.
func (s *Program) readBatchRecipients(
	ctx context.Context,
	w *programWrites,
	due []*database.ProgramEnrollment,
	programID uuid.UUID,
) {
	ids := []*uuid.UUID{}
	for _, e := range due {
		if e.ProgramID != nil && *e.ProgramID == programID && e.RecipientID != nil {
			ids = append(ids, e.RecipientID)
		}
	}
	recipients, err := s.RecipientRepository.GetByIDs(ctx, ids)
	if err != nil {
		s.Logger.Warnw("failed to read the program people of the batch", "error", err)
		return
	}
	for _, r := range recipients {
		if id, err := r.ID.Get(); err == nil {
			w.recipients[id] = r
		}
	}
}

// programWrites holds the writes of one advance batch, so they are stored
// together in one transaction that only writes. recipients holds the people of
// the batch read ahead, by id, for the anonymous snapshot.
type programWrites struct {
	people     []*programPersonWrites
	current    *programPersonWrites
	recipients map[uuid.UUID]*model.Recipient
}

// programPersonWrites is what an advance stores for one person: a new send or
// a cancelled send put back in the queue, and the enrollment progress
type programPersonWrites struct {
	e        *database.ProgramEnrollment
	send     *model.CampaignRecipient
	reviveID *uuid.UUID
	reviveAt time.Time
	fields   map[string]any
	// sendStored is set when the send was stored right away and not with the
	// batch, as a send with a lure code is
	sendStored bool
}

// begin starts the writes of the next person
func (w *programWrites) begin(e *database.ProgramEnrollment) {
	w.current = &programPersonWrites{e: e}
	w.people = append(w.people, w.current)
}

// drop forgets the writes of the current person, whose advance failed
func (w *programWrites) drop() {
	if len(w.people) > 0 && w.people[len(w.people)-1] == w.current {
		w.people = w.people[:len(w.people)-1]
	}
	w.current = nil
}

// storeEnrollment stores progress fields of an enrollment, or keeps them in w
// for the batch write
func (s *Program) storeEnrollment(
	ctx context.Context,
	w *programWrites,
	e *database.ProgramEnrollment,
	fields map[string]any,
) error {
	if w != nil {
		w.current.fields = fields
		return nil
	}
	return s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, fields)
}

// storePersonWrites stores the writes of one person with the given repositories
func storePersonWrites(
	ctx context.Context,
	programRepo *repository.Program,
	recipientRepo *repository.CampaignRecipient,
	p *programPersonWrites,
) error {
	if p.send != nil {
		if _, err := recipientRepo.Insert(ctx, p.send); err != nil {
			return errs.Wrap(err)
		}
	}
	if p.reviveID != nil {
		if err := programRepo.ReviveCancelledSend(ctx, p.reviveID, p.reviveAt); err != nil {
			return errs.Wrap(err)
		}
	}
	if p.fields != nil {
		if err := programRepo.UpdateEnrollmentFields(ctx, p.e.ID, p.fields); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// storeWrites stores the writes of a batch in one transaction that only
// writes. When it fails each person is stored on their own, and a person whose
// writes fail waits like after a failed advance, as with writes made one by
// one. It returns the enrollment ids of the people whose writes failed.
func (s *Program) storeWrites(ctx context.Context, w *programWrites, now time.Time) map[uuid.UUID]bool {
	failed := map[uuid.UUID]bool{}
	if len(w.people) == 0 {
		return failed
	}
	err := s.ProgramRepository.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		programRepo := &repository.Program{DB: tx}
		recipientRepo := &repository.CampaignRecipient{DB: tx}
		for _, p := range w.people {
			if err := storePersonWrites(ctx, programRepo, recipientRepo, p); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		return failed
	}
	s.Logger.Warnw("failed to store the program batch in one write, storing each person", "error", err)
	for _, p := range w.people {
		if err := storePersonWrites(ctx, s.ProgramRepository, s.CampaignRecipientRepository, p); err != nil {
			failed[*p.e.ID] = true
			s.delayFailedEnrollment(ctx, p.e, now, err)
		}
	}
	return failed
}

// programBatchPeople returns the people of the due enrollments of one program,
// each with their loop, so only the campaigns of the loops around it are read
func programBatchPeople(due []*database.ProgramEnrollment, programID uuid.UUID) []repository.ProgramPerson {
	out := []repository.ProgramPerson{}
	for _, e := range due {
		if e.ProgramID != nil && *e.ProgramID == programID {
			loop := e.LoopCount
			out = append(out, repository.ProgramPerson{RecipientID: e.RecipientID, AnonymizedID: e.AnonymizedID, Loop: &loop})
		}
	}
	return out
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
	return s.advanceEnrollmentWith(ctx, session, program, e, now, campaignCache, usableCache, nil, nil, nil)
}

// advanceEnrollmentWith is advanceEnrollment with the sends of the person read
// ahead for the whole batch. With sends nil they are read for this person.
// With mix the template is mixed among colleagues, nil picks at random.
// With w the writes are kept in w and stored later for the whole batch, with
// w nil they are stored right away.
func (s *Program) advanceEnrollmentWith(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	e *database.ProgramEnrollment,
	now time.Time,
	campaignCache map[string]*model.Campaign,
	usableCache map[uuid.UUID]bool,
	sends *repository.ProgramPeopleSends,
	mix *programMix,
	w *programWrites,
) error {
	steps := program.Steps
	if len(steps) == 0 {
		return s.completeEnrollment(ctx, w, e, now, e.Position, nil)
	}
	person := repository.ProgramPerson{RecipientID: e.RecipientID, AnonymizedID: e.AnonymizedID}
	var deliveries []repository.ProgramDelivery
	if sends != nil {
		deliveries = sends.LoopDeliveries(person, e.LoopCount)
	} else {
		var err error
		deliveries, err = s.ProgramRepository.GetLoopDeliveries(ctx, e.ProgramID, e.RecipientID, e.AnonymizedID, e.LoopCount)
		if err != nil {
			return errs.Wrap(err)
		}
	}
	delivered := deliveredSteps(steps, deliveries)
	loopCount := e.LoopCount
	candidates := undeliveredSteps(steps, delivered)
	sentBefore := len(steps) - len(candidates)
	if len(candidates) == 0 {
		if program.LoopModeOrDefault() != model.ProgramLoopLoop {
			return s.completeEnrollment(ctx, w, e, now, len(steps), nil)
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
	// the due time was drawn inside the window, a late tick snaps to the next
	// opening. A person overdue by more than a grace, for example after the
	// server was down, is drawn over the spread from now instead, so the people
	// who waited are not all sent at once
	sendAt := programNextWindowSlot(program, now)
	// a due time drawn in the last seconds of the sending hours is reached by a
	// tick just after they close. It is sent at its due time then, not moved to
	// the next opening, which can be days later.
	if e.NextDueAt != nil && now.Sub(*e.NextDueAt) >= 0 && now.Sub(*e.NextDueAt) < programTickLagGrace &&
		programNextWindowSlot(program, *e.NextDueAt).Equal(*e.NextDueAt) {
		sendAt = *e.NextDueAt
	}
	if e.NextDueAt != nil && now.Sub(*e.NextDueAt) > programCatchUpGrace {
		drawn := programPickSendTimeAfter(program, now, now, programMinNextSend(program, e.LastSendAt), rng)
		// a draw past the end would lose a template the person can still get
		// before it, the next opening is used then
		if end := program.EndAtTime(); end == nil || drawn.Before(*end) {
			sendAt = drawn
		}
	}
	// nothing is sent at or after the end, the person waits for the program to
	// close then. This is checked before a step is picked, so no hidden
	// campaign is made that would never send
	if end := program.EndAtTime(); end != nil && !sendAt.Before(*end) {
		return s.storeEnrollment(ctx, w, e, map[string]any{
			"next_due_at": *end,
		})
	}
	mixCounts := mix.countsFor(e.RecipientID)
	step, campaign, blocked, err := s.pickSendableStep(ctx, session, program, preferred, loopCount, rng, campaignCache, usableCache, mixCounts)
	if err != nil {
		return err
	}
	if step == nil && len(fallback) > 0 {
		var fallbackBlocked bool
		step, campaign, fallbackBlocked, err = s.pickSendableStep(ctx, session, program, fallback, loopCount, rng, campaignCache, usableCache, mixCounts)
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
			return s.postponeEnrollment(ctx, w, program, e, now)
		}
		// every remaining step is closed and can never be sent in this loop, so
		// the loop is over for this recipient
		if program.LoopModeOrDefault() != model.ProgramLoopLoop {
			return s.completeEnrollment(ctx, w, e, now, sentBefore, nil)
		}
		return s.planNextRound(ctx, w, program, e, roundStart, now, 0, loopCount+1, nil)
	}
	campaignID := campaign.ID.MustGet()
	// the person was deleted while this row was read, there is nobody to send
	// to, so the row leaves the program instead of being sent
	if e.RecipientID == nil {
		// no exit time is kept, an unlinked row holds no times
		return s.storeEnrollment(ctx, w, e, map[string]any{
			"status":        model.ProgramEnrollmentExited,
			"next_due_at":   nil,
			"missing_since": nil,
		})
	}
	recipientID := *e.RecipientID
	var existing *repository.ProgramPersonSend
	if sends != nil {
		existing = sends.PersonSend(campaignID, repository.ProgramPerson{RecipientID: &recipientID, AnonymizedID: e.AnonymizedID})
	} else {
		existing, err = s.ProgramRepository.GetPersonSend(ctx, &campaignID, &recipientID, e.AnonymizedID)
		if err != nil {
			return errs.Wrap(err)
		}
	}
	if existing != nil && (existing.CancelledAt == nil || existing.SentAt != nil || existing.LastAttemptAt != nil) {
		// already given in this campaign. A row the sender took counts as
		// given, it may have gone out before a crash kept sent_at from being
		// written
		return s.planNextRound(ctx, w, program, e, roundStart, now, sentBefore+1, loopCount, nil)
	}
	// the send and the stored progress are two writes. When the progress was not
	// stored after a send, the person is due again with a stale last send, so the
	// newest send is checked before another one is added
	var newest *repository.ProgramNewestSend
	if sends != nil {
		newest = sends.NewestScheduledSend(person)
	} else {
		newest, err = s.ProgramRepository.GetNewestScheduledSend(ctx, e.ProgramID, e.RecipientID, e.AnonymizedID)
		if err != nil {
			return errs.Wrap(err)
		}
	}
	if newest != nil && programMinNextSend(program, &newest.SendAt).After(sendAt) {
		s.Logger.Warnw("program person had a send too recently, planning again",
			"programID", e.ProgramID.String(),
			"enrollmentID", e.ID.String(),
		)
		return s.planNextRound(ctx, w, program, e, roundStart, now, sentBefore, loopCount,
			&programSent{at: newest.SendAt, stepID: newest.StepID})
	}
	switch {
	case existing != nil:
		// a cancelled send, for example of a person who left and came back, is
		// put back in the queue, as a campaign holds one row per person
		if w != nil {
			w.current.reviveID = &existing.ID
			w.current.reviveAt = sendAt
		} else if err := s.ProgramRepository.ReviveCancelledSend(ctx, &existing.ID, sendAt); err != nil {
			return errs.Wrap(err)
		} else {
			programTickStatsFrom(ctx).add(e.ProgramID, 0, 1, 0, 0)
		}
	default:
		if err := s.insertStepSend(ctx, w, campaign, e, recipientID, sendAt); err != nil {
			return err
		}
	}
	sent := &programSent{at: sendAt, stepID: step.ID.MustGet()}
	// later people of the batch see this pick
	mix.record(e.ID, e.RecipientID, sent.stepID, sent.at)
	sentCount := sentBefore + 1
	// the last step of a program that does not loop completes the recipient
	if sentCount >= len(steps) && program.LoopModeOrDefault() != model.ProgramLoopLoop {
		return s.completeEnrollment(ctx, w, e, now, sentCount, sent)
	}
	return s.planNextRound(ctx, w, program, e, roundStart, now, sentCount, loopCount, sent)
}

// insertStepSend adds a scheduled send of a hidden campaign for a person. The
// enrollment may be nil only on a campaign that is not anonymous.
func (s *Program) insertStepSend(
	ctx context.Context,
	w *programWrites,
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
		// the snapshot keeps the department and position for grouped results. A
		// person that can not be read is sent without it. The batch reads its
		// people ahead
		var recipient *model.Recipient
		var err error
		if w != nil {
			recipient = w.recipients[recipientID]
		}
		if recipient == nil {
			recipient, err = s.RecipientRepository.GetByID(ctx, &recipientID, &repository.RecipientOption{})
		}
		if err != nil {
			s.Logger.Warnw("failed to read program recipient for the anonymous snapshot",
				"campaignID", campaignID.String(),
				"error", err,
			)
		} else {
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
	// without lure codes the insert waits for the batch write. A lure code
	// insert redraws its code on a conflict, so it is stored right away
	if w != nil && !allocator.state.enabled {
		w.current.send = campaignRecipient
		return nil
	}
	if err := s.CampaignService.insertScheduledRecipient(ctx, allocator, campaignRecipient); err != nil {
		return errs.Wrap(err)
	}
	if w != nil && w.current != nil {
		w.current.sendStored = true
	}
	if programID, err := campaign.ProgramID.Get(); err == nil {
		programTickStatsFrom(ctx).add(&programID, 0, 1, 0, 0)
	}
	return nil
}

// planNextRound stores the recipient's progress and draws the send time of their
// next round. The next send is never earlier than half an interval after the
// last one. sent is the send just planned, nil when none was.
func (s *Program) planNextRound(
	ctx context.Context,
	w *programWrites,
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
	return s.storeEnrollment(ctx, w, e, fields)
}

// completeEnrollment marks a recipient as done with the program. position is
// how many steps they had in their last loop, sent is the send just planned,
// nil when none was.
func (s *Program) completeEnrollment(
	ctx context.Context,
	w *programWrites,
	e *database.ProgramEnrollment,
	now time.Time,
	position int,
	sent *programSent,
) error {
	fields := map[string]any{
		"status":       model.ProgramEnrollmentCompleted,
		"completed_at": now,
		"next_due_at":  nil,
		"position":     position,
	}
	if sent != nil {
		fields["last_send_at"] = sent.at
		fields["last_step_id"] = sent.stepID
	}
	return s.storeEnrollment(ctx, w, e, fields)
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
		// an anonymous program never saves data
		SaveSubmittedData: nullable.NewNullableWithValue(program.SaveSubmittedDataOn() && !isAnon),
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
	// the lure and training settings are copied before the campaign is linked,
	// so a linked campaign always has them, also when the server stops in
	// between. Without them a training step would send as phishing, so the
	// campaign is removed and the next tick makes it again
	campaign.ID = nullable.NewNullableWithValue(*campaignID)
	if err := s.CampaignService.snapshotLureSettings(ctx, session, campaign); err != nil {
		s.removeNewStepCampaign(ctx, session, campaignID)
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
	programTickStatsFrom(ctx).add(&programID, 1, 0, 0, 0)
	if err := s.applyStepCampaignSettings(ctx, session, &programID, campaignID); err != nil {
		return nil, err
	}
	reloaded, err := s.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{
		IncludeProgramManaged: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return remember(reloaded), nil
}

// removeNewStepCampaign unlinks and deletes a hidden campaign just made whose
// setup failed, before it holds any send. A failure is logged.
func (s *Program) removeNewStepCampaign(ctx context.Context, session *model.Session, campaignID *uuid.UUID) {
	if err := s.ProgramRepository.DeleteProgramCampaignByCampaignID(ctx, campaignID); err != nil {
		s.Logger.Errorw("failed to unlink program campaign", "error", err)
	}
	if err := s.CampaignService.deleteByID(ctx, session, campaignID, true); err != nil {
		s.Logger.Errorw("failed to remove program campaign", "error", err)
	}
}

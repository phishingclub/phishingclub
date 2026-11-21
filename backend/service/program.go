package service

import (
	"context"
	"errors"
	"math/rand"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Program is the service for programs: their lifecycle, the enrollment scheduler
// that feeds hidden campaigns and reporting.
type Program struct {
	Common
	ProgramRepository           *repository.Program
	CampaignRepository          *repository.Campaign
	CampaignRecipientRepository *repository.CampaignRecipient
	RecipientRepository         *repository.Recipient
	RecipientGroupRepository    *repository.RecipientGroup
	CampaignTemplateService     *CampaignTemplate
	// used to check the scope of program bot filtering, pages, webhooks and
	// the target company of a clone
	AllowDenyRepository *repository.AllowDeny
	WebhookRepository   *repository.Webhook
	PageRepository      *repository.Page
	CompanyRepository   *repository.Company
	// CampaignService is reused for the send side helpers (lure allocation,
	// scheduled insert, lure/training snapshot, anonymous snapshot, campaign
	// close and delete).
	CampaignService *Campaign
	// used by the program report and the automated report email
	ReportTemplateRepository      *repository.ReportTemplate
	CompanyReportConfigRepository *repository.CompanyReportConfig
	ReportSendLogRepository       *repository.ReportSendLog
	SMTPConfigService             *SMTPConfiguration
	OptionService                 *Option
	RemoteBrowserExecPath         string
	// SystemSession authorizes the background scheduler, mirroring how the
	// campaign send batch runs.
	SystemSession *model.Session

	lastMembershipAt time.Time
	lastRetentionAt  time.Time
	// closeFailureAuditAt is when a failed close of a program was last
	// audited. It is guarded by closes.mu.
	closeFailureAuditAt map[uuid.UUID]time.Time
	// identityAuditMonth is the UTC month the identity links counted in
	// identityAuditRows were removed in, see removeOldIdentityLinks. Only the
	// scheduler uses them, from the one task runner goroutine.
	identityAuditMonth string
	identityAuditRows  int64
	// failureAudit holds back scheduler failures that repeat on every tick,
	// see auditBackgroundFailure. A test may set it, else it is made once.
	failureAudit     *auditThrottle
	failureAuditOnce sync.Once
	// lifecycleMu keeps a scheduler tick and a close or delete from running at
	// the same time, so no send is added to a program that is being stopped.
	// Always release it with defer, see withLifecycleLock.
	lifecycleMu ctxMutex
	// stopping holds the programs whose close or delete is doing its long part
	// now, after the lifecycle lock was let go, so a second close or delete of
	// the same program waits for it instead of doing the same work at once
	stoppingMu sync.Mutex
	stopping   map[uuid.UUID]bool
	// anonymizing holds the programs an Anonymize All or Anonymize Data runs
	// on now, so two never overlap and a close or delete waits for it, see
	// claimAnonymizing. It is guarded by stoppingMu.
	anonymizing map[uuid.UUID]bool
	// anonymizes is the state of the background pass of the anonymize dates
	anonymizes programAnonymizeState
	// replanning holds the programs whose people this process plans again
	// after an edit now, so the membership pass does not do the same work at
	// once, see claimReplan. It is guarded by stoppingMu.
	replanning map[uuid.UUID]bool
	// enrolling holds the programs a background enrollment runs for now, set
	// when one more run is asked for, see enrollInBackground. It is guarded by
	// stoppingMu.
	enrolling map[uuid.UUID]bool
	// actionFailures holds the last resume or Anonymize Data of each program
	// that failed in the background, see finishAction. It is guarded by
	// stoppingMu.
	actionFailures map[uuid.UUID]*programActionFailure
	// requestedBy names the user who asked for an action on a program, keyed
	// by action and program id, for the audit of its background part. It is
	// guarded by stoppingMu.
	requestedBy map[string]string
	// background counts the work started by a request that runs on after it,
	// so a test can wait for it, see waitForBackground
	background sync.WaitGroup
	// resuming holds the token of the resume each program is in now, so a
	// pause can stop it, see Pause. They are guarded by stoppingMu.
	resuming     map[uuid.UUID]uint64
	resumeTokens uint64
	// closeRetryAt holds back the close of a program at its end after the last
	// try failed, see endProgramsDue. It is guarded by closes.mu.
	closeRetryAt map[uuid.UUID]time.Time
	// closes is the state of the background pass that closes the programs at
	// their end, see endProgramsDueInBackground
	closes programCloseState
	// reports is the state of the automated report sender
	reports programReportState
	// deletes is the state of the background pass that finishes deletes, see
	// finishDeletesInBackground
	deletes programDeleteState
	// resultsCache keeps computed program results, see programResults
	resultsCache programResultsCache
}

// ctxMutex is a lock whose wait can be given up when a context ends. The zero
// value is an unlocked lock.
type ctxMutex struct {
	once sync.Once
	ch   chan struct{}
}

func (m *ctxMutex) init() {
	m.once.Do(func() { m.ch = make(chan struct{}, 1) })
}

// Lock waits for the lock
func (m *ctxMutex) Lock() {
	m.init()
	m.ch <- struct{}{}
}

// LockContext waits for the lock until ctx ends and returns the context error
// when the lock was not taken
func (m *ctxMutex) LockContext(ctx context.Context) error {
	m.init()
	select {
	case m.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Unlock releases the lock
func (m *ctxMutex) Unlock() {
	<-m.ch
}

// withLifecycleLock runs fn while it holds the lifecycle lock. The lock is
// released by a defer, so a panic in fn never leaves it held.
func (s *Program) withLifecycleLock(ctx context.Context, fn func() error) error {
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	return fn()
}

// errProgramBusy refuses a close or delete while another close or delete of
// the same program is under way
var errProgramBusy = errors.New("the program is being closed or deleted, try again shortly")

// errProgramNotEnded stops a close by the scheduler of a program whose end was
// moved later after the scheduler listed it
var errProgramNotEnded = errors.New("the program end has not passed")

// errProgramStopping refuses a change to a program whose close or delete has
// begun
var errProgramStopping = errors.New("the program is being closed or deleted")

// isProgramBusy reports whether err is errProgramBusy or
// errProgramAnonymizing, a refusal that goes away once the other work ends
func isProgramBusy(err error) bool {
	var v errs.ValidationError
	return errors.As(err, &v) && (v.Err == errProgramBusy || v.Err == errProgramAnonymizing)
}

// claimStopping marks a program as doing the long part of a close or delete.
// It reports false when another call already does.
func (s *Program) claimStopping(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.stopping[id] {
		return false
	}
	if s.stopping == nil {
		s.stopping = map[uuid.UUID]bool{}
	}
	s.stopping[id] = true
	return true
}

// isStoppingNow reports whether a close or delete of the program does its long
// part now
func (s *Program) isStoppingNow(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	return s.stopping[id]
}

// releaseStopping ends the claim of claimStopping
func (s *Program) releaseStopping(id uuid.UUID) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	delete(s.stopping, id)
}

// rng returns a random source for one scheduling decision. It draws from one
// shared source behind a lock, which is safe to use from the scheduler and
// request handlers.
func (s *Program) rng() *rand.Rand {
	return rand.New(programRandSource)
}

// Create stores a new program with its steps and assigned groups.
func (s *Program) Create(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
) (_ *uuid.UUID, err error) {
	ae := NewAuditEvent("Program.Create", session)
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
	if err := program.ValidateCreate(); err != nil {
		return nil, errs.Wrap(err)
	}
	// a group that is being deleted is refused in validateScope, the read
	// locks of the groups keep a delete mark from passing before the program
	// is stored, see deleteMarkLock
	unlockGroups := groupDeleteLock.read(nullableIDs(program.RecipientGroupIDs)...)
	defer unlockGroups()
	companyID := optionalID(program.CompanyID)
	unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, companyID)
	if err != nil {
		return nil, err
	}
	defer unlockCompany()
	// a company that does not exist is a wrong request, not a failed write
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
		if _, err := s.CompanyRepository.GetByID(ctx, companyID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, validate.WrapErrorWithField(errors.New("company not found"), "companyID")
			}
			return nil, errs.Wrap(err)
		}
	}
	if err := s.checkNameIsUnique(ctx, program.Name.MustGet(), companyID, nil); err != nil {
		return nil, err
	}
	if err := s.validateScope(ctx, session, companyID, program.Steps, program.RecipientGroupIDs.MustGet()); err != nil {
		return nil, err
	}
	if err := s.validateProgramSettings(ctx, companyID, nil, program); err != nil {
		return nil, err
	}
	now := time.Now()
	applyStartRequest(program, nil, now)
	program.ApplyReportBaseline(nil, now)
	if err := program.ValidateEndAt(program.StartAtTime(), true, now); err != nil {
		return nil, err
	}
	if err := program.ValidateAnonymizeDates(program.IsAnonymousOn(), program.StartAtTime(), program.EndAtTime()); err != nil {
		return nil, err
	}
	// the program, its steps, groups and settings are stored in one
	// transaction, so a program never runs without them
	id, err := s.ProgramRepository.Insert(ctx, program, programChildren(program))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// the program is stored, a delete of its company finds it and a delete of
	// its groups is refused, so the locks are not needed for the enrollment
	unlockGroups()
	unlockCompany()
	ae.Details["id"] = id.String()
	// a program that starts now enrolls its members right away. The program is
	// stored, so when the request ends first the scheduler enrolls them instead
	if start := program.StartAtTime(); start != nil && !start.After(now) {
		s.enrollNowUnlocked(ctx, id)
	}
	s.AuditLogAuthorized(ae)
	return id, nil
}

// programChildren returns the child rows a create or update request sets. A
// list that is not in the request is nil, so it is kept.
func programChildren(p *model.Program) *repository.ProgramChildren {
	children := &repository.ProgramChildren{}
	if len(p.Steps) > 0 {
		children.Steps = p.Steps
	}
	if ids, err := p.RecipientGroupIDs.Get(); err == nil {
		children.RecipientGroupIDs = ids
		if children.RecipientGroupIDs == nil {
			children.RecipientGroupIDs = []*uuid.UUID{}
		}
	}
	if p.AllowDenyIDs.IsSpecified() {
		children.AllowDenyIDs = nullableIDs(p.AllowDenyIDs)
	}
	if p.Webhooks.IsSpecified() {
		children.Webhooks = nullableWebhooks(p.Webhooks)
	}
	return children
}

// checkNameIsUnique returns a validation error when another program of the
// company, or a global one, already has the name. currentID is the program
// being updated, nil on create.
func (s *Program) checkNameIsUnique(
	ctx context.Context,
	name vo.String64,
	companyID *uuid.UUID,
	currentID *uuid.UUID,
) error {
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		s.ProgramRepository.DB,
		database.PROGRAM_TABLE,
		name.String(),
		companyID,
		currentID,
	)
	if err != nil {
		s.Logger.Errorw("failed to check program name uniqueness", "error", err)
		return errs.Wrap(err)
	}
	if !isOK {
		return validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	return nil
}

// UpdateByID updates a program while it runs. Steps keep their ids across an
// edit, so recipients keep their history and only future rounds see the change.
func (s *Program) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Program,
) (err error) {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	ae := NewAuditEvent("Program.UpdateById", session)
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
	if err := incoming.Validate(); err != nil {
		return errs.Wrap(err)
	}
	// the closed check and the write must not race a close or a tick. The lock
	// is held for them only, the people are planned again after it
	var existing *model.Program
	editsSteps := false
	endChanged := false
	var replanMark *time.Time
	replanning := false
	defer func() {
		if replanning {
			s.releaseReplan(*id)
		}
	}()
	if err := s.withLifecycleLock(ctx, func() error {
		var err error
		existing, err = s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if existing.IsClosed() {
			return errs.NewValidationError(errors.New("a closed program cannot be changed"))
		}
		if existing.IsStopping() {
			return errs.NewValidationError(errProgramStopping)
		}
		// a program of a company that is being deleted is not changed
		unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, optionalID(existing.CompanyID))
		if err != nil {
			return err
		}
		defer unlockCompany()
		if err := incoming.ValidateUpdate(existing, time.Now()); err != nil {
			return err
		}
		// a program stays with the company it was created for
		incoming.CompanyID = existing.CompanyID
		companyID := optionalID(existing.CompanyID)
		if name, err := incoming.Name.Get(); err == nil {
			if err := s.checkNameIsUnique(ctx, name, companyID, id); err != nil {
				return err
			}
		}
		// the start can change only until the program has started
		now := time.Now()
		applyStartRequest(incoming, existing, now)
		// a paused program keeps its pause through a new start, it would stay
		// paused as a draft or a scheduled program that nothing resumes
		if existing.IsPaused() && incoming.StartAt.IsSpecified() &&
			(incoming.StartAt.IsNull() || incoming.StartAt.MustGet().After(now)) {
			return validate.WrapErrorWithField(errors.New("A paused program cannot be made a draft or given a later start. Resume it first."), "startAt")
		}
		incoming.ApplyReportBaseline(existing, now)
		endChanged, err = validateEndAtUpdate(incoming, existing, now)
		if err != nil {
			return err
		}
		// anonymity decides how every result is stored, it is fixed once people are
		// enrolled. The enrollments are counted too, as the start mark is stored
		// after the first people are enrolled
		hasPeople := func() (bool, error) {
			if existing.StartedAt != nil {
				return true, nil
			}
			enrolled, err := s.ProgramRepository.CountEnrollments(ctx, id)
			if err != nil {
				return false, errs.Wrap(err)
			}
			return enrolled > 0, nil
		}
		if incoming.IsAnonymous.IsSpecified() && !incoming.IsAnonymous.IsNull() &&
			incoming.IsAnonymousOn() != existing.IsAnonymousOn() {
			started, err := hasPeople()
			if err != nil {
				return err
			}
			if started {
				return validate.WrapErrorWithField(errors.New("anonymous cannot be changed after the program has started"), "isAnonymous")
			}
		}
		// the stored value counts for a field the request leaves out, so a program
		// that saves data can not be made anonymous and the other way around
		anonymous := existing.IsAnonymousOn()
		if incoming.IsAnonymous.IsSpecified() && !incoming.IsAnonymous.IsNull() {
			anonymous = incoming.IsAnonymousOn()
		}
		// anonymous results are cut into months in the program zone. Another zone
		// moves the month edges, and the counts before and after the move tell
		// apart the outcome of the few people near an edge, so the zone is fixed
		// once people are enrolled, the same as anonymity
		if anonymous && programZoneChanged(incoming, existing) {
			started, err := hasPeople()
			if err != nil {
				return err
			}
			if started {
				return validate.WrapErrorWithField(errors.New("the time zone of an anonymous program cannot be changed after it has started"), "timeZone")
			}
		}
		saveData := existing.SaveSubmittedDataOn()
		if incoming.SaveSubmittedData.IsSpecified() && !incoming.SaveSubmittedData.IsNull() {
			saveData = incoming.SaveSubmittedDataOn()
		}
		if err := model.ValidateProgramDataSaving(anonymous, saveData); err != nil {
			return err
		}
		if err := validateAnonymizeDatesUpdate(incoming, existing, anonymous); err != nil {
			return err
		}
		editsSteps = len(incoming.Steps) > 0
		// the steps before the change tell which templates the change removes
		if editsSteps {
			existing.Steps, err = s.ProgramRepository.GetSteps(ctx, id)
			if err != nil {
				return errs.Wrap(err)
			}
		}
		groupIDs := nullableIDs(incoming.RecipientGroupIDs)
		// a group that is being deleted is refused in validateScope, the read
		// locks of the groups keep a delete mark from passing before the edit
		// is stored, see deleteMarkLock
		defer groupDeleteLock.read(groupIDs...)()
		if err := s.validateScope(ctx, session, companyID, incoming.Steps, groupIDs); err != nil {
			return err
		}
		if err := s.validateProgramSettings(ctx, companyID, id, incoming); err != nil {
			return err
		}
		ae.Details["changed"] = s.programChangedFields(ctx, id, incoming)
		// the mark is stored with the edit, so the membership pass plans the
		// people again when the replan below is cut short
		var replanFields map[string]any
		replanFields, replanMark = programReplanMark(existing, incoming, editsSteps, now)
		if len(replanFields) > 0 {
			// the membership pass leaves the program alone while the replan below runs
			replanning = s.claimReplan(*id)
		}
		// the program row, steps, groups and settings change in one transaction
		if err := s.ProgramRepository.UpdateByID(ctx, id, incoming, programChildren(incoming), replanFields); err != nil {
			return errs.Wrap(err)
		}
		if programCampaignSettingsSpecified(incoming) {
			if err := s.syncProgramCampaignSettings(ctx, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// a new end also drops the sends already planned at or after it. They are
	// cancelled after the lock is let go, in chunks. The scheduler plans
	// nothing past the stored end, and a send a failed cancel leaves is
	// cancelled when the program closes at its end
	cancelled := 0
	if endChanged {
		n, _, err := s.cancelSendsFromEnd(context.WithoutCancel(ctx), id)
		cancelled += n
		if err != nil {
			s.Logger.Errorw("failed to cancel program sends after its new end", "programID", id.String(), "error", err)
		}
	}
	// the change is stored, a failed replan only delays when it takes effect.
	// A request that ends does not stop it, the sends of a removed template
	// are cancelled here, see also cancelRemovedStepBacklog
	n, replanErr := s.applyEditToPeople(context.WithoutCancel(ctx), existing, id, editsSteps)
	cancelled += n
	if replanErr != nil {
		s.Logger.Errorw("failed to plan program people again after an edit, the membership pass tries again", "programID", id.String(), "error", replanErr)
	} else if replanMark != nil {
		if err := s.ProgramRepository.ClearReplanPending(context.WithoutCancel(ctx), id, *replanMark); err != nil {
			s.Logger.Errorw("failed to clear the replan mark of a program", "programID", id.String(), "error", err)
		}
	}
	ae.Details["cancelledSends"] = cancelled
	// an edit that starts a program now enrolls its members right away
	if existing.StartedAt == nil {
		s.enrollNowUnlocked(ctx, id)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// programReplanMark returns the columns that mark an edit whose people are
// planned again after the lifecycle lock is let go, see replanPeople, and the
// mark it sets. Only an edit of the steps, a change of the loop mode and an
// end moved later or cleared can leave people that must be planned again, a
// replan cut short for the other changes leaves a valid plan. A program that
// has not started has nobody to plan. Every such edit writes a new mark, so a
// call that clears an older mark leaves this edit marked. When an earlier
// edit left a mark no mark is returned, so the membership pass, which plans
// the people again with the old end stored with the first mark, clears it.
func programReplanMark(existing *model.Program, incoming *model.Program, editsSteps bool, now time.Time) (map[string]any, *time.Time) {
	if existing.StartedAt == nil {
		return nil, nil
	}
	loopChanged := incoming.LoopMode.IsSpecified() && !incoming.LoopMode.IsNull() &&
		incoming.LoopMode.MustGet() != existing.LoopModeOrDefault()
	oldEnd := existing.EndAtTime()
	newEnd := oldEnd
	if incoming.EndAt.IsSpecified() {
		newEnd = incoming.EndAtTime()
	}
	endMovedLater := oldEnd != nil && (newEnd == nil || newEnd.After(*oldEnd))
	if !editsSteps && !loopChanged && !endMovedLater {
		return nil, nil
	}
	fields := map[string]any{}
	m := now.UTC()
	var mark *time.Time
	if existing.ReplanPendingAt == nil {
		mark = &m
	} else if !m.After(existing.ReplanPendingAt.UTC()) {
		// the new mark must differ from the earlier one
		m = existing.ReplanPendingAt.UTC().Add(time.Millisecond)
	}
	fields["replan_pending_at"] = m
	if endMovedLater && existing.ReplanOldEndAt == nil {
		fields["replan_old_end_at"] = oldEnd.UTC()
	}
	return fields, mark
}

// claimReplan marks a program whose people this process plans again after an
// edit now. It reports false when another call already does.
func (s *Program) claimReplan(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.replanning[id] {
		return false
	}
	if s.replanning == nil {
		s.replanning = map[uuid.UUID]bool{}
	}
	s.replanning[id] = true
	return true
}

// releaseReplan ends the claim of claimReplan
func (s *Program) releaseReplan(id uuid.UUID) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	delete(s.replanning, id)
}

// programZoneChanged reports whether an update sets another time zone than
// the stored one. A zone that is not set is UTC.
func programZoneChanged(incoming *model.Program, existing *model.Program) bool {
	if !incoming.TimeZone.IsSpecified() || incoming.TimeZone.IsNull() {
		return false
	}
	return (&model.Program{TimeZone: incoming.TimeZone}).Location().String() != existing.Location().String()
}

// validateAnonymizeDatesUpdate checks the anonymize dates the program has
// after an update with the start and end it will have. A date the request
// leaves out keeps the stored one.
func validateAnonymizeDatesUpdate(incoming *model.Program, existing *model.Program, anonymous bool) error {
	start := existing.StartAtTime()
	if incoming.StartAt.IsSpecified() {
		start = incoming.StartAtTime()
	}
	end := existing.EndAtTime()
	if incoming.EndAt.IsSpecified() {
		end = incoming.EndAtTime()
	}
	merged := &model.Program{AnonymizeAt: existing.AnonymizeAt, DataAnonymizeAt: existing.DataAnonymizeAt}
	if incoming.AnonymizeAt.IsSpecified() {
		merged.AnonymizeAt = incoming.AnonymizeAt
	}
	if incoming.DataAnonymizeAt.IsSpecified() {
		merged.DataAnonymizeAt = incoming.DataAnonymizeAt
	} else if at := existing.DataAnonymizeAtTime(); at != nil && existing.DataAnonymizedAt != nil && !existing.DataAnonymizedAt.Before(*at) {
		// a date that has run is not checked again, a new date is
		merged.DataAnonymizeAt = nullable.NewNullNullable[time.Time]()
	}
	return merged.ValidateAnonymizeDates(anonymous, start, end)
}

// validateEndAtUpdate checks the end date of an update against the start the
// program will have. It reports whether the request sets a new end.
func validateEndAtUpdate(incoming *model.Program, existing *model.Program, now time.Time) (bool, error) {
	start := existing.StartAtTime()
	if incoming.StartAt.IsSpecified() {
		start = incoming.StartAtTime()
	}
	merged := &model.Program{EndAt: existing.EndAt}
	changed := false
	if incoming.EndAt.IsSpecified() {
		merged.EndAt = incoming.EndAt
		before := existing.EndAtTime()
		after := incoming.EndAtTime()
		changed = after != nil && (before == nil || !before.Equal(*after))
	}
	return changed, merged.ValidateEndAt(start, changed, now)
}

// cancelSendsFromEnd cancels the not yet sent sends of a program planned at or
// after its stored end. The ids are read without the lifecycle lock and
// cancelled in chunks, each under the lock for its own write only with a
// pause between them, see writeInChunks. The rest stops when the program is
// closed, being closed or deleted, or gets another end, as that edit cancels
// for its own end. It returns how many sends it cancelled and whether every
// chunk was written. A request that ends does not stop it. The caller must not
// hold the lifecycle lock.
func (s *Program) cancelSendsFromEnd(ctx context.Context, id *uuid.UUID) (int, bool, error) {
	ctx = context.WithoutCancel(ctx)
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return 0, false, errs.Wrap(err)
	}
	end := program.EndAtTime()
	if end == nil {
		return 0, true, nil
	}
	allowed := func(p *model.Program) bool {
		return !p.IsClosed() && !p.IsStopping() && sameTime(p.EndAtTime(), end)
	}
	return s.cancelSendsFrom(ctx, id, *end, allowed)
}

// cancelSendsFrom cancels the not yet sent sends of a program planned at or
// after at, in chunks while allowed reports true, see writeInChunks. It
// returns how many sends it cancelled and whether every chunk was written.
// The caller must not hold the lifecycle lock.
func (s *Program) cancelSendsFrom(
	ctx context.Context,
	id *uuid.UUID,
	at time.Time,
	allowed func(*model.Program) bool,
) (int, bool, error) {
	ids, err := s.ProgramRepository.GetUnsentSendIDsFrom(ctx, id, at)
	if err != nil {
		return 0, false, errs.Wrap(err)
	}
	cancelled := 0
	done, err := s.writeInChunks(ctx, id, len(ids), allowed, func(start, end int) error {
		n, err := s.ProgramRepository.CancelSends(ctx, ids[start:end])
		cancelled += n
		return err
	})
	if err != nil {
		return cancelled, false, errs.Wrap(err)
	}
	return cancelled, done, nil
}

// sameScope reports whether a resource owned by ownerID may be used by a program
// of companyID. Global resources are usable everywhere, company resources only
// by that company.
func sameScope(companyID *uuid.UUID, owner nullable.Nullable[uuid.UUID]) bool {
	if !owner.IsSpecified() || owner.IsNull() {
		return true
	}
	return companyID != nil && *companyID == owner.MustGet()
}

// validateScope checks that the templates and groups a program uses belong to
// its company or are global, so one client's program never phishes another
// client's people.
func (s *Program) validateScope(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	steps []*model.ProgramStep,
	groupIDs []*uuid.UUID,
) error {
	for _, st := range steps {
		templateID := st.CampaignTemplateID.MustGet()
		t, err := s.CampaignTemplateService.GetByID(ctx, session, &templateID, &repository.CampaignTemplateOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(errors.New("A template does not exist"), "steps")
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if !sameScope(companyID, t.CompanyID) {
			return validate.WrapErrorWithField(errors.New("A template belongs to another company"), "steps")
		}
	}
	for _, groupID := range groupIDs {
		g, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(errors.New("a selected group does not exist"), "recipientGroupIDs")
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if !sameScope(companyID, g.CompanyID) {
			return validate.WrapErrorWithField(errors.New("a selected group belongs to another company"), "recipientGroupIDs")
		}
		if g.DeletingAt != nil {
			return validate.WrapErrorWithField(errGroupDeleting, "recipientGroupIDs")
		}
	}
	return nil
}

// DeleteByID starts the delete of a program with its hidden campaigns and all
// their results. Under the lifecycle lock all sending stops and the program is
// marked as being deleted. The hidden campaigns and the program are then
// removed in the background, see finishDeletesInBackground, so a large program
// never runs past the request timeout. The request audits the start, the
// background part audits the finish or a failure as the system. A delete that
// fails half way keeps the mark and the scheduler tries it again.
func (s *Program) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	return s.deleteByID(ctx, session, id, false)
}

// programDeleteBusyWait is the longest DeleteByIDAndWait waits for a close or
// delete of the same program that runs now, and programDeleteBusyPoll is how
// often it looks whether that one has ended
var (
	programDeleteBusyWait = 10 * time.Minute
	programDeleteBusyPoll = 200 * time.Millisecond
)

// DeleteByIDAndWait deletes a program the same as DeleteByID, but removes the
// hidden campaigns and the program before it returns. It is for a caller that
// deletes what the program uses right after, such as a company delete. A
// program that is gone counts as deleted, as a background delete may finish
// it first. When a close or delete of the program runs now, it waits up to
// programDeleteBusyWait for that one to end and then deletes what is left.
func (s *Program) DeleteByIDAndWait(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	deadline := time.Now().Add(programDeleteBusyWait)
	for {
		if err := s.waitNotStopping(ctx, *id, deadline); err != nil {
			return err
		}
		err := s.deleteByID(ctx, session, id, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if !isProgramBusy(err) || !time.Now().Before(deadline) {
			return err
		}
	}
}

// DeleteByIDNow deletes a program the same as DeleteByIDAndWait, but tries
// once and does not wait: when a close, delete or anonymize of the program
// runs now it returns the busy refusal, and the caller tries again later. A
// program that is gone counts as deleted.
func (s *Program) DeleteByIDNow(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	err := s.deleteByID(ctx, session, id, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// waitNotStopping waits until no close, delete or anonymize of the program runs in this
// process, or until the deadline or the end of ctx. Reaching the deadline is
// not an error, the caller then gets errProgramBusy from its own try.
func (s *Program) waitNotStopping(ctx context.Context, id uuid.UUID, deadline time.Time) error {
	for (s.isStoppingNow(id) || s.isAnonymizingNow(id)) && time.Now().Before(deadline) {
		timer := time.NewTimer(programDeleteBusyPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errs.Wrap(ctx.Err())
		case <-timer.C:
		}
	}
	return nil
}

// deleteByID is the work of DeleteByID and DeleteByIDAndWait
func (s *Program) deleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	wait bool,
) (err error) {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	ae := NewAuditEvent("Program.DeleteById", session)
	defer s.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	addDeleteOrigin(ctx, ae)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// the lock is held only to stop all sending and mark the delete. The hidden
	// campaigns are removed after it is let go, so a tick or another program
	// does not wait for them
	if err := s.withLifecycleLock(ctx, func() error {
		return s.markDeleting(ctx, id)
	}); err != nil {
		return err
	}
	if wait {
		defer s.releaseStopping(*id)
		// a caller that gives up does not stop the delete half way. A delete
		// that fails half way keeps the mark and the scheduler finishes it
		if err := s.deleteProgramData(context.WithoutCancel(ctx), session, id); err != nil {
			return err
		}
		s.forgetProgramState(*id, true)
		s.AuditLogAuthorized(ae)
		return nil
	}
	system, err := model.NewSystemSession()
	if err != nil {
		s.releaseStopping(*id)
		return errs.Wrap(err)
	}
	// the background part claims the program again, see finishDeleteOf
	s.releaseStopping(*id)
	s.noteRequester(programActionDelete, *id, session)
	ae.Details["reason"] = "delete started"
	s.AuditLogAuthorized(ae)
	s.finishDeletesInBackground(ctx, system, id)
	return nil
}

// deleteProgramData removes the hidden campaigns of a program marked as being
// deleted, with their results, and then the program. The hidden campaigns are
// only reachable through the program, so they and their results are deleted
// with it, the same as deleting a campaign. A delete that stops half way
// keeps the mark, and a new delete finds the campaigns that are left. The
// caller holds the claim of markDeleting and not the lifecycle lock.
func (s *Program) deleteProgramData(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get program campaigns on delete", "error", err)
		return errs.Wrap(err)
	}
	for _, campaignID := range campaignIDs {
		if err := s.ProgramRepository.DeleteProgramCampaignByCampaignID(ctx, campaignID); err != nil {
			s.Logger.Errorw("failed to unlink program campaign", "error", err, "campaignID", campaignID.String())
			return errs.Wrap(err)
		}
		if err := s.CampaignService.deleteByID(ctx, session, campaignID, true); err != nil {
			s.Logger.Errorw("failed to delete program campaign", "error", err, "campaignID", campaignID.String())
			return errs.Wrap(err)
		}
	}
	if err := s.ProgramRepository.DeleteByID(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// programDeleteRetryEvery is how long the scheduler waits before it tries a
// failed delete of the same program again
const programDeleteRetryEvery = 10 * time.Minute

// programDeleteScanLimit is how many programs marked as being deleted one
// pass of finishDelete looks at to find one to finish
const programDeleteScanLimit = 20

// programDeleteState is the state of the background pass that finishes
// deletes
type programDeleteState struct {
	mu      sync.Mutex
	running bool
	// again asks the running pass to look for programs marked as being
	// deleted once more before it ends
	again bool
	// retryAt holds back a program whose last try failed
	retryAt map[uuid.UUID]time.Time
}

// programCloseState is the state of the background pass that closes the
// programs whose end date has passed and finishes the closes that began
type programCloseState struct {
	mu      sync.Mutex
	running bool
	// again asks the running pass to look for programs to close once more
	// before it ends
	again bool
}

// endProgramsDueInBackground runs endProgramsDue on its own goroutine, so the
// close of a large program never holds up the scheduler tick or the sending
// that runs after it, see closesInBackground
func (s *Program) endProgramsDueInBackground(ctx context.Context, session *model.Session) {
	s.closesInBackground(ctx, session, nil)
}

// closesInBackground runs endProgramsDue on its own goroutine. first, when
// set, is a program a request marked as closing, it is tried even when an
// earlier try failed a short while ago. At most one pass runs at a time, a
// call while a pass runs asks that pass to look again before it ends. ctx may
// end with the caller, the close does not stop with it.
func (s *Program) closesInBackground(ctx context.Context, session *model.Session, first *uuid.UUID) {
	st := &s.closes
	st.mu.Lock()
	if first != nil {
		delete(s.closeRetryAt, *first)
	}
	if st.running {
		st.again = true
		st.mu.Unlock()
		return
	}
	st.running = true
	st.again = false
	st.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	go func() {
		ended := false
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Errorw("recovered from panic while closing programs",
					"panic", r, "stack", string(debug.Stack()))
			}
			if !ended {
				st.mu.Lock()
				st.running = false
				st.mu.Unlock()
			}
		}()
		for {
			s.endProgramsDue(ctx, session, time.Now())
			st.mu.Lock()
			if !st.again {
				st.running = false
				ended = true
				st.mu.Unlock()
				return
			}
			st.again = false
			st.mu.Unlock()
		}
	}()
}

// closeHeldIDs returns the programs whose close waits after a failed try, at
// most programCloseHeldLimit, and drops the ones whose wait is over
func (s *Program) closeHeldIDs(now time.Time) []*uuid.UUID {
	s.closes.mu.Lock()
	defer s.closes.mu.Unlock()
	out := []*uuid.UUID{}
	for id, at := range s.closeRetryAt {
		if !now.Before(at) {
			delete(s.closeRetryAt, id)
			continue
		}
		if len(out) < programCloseHeldLimit {
			id := id
			out = append(out, &id)
		}
	}
	return out
}

// programCloseHeldLimit bounds how many held back programs one read of the
// close pass leaves out
const programCloseHeldLimit = 500

// closeHeld reports whether the close of a program waits after a failed try
func (s *Program) closeHeld(id uuid.UUID, now time.Time) bool {
	s.closes.mu.Lock()
	defer s.closes.mu.Unlock()
	at, ok := s.closeRetryAt[id]
	return ok && now.Before(at)
}

// closeFailed holds back the close of a program after a failed try and
// reports whether the failure is to be audited, at most once per
// programFailureAuditEvery
func (s *Program) closeFailed(id uuid.UUID, now time.Time) bool {
	s.closes.mu.Lock()
	defer s.closes.mu.Unlock()
	if s.closeRetryAt == nil {
		s.closeRetryAt = map[uuid.UUID]time.Time{}
	}
	s.closeRetryAt[id] = now.Add(programCloseRetryEvery)
	if last, ok := s.closeFailureAuditAt[id]; ok && now.Sub(last) < programFailureAuditEvery {
		return false
	}
	if s.closeFailureAuditAt == nil {
		s.closeFailureAuditAt = map[uuid.UUID]time.Time{}
	}
	s.closeFailureAuditAt[id] = now
	return true
}

// closeDone forgets the failures of a program that is closed
func (s *Program) closeDone(id uuid.UUID) {
	s.closes.mu.Lock()
	defer s.closes.mu.Unlock()
	delete(s.closeFailureAuditAt, id)
	delete(s.closeRetryAt, id)
}

// waitForCloses waits until no background close pass runs or ctx ends
func (s *Program) waitForCloses(ctx context.Context) error {
	for {
		s.closes.mu.Lock()
		running := s.closes.running
		s.closes.mu.Unlock()
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// finishDeletesInBackground starts the delete of the programs marked as being
// deleted on its own goroutine, so a large delete never holds up a request or
// the scheduler tick. first, when set, is deleted before the others and is
// tried even when an earlier try failed a short while ago. At most one pass
// runs at a time, so two deletes never run at once. A call while a pass runs
// asks that pass to look again before it ends. The pass ends at the first
// failure, the next tick tries again. ctx may end with the caller, the delete
// does not stop with it.
func (s *Program) finishDeletesInBackground(ctx context.Context, session *model.Session, first *uuid.UUID) {
	st := &s.deletes
	st.mu.Lock()
	if first != nil {
		delete(st.retryAt, *first)
	}
	if st.running {
		st.again = true
		st.mu.Unlock()
		return
	}
	st.running = true
	st.again = false
	st.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	go func() {
		ended := false
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Errorw("recovered from panic while finishing a program delete", "panic", r)
			}
			if !ended {
				st.mu.Lock()
				st.running = false
				st.mu.Unlock()
			}
		}()
		if first != nil {
			s.finishDeleteOf(ctx, session, first, time.Now())
		}
		for {
			for s.finishDelete(ctx, session, time.Now()) {
			}
			st.mu.Lock()
			if !st.again {
				st.running = false
				ended = true
				st.mu.Unlock()
				return
			}
			st.again = false
			st.mu.Unlock()
		}
	}()
}

// waitForDeletes waits until no background delete pass runs or ctx ends
func (s *Program) waitForDeletes(ctx context.Context) error {
	for {
		s.deletes.mu.Lock()
		running := s.deletes.running
		s.deletes.mu.Unlock()
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// finishDelete finishes one delete of a program marked as being deleted, for
// example one a request started or one that stopped half way because the
// server stopped. It picks the oldest program marked as being deleted that no
// other call deletes now and that did not fail within programDeleteRetryEvery,
// see finishDeleteOf. It reports whether a program was deleted. The caller
// must not hold the lifecycle lock.
func (s *Program) finishDelete(ctx context.Context, session *model.Session, now time.Time) bool {
	ids, err := s.ProgramRepository.GetIDsBeingDeleted(ctx, programDeleteScanLimit)
	if err != nil {
		s.Logger.Errorw("failed to list programs being deleted", "error", err)
		programTickStatsFrom(ctx).fail(err)
		return false
	}
	st := &s.deletes
	var id *uuid.UUID
	st.mu.Lock()
	for _, candidate := range ids {
		if at, ok := st.retryAt[*candidate]; ok && now.Before(at) {
			continue
		}
		if s.isStoppingNow(*candidate) || s.isAnonymizingNow(*candidate) {
			continue
		}
		id = candidate
		break
	}
	st.mu.Unlock()
	if id == nil {
		return false
	}
	return s.finishDeleteOf(ctx, session, id, now)
}

// finishDeleteOf deletes one program marked as being deleted. The mark is
// claimed under the lifecycle lock, the campaigns are removed after it is let
// go. It is audited as a delete by the system, a failure at most once per
// programFailureAuditEvery, and a failed program waits programDeleteRetryEvery
// before finishDelete tries it again. It reports whether the program was
// deleted. The caller must not hold the lifecycle lock.
func (s *Program) finishDeleteOf(ctx context.Context, session *model.Session, id *uuid.UUID, now time.Time) bool {
	st := &s.deletes
	ae := NewAuditEvent("Program.DeleteById", session)
	ae.Details["id"] = id.String()
	ae.Details["reason"] = "delete finished"
	if by := s.takeRequester(programActionDelete, *id, false); by != "" {
		ae.Details["requestedBy"] = by
	}
	err := s.withLifecycleLock(ctx, func() error {
		program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		// only a program marked as being deleted is deleted here
		if program.DeletingAt == nil {
			return gorm.ErrRecordNotFound
		}
		return s.markDeleting(ctx, id)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || isProgramBusy(err) {
		return false
	}
	if err == nil {
		err = func() error {
			// a panic in the delete must not leave the claim, or the program
			// would stay half deleted until a restart
			defer s.releaseStopping(*id)
			defer s.invalidateResults(id)
			return s.deleteProgramData(ctx, session, id)
		}()
	}
	st.mu.Lock()
	if err != nil {
		if st.retryAt == nil {
			st.retryAt = map[uuid.UUID]time.Time{}
		}
		st.retryAt[*id] = now.Add(programDeleteRetryEvery)
	} else {
		delete(st.retryAt, *id)
	}
	st.mu.Unlock()
	if err != nil {
		s.Logger.Errorw("failed to finish program delete", "programID", id.String(), "error", err)
		s.auditBackgroundFailure("delete:"+id.String(), ae, err)
		return false
	}
	s.Logger.Infow("program delete finished", "programID", id.String())
	s.forgetProgramState(*id, true)
	s.AuditLogAuthorized(ae)
	return true
}

// markDeleting stops all sending of a program and marks it as being deleted,
// then claims the rest of the delete. The caller holds the lifecycle lock.
// The program is paused so the scheduler adds nothing, and the hidden
// campaigns are paused so the sender sends nothing that is queued.
func (s *Program) markDeleting(ctx context.Context, id *uuid.UUID) error {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if s.isStoppingNow(*id) {
		return errs.NewValidationError(errProgramBusy)
	}
	if s.isAnonymizingNow(*id) {
		return errs.NewValidationError(errProgramAnonymizing)
	}
	now := time.Now().UTC()
	fields := map[string]any{}
	if !program.IsPaused() {
		fields["paused_at"] = now
	}
	if program.DeletingAt == nil {
		fields["deleting_at"] = now
	}
	// the program and its hidden campaigns are paused in one write, so a
	// failure leaves both as they were
	if err := s.ProgramRepository.SetFieldsAndCampaignPause(ctx, id, fields, true); err != nil {
		s.Logger.Errorw("failed to pause program before delete", "error", err)
		return errs.Wrap(err)
	}
	// a resume under way stops, and a failure of an action no longer applies
	s.forgetProgramState(*id, false)
	// nothing else claims the program while the lock is held
	s.claimStopping(*id)
	return nil
}

// GetByID returns a program with its steps and groups.
func (s *Program) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.Program, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.GetById", session)
		ae.Details["id"] = id.String()
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.fillProgramSettings(ctx, program); err != nil {
		return nil, err
	}
	program.SetReadState(time.Now())
	// the step templates are read once for the warnings, the attention and
	// the names
	templates, err := s.stepTemplateInfos(ctx, program)
	if err != nil {
		return nil, err
	}
	program.Warnings = s.healthWarningsWith(ctx, program, templates)
	usable := make(map[uuid.UUID]bool, len(templates))
	for id, t := range templates {
		usable[id] = t.IsUsable
	}
	if err := s.applyListSummariesWith(ctx, []*model.Program{program}, usable); err != nil {
		return nil, err
	}
	program.Names = s.programNames(ctx, program, templates)
	s.applyRunState(program)
	return program, nil
}

// programNames names what a program refers to. A name that can not be read
// is left out, the page then shows the id as it did before.
func (s *Program) programNames(
	ctx context.Context,
	program *model.Program,
	templates map[uuid.UUID]repository.ProgramTemplateInfo,
) *model.ProgramNames {
	names := &model.ProgramNames{
		Templates: make(map[string]string, len(templates)),
		Groups:    []model.ProgramNamedRef{},
		AllowDeny: []model.ProgramNamedRef{},
		Webhooks:  []model.ProgramNamedRef{},
	}
	for id, t := range templates {
		names.Templates[id.String()] = t.Name
	}
	for _, g := range program.RecipientGroups {
		id, err := g.ID.Get()
		if err != nil {
			continue
		}
		if v, err := g.Name.Get(); err == nil {
			names.Groups = append(names.Groups, model.ProgramNamedRef{ID: id.String(), Name: v.String()})
		}
	}
	read := func(kind repository.ProgramNameKind, ids []*uuid.UUID) []model.ProgramNamedRef {
		if len(ids) == 0 {
			return []model.ProgramNamedRef{}
		}
		out, err := s.ProgramRepository.GetNames(ctx, kind, ids)
		if err != nil {
			s.Logger.Warnw("failed to read program names", "error", err)
			return []model.ProgramNamedRef{}
		}
		return out
	}
	one := func(kind repository.ProgramNameKind, v nullable.Nullable[uuid.UUID]) *model.ProgramNamedRef {
		id, err := v.Get()
		if err != nil {
			return nil
		}
		if out := read(kind, []*uuid.UUID{&id}); len(out) > 0 {
			return &out[0]
		}
		return nil
	}
	if ids, err := program.AllowDenyIDs.Get(); err == nil {
		names.AllowDeny = read(repository.ProgramNameAllowDeny, ids)
		// the lists of a program share one mode, the first one tells it
		if len(names.AllowDeny) > 0 {
			id, err := uuid.Parse(names.AllowDeny[0].ID)
			if err == nil {
				if allowed, err := s.ProgramRepository.GetAllowDenyAllowed(ctx, &id); err == nil {
					names.AllowDenyAllowed = &allowed
				} else {
					s.Logger.Warnw("failed to read program list mode", "error", err)
				}
			}
		}
	}
	names.DenyPage = one(repository.ProgramNamePage, program.DenyPageID)
	names.EvasionPage = one(repository.ProgramNamePage, program.EvasionPageID)
	if hooks, err := program.Webhooks.Get(); err == nil {
		ids := make([]*uuid.UUID, 0, len(hooks))
		for _, h := range hooks {
			if h == nil {
				continue
			}
			if id, err := h.WebhookID.Get(); err == nil {
				ids = append(ids, &id)
			}
		}
		names.Webhooks = read(repository.ProgramNameWebhook, ids)
	}
	names.Company = one(repository.ProgramNameCompany, program.CompanyID)
	return names
}

// CountAudience returns how many distinct, active people are in the given groups,
// for the create and edit forms. Groups from another company are rejected.
func (s *Program) CountAudience(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	groupIDs []*uuid.UUID,
) (int, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return 0, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.CountAudience", session)
		if companyID != nil {
			ae.Details["companyId"] = companyID.String()
		}
		s.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}
	if err := model.ValidateProgramRecipientGroupCount(groupIDs); err != nil {
		return 0, err
	}
	if err := s.validateScope(ctx, session, companyID, nil, groupIDs); err != nil {
		return 0, err
	}
	members, err := s.resolveMembership(ctx, &model.Program{
		RecipientGroupIDs: nullable.NewNullableWithValue(groupIDs),
	})
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return len(members), nil
}

// healthWarningsWith lists the health warnings with the step templates
// already read
func (s *Program) healthWarningsWith(
	ctx context.Context,
	program *model.Program,
	templates map[uuid.UUID]repository.ProgramTemplateInfo,
) []string {
	warnings := []string{}
	if program.IsClosed() {
		return warnings
	}
	warnings = append(warnings, templateWarningsFrom(program, templates)...)
	if s.audienceIsEmpty(ctx, program) {
		warnings = append(warnings, "The groups are empty, so nobody is enrolled.")
	}
	return warnings
}

// audienceIsEmpty reports whether the program has nobody to send to. It counts
// instead of resolving every member: active enrollments first, then the group
// member counts only when nobody is enrolled.
func (s *Program) audienceIsEmpty(ctx context.Context, program *model.Program) bool {
	programID := program.ID.MustGet()
	active, err := s.ProgramRepository.CountActiveEnrollments(ctx, &programID)
	if err != nil || active > 0 {
		return false
	}
	if !program.RecipientGroupIDs.IsSpecified() || program.RecipientGroupIDs.IsNull() {
		return true
	}
	for _, groupID := range program.RecipientGroupIDs.MustGet() {
		count, err := s.RecipientGroupRepository.GetRecipientCount(ctx, groupID)
		if err != nil || count > 0 {
			return false
		}
	}
	return true
}

// templateWarnings lists the steps whose template is gone or cannot be sent
func (s *Program) templateWarnings(ctx context.Context, _ *model.Session, program *model.Program) []string {
	return templateWarningsFrom(program, s.stepTemplatesOrNil(ctx, program))
}

// stepTemplatesOrNil reads the step templates, nil when they can not be read
func (s *Program) stepTemplatesOrNil(ctx context.Context, program *model.Program) map[uuid.UUID]repository.ProgramTemplateInfo {
	templates, err := s.stepTemplateInfos(ctx, program)
	if err != nil {
		s.Logger.Warnw("failed to read the program templates", "error", err)
		return nil
	}
	return templates
}

// templateWarningsFrom lists the steps whose template is gone or cannot be
// sent. With templates nil they could not be read and nothing is listed.
func templateWarningsFrom(program *model.Program, templates map[uuid.UUID]repository.ProgramTemplateInfo) []string {
	warnings := []string{}
	if templates == nil {
		return warnings
	}
	for _, st := range program.Steps {
		templateID, err := st.CampaignTemplateID.Get()
		if err != nil {
			continue
		}
		t, ok := templates[templateID]
		if !ok {
			warnings = append(warnings, "A template in this program no longer exists")
			continue
		}
		if !t.IsUsable {
			warnings = append(warnings, "Template '"+t.Name+"' is missing a domain, email, page, or an SMTP configuration or API sender, so it is not sent")
		}
	}
	return warnings
}

// GetAll lists programs for a company.
func (s *Program) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.ProgramOption,
) (*model.Result[model.Program], error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.GetAll", session)
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	now := time.Now()
	result, err := s.ProgramRepository.GetAll(ctx, companyID, options, now)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, p := range result.Rows {
		p.SetReadState(now)
		s.applyRunState(p)
	}
	if err := s.applyListSummaries(ctx, result.Rows); err != nil {
		return nil, err
	}
	return result, nil
}

// applyListSummaries sets the enrollment count, number of simulations, next
// send and attention of programs, as the list and a single read show them
func (s *Program) applyListSummaries(ctx context.Context, programs []*model.Program) error {
	return s.applyListSummariesWith(ctx, programs, nil)
}

// applyListSummariesWith is applyListSummaries with the usability of the step
// templates already read, nil reads it
func (s *Program) applyListSummariesWith(ctx context.Context, programs []*model.Program, known map[uuid.UUID]bool) error {
	programIDs := make([]*uuid.UUID, 0, len(programs))
	for _, p := range programs {
		id := p.ID.MustGet()
		programIDs = append(programIDs, &id)
	}
	summaries, err := s.ProgramRepository.GetListSummaries(ctx, programIDs)
	if err != nil {
		return errs.Wrap(err)
	}
	templateIDs := []uuid.UUID{}
	for _, sum := range summaries {
		templateIDs = append(templateIDs, sum.StepTemplateIDs...)
	}
	usable := known
	if usable == nil {
		usable, err = s.ProgramRepository.GetTemplateUsability(ctx, templateIDs)
		if err != nil {
			return errs.Wrap(err)
		}
	}
	for _, p := range programs {
		sum := summaries[p.ID.MustGet()]
		if sum == nil {
			continue
		}
		p.Enrolled = sum.Enrolled
		p.Stopped = sum.Stopped
		p.EnrolledTotal = sum.Total
		// when a person of an anonymous program had every template would point
		// at the time of their sends, so they count as active while the
		// program is open and as stopped once it closed, like the tiles and the
		// recipient page
		if p.IsAnonymousOn() {
			if completed := sum.People - sum.Enrolled - sum.Stopped; completed > 0 {
				if p.IsClosed() {
					p.Stopped += completed
				} else {
					p.Enrolled += completed
				}
			}
		}
		p.SimulationCount = sum.Simulations
		if p.IsClosed() {
			continue
		}
		// the next send of an anonymous program tells when someone is sent
		if !p.IsAnonymousOn() {
			p.NextSendAt = nextSendBeforeEnd(p, sum.NextDueAt)
		}
		p.AttentionReason = programAttentionReason(p, sum, usable, time.Now())
		p.NeedsAttention = p.AttentionReason != ""
	}
	return nil
}

// nextSendBeforeEnd returns the next send, nil when it falls at or after the
// end date as the program closes before it
func nextSendBeforeEnd(program *model.Program, next *time.Time) *time.Time {
	if next != nil && program.IsEndedBy(*next) {
		return nil
	}
	return next
}

// applyStartRequest turns the startAt and draft fields of a create or update
// request into the start_at to store. A draft has no start. On create, no
// start time means now. A start in the past means now. Once a program has
// started its start time no longer changes.
func applyStartRequest(incoming *model.Program, existing *model.Program, now time.Time) {
	now = now.UTC()
	draft := incoming.Draft != nil && *incoming.Draft
	notDraft := incoming.Draft != nil && !*incoming.Draft
	if existing != nil && existing.StartedAt != nil {
		incoming.StartAt = nullable.Nullable[time.Time]{}
		return
	}
	if draft {
		incoming.StartAt = nullable.NewNullNullable[time.Time]()
		return
	}
	hasStart := incoming.StartAt.IsSpecified() && !incoming.StartAt.IsNull()
	if !hasStart {
		// create starts now unless it is a draft. An update leaves the start
		// alone unless it asks to start or clears it
		switch {
		case existing == nil || notDraft:
			incoming.StartAt = nullable.NewNullableWithValue(now)
		case incoming.StartAt.IsSpecified():
			incoming.StartAt = nullable.NewNullNullable[time.Time]()
		}
		return
	}
	if incoming.StartAt.MustGet().Before(now) {
		incoming.StartAt = nullable.NewNullableWithValue(now)
	}
}

// Start starts a draft or scheduled program now.
func (s *Program) Start(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	ae := NewAuditEvent("Program.Start", session)
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
	// the lock is held for the checks and the start mark only
	if err := s.withLifecycleLock(ctx, func() error {
		program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{WithRecipientGroups: true})
		if err != nil {
			return errs.Wrap(err)
		}
		if program.IsClosed() {
			return errs.NewValidationError(errors.New("a closed program cannot be started"))
		}
		unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, optionalID(program.CompanyID))
		if err != nil {
			return err
		}
		defer unlockCompany()
		if program.IsStopping() {
			return errs.NewValidationError(errProgramStopping)
		}
		// a program cloned to another company has no groups until they are chosen
		if groups, err := program.RecipientGroupIDs.Get(); err != nil || len(groups) == 0 {
			return errs.NewValidationError(errors.New("choose at least one recipient group before starting"))
		}
		if program.StartedAt != nil {
			return errs.NewValidationError(errors.New("the program has already started"))
		}
		now := time.Now().UTC()
		if end := program.EndAtTime(); end != nil && !end.After(now) {
			return errs.NewValidationError(errors.New("The end has passed. Change or clear the end first."))
		}
		// a start that has passed but that the scheduler has not picked up yet
		// starts now as well, it keeps its start time
		if start := program.StartAtTime(); start != nil && !start.After(now) {
			return nil
		}
		if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{"start_at": now}); err != nil {
			return errs.Wrap(err)
		}
		return nil
	}); err != nil {
		return err
	}
	// the members are enrolled in the background, so the people list shows
	// their planned sends without waiting for the next tick
	s.noteRequester(programActionStart, *id, session)
	s.enrollInBackground(id)
	s.AuditLogAuthorized(ae)
	return nil
}

// Pause halts enrollment and advancement and allows step edits. A pause of a
// paused program whose resume is under way stops that resume, the program
// stays paused.
func (s *Program) Pause(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	ae := NewAuditEvent("Program.Pause", session)
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
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsClosed() {
		return errs.NewValidationError(errors.New("a closed program cannot be paused"))
	}
	if program.IsStopping() {
		return errs.NewValidationError(errProgramStopping)
	}
	if program.IsPaused() {
		// a resume under way still holds the pause, it is stopped so the
		// program stays paused
		if s.stopResume(*id) {
			ae.Details["reason"] = "resume stopped"
			s.AuditLogAuthorized(ae)
			return nil
		}
		return errs.NewValidationError(errProgramAlreadyPaused)
	}
	now := time.Now().UTC()
	// a draft or scheduled program sends nothing yet, so only a running one is
	// paused, the same as a campaign
	if program.StatusAt(now) != model.ProgramStatusActive {
		return errs.NewValidationError(errProgramNotRunning)
	}
	// the program and its hidden campaigns are paused in one write, the
	// campaign pause holds the sends that are already queued
	if err := s.ProgramRepository.SetFieldsAndCampaignPause(ctx, id, map[string]any{"paused_at": now}, true); err != nil {
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// Resume checks that a program can be resumed and resumes it in the
// background, see resumeProgram. A second resume while one runs is refused.
// The background part audits how it ends, a failure is shown on the program,
// see ActionFailure, and the program then stays paused.
func (s *Program) Resume(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	ae := NewAuditEvent("Program.Resume", session)
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
	system, err := model.NewSystemSession()
	if err != nil {
		return errs.Wrap(err)
	}
	var token uint64
	if err := s.withLifecycleLock(ctx, func() error {
		program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if err := checkResumable(program); err != nil {
			return err
		}
		unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, optionalID(program.CompanyID))
		if err != nil {
			return err
		}
		defer unlockCompany()
		if s.isResuming(*id) {
			return errs.NewValidationError(errProgramResuming)
		}
		// a pause while the resume runs stops it, see Pause
		token = s.startResume(*id)
		return nil
	}); err != nil {
		return err
	}
	s.clearActionFailure(*id)
	s.noteRequester(programActionResume, *id, session)
	ae.Details["reason"] = "resume started"
	s.AuditLogAuthorized(ae)
	s.runInBackground(*id, programActionResume, system, "Program.Resume", func(ctx context.Context) error {
		defer s.endResume(*id, token)
		return s.resumeProgram(ctx, id, token)
	})
	return nil
}

// errProgramClosedResume refuses a resume of a closed program
var errProgramClosedResume = errors.New("a closed program cannot be resumed")

// errProgramResuming refuses a resume of a program that is being resumed
var errProgramResuming = errors.New("the program is being resumed")

// resumeProgram clears the pause so the scheduler picks the program up again.
// The members and the enrollments are read and the new people planned before
// the lock is taken. The checks and the people who return or left are decided
// under the lock. The new people and the people who left are then written,
// their sends cancelled, and the held sends and the people who came due
// during the pause are then planned again in chunks, each under the lock for
// its own write only, while the program is still paused and not changed, so
// nothing is sent on an old time and an edit in between is not overwritten.
// The pause is cleared under the lock at the end. A pause, close, delete or
// edit in between stops the rest and the program stays paused, a new resume
// plans again. A failed membership read stops the resume too.
func (s *Program) resumeProgram(ctx context.Context, id *uuid.UUID, token uint64) error {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	now := time.Now()
	var program *model.Program
	var membership programResumeMembership
	// the members, the enrollments and the new people are read and planned
	// before the lock is taken. A program edited in between is planned again
	for attempt := 1; ; attempt++ {
		plan, err := s.resumeMembers(ctx, id, now)
		if err != nil {
			s.Logger.Errorw("failed to resolve membership on resume", "programID", id.String(), "error", err)
			return err
		}
		changed := false
		if err := s.withLifecycleLock(ctx, func() error {
			var err error
			program, err = s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
			if err != nil {
				return errs.Wrap(err)
			}
			if err := checkResumable(program); err != nil {
				return err
			}
			if !s.resumeLive(*id, token) {
				return errs.NewValidationError(errProgramPausedOnResume)
			}
			if program.StartedAt == nil && plan == nil {
				return nil
			}
			membership, changed, err = s.resolveMembershipOnResume(ctx, id, plan, now)
			if err != nil {
				s.Logger.Errorw("failed to resolve membership on resume", "programID", id.String(), "error", err)
			}
			return err
		}); err != nil {
			return err
		}
		if !changed {
			break
		}
		if attempt == programResumePlanTries {
			return errs.NewValidationError(errProgramChangedOnResume)
		}
	}
	stillPaused := func(p *model.Program) bool {
		return checkResumable(p) == nil && sameTime(p.UpdatedAt, program.UpdatedAt) && s.resumeLive(*id, token)
	}
	// people who left the groups lose their held sends before they are let out.
	// The program stays paused meanwhile, so nothing is sent
	if program.StartedAt != nil {
		done, err := s.finishResumeMembership(ctx, id, membership, now, stillPaused)
		if err != nil {
			return errs.Wrap(err)
		}
		if !done {
			return s.resumeStopped(ctx, id, token)
		}
	}
	// held sends wait for an open window slot
	moves, err := s.heldSendMoves(ctx, program, now)
	if err != nil {
		return err
	}
	done, err := s.writeInChunks(ctx, id, len(moves), stillPaused, func(start, end int) error {
		return s.ProgramRepository.MoveSends(ctx, moves[start:end])
	})
	if err != nil {
		return errs.Wrap(err)
	}
	if !done {
		return s.resumeStopped(ctx, id, token)
	}
	// people whose send came due during the pause start a fresh round now,
	// drawn over the spread, instead of all being sent at once
	plans, err := s.resumeCatchUpPlans(ctx, program, now)
	if err != nil {
		return err
	}
	done, err = s.writeInChunks(ctx, id, len(plans), stillPaused, func(start, end int) error {
		return s.ProgramRepository.PlanEnrollments(ctx, plans[start:end], nil, model.ProgramEnrollmentActive)
	})
	if err != nil {
		return errs.Wrap(err)
	}
	if !done {
		return s.resumeStopped(ctx, id, token)
	}
	// a held send moved to or planned at or after the end is cancelled while
	// the program is still paused, in chunks with the lock let go between them
	if end := program.EndAtTime(); end != nil {
		_, done, err := s.cancelSendsFrom(ctx, id, *end, stillPaused)
		if err != nil {
			return err
		}
		if !done {
			return s.resumeStopped(ctx, id, token)
		}
	}
	startedAt := program.StartedAt
	if err := s.withLifecycleLock(ctx, func() error {
		current, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if err := checkResumable(current); err != nil {
			return err
		}
		if !s.resumeLive(*id, token) {
			return errs.NewValidationError(errProgramPausedOnResume)
		}
		if !stillPaused(current) {
			return errs.NewValidationError(errProgramChangedOnResume)
		}
		startedAt = current.StartedAt
		// the program and its hidden campaigns resume in one write
		return errs.Wrap(s.ProgramRepository.SetFieldsAndCampaignPause(ctx, id, map[string]any{"paused_at": nil}, false))
	}); err != nil {
		return err
	}
	// a program paused before its first enrollment enrolls its members now
	if startedAt == nil {
		s.enrollNowUnlocked(ctx, id)
	}
	return nil
}

// programResumePlanTries is how many times a resume plans its membership
// before it gives up on a program that keeps changing
const programResumePlanTries = 3

// errProgramChangedOnResume refuses a resume of a program that was changed
// while it was planned again
var errProgramChangedOnResume = errors.New("the program changed while it was resumed, try again")

// errProgramPausedOnResume ends a resume that a pause stopped
var errProgramPausedOnResume = errors.New("the program was paused while it was resumed, it stays paused")

// startResume marks a resume of a program under way and returns its token.
// A newer resume of the same program takes the mark over.
func (s *Program) startResume(id uuid.UUID) uint64 {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.resuming == nil {
		s.resuming = map[uuid.UUID]uint64{}
	}
	s.resumeTokens++
	s.resuming[id] = s.resumeTokens
	return s.resumeTokens
}

// resumeLive reports whether the resume with the token still runs and was
// not stopped by a pause
func (s *Program) resumeLive(id uuid.UUID, token uint64) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	return s.resuming[id] == token
}

// endResume removes the mark of startResume when it is still the one of the
// token
func (s *Program) endResume(id uuid.UUID, token uint64) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.resuming[id] == token {
		delete(s.resuming, id)
	}
}

// stopResume stops the resume of a program under way and reports whether
// there was one. The caller holds the lifecycle lock, so the resume can not
// let the pause go in between: it checks its mark under the lock before it
// does.
func (s *Program) stopResume(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if _, ok := s.resuming[id]; !ok {
		return false
	}
	delete(s.resuming, id)
	return true
}

// sameTime reports whether two optional times are both unset or equal
func sameTime(a *time.Time, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// checkResumable refuses a resume of a program that is closed, being closed
// or deleted, or not paused
func checkResumable(program *model.Program) error {
	if program.IsClosed() {
		return errs.NewValidationError(errProgramClosedResume)
	}
	if program.IsStopping() {
		return errs.NewValidationError(errProgramStopping)
	}
	if !program.IsPaused() {
		return errs.NewValidationError(errProgramNotPaused)
	}
	return nil
}

// resumeStopped returns why a resume stopped between its chunks: the program
// is gone, closed, being closed or deleted, no longer paused, paused again or
// changed
func (s *Program) resumeStopped(ctx context.Context, id *uuid.UUID, token uint64) error {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if err := checkResumable(program); err != nil {
		return err
	}
	if !s.resumeLive(*id, token) {
		return errs.NewValidationError(errProgramPausedOnResume)
	}
	return errs.NewValidationError(errProgramChangedOnResume)
}

// resumeCatchUpPlans returns a new round from now for every active person
// whose send came due during the pause
func (s *Program) resumeCatchUpPlans(ctx context.Context, program *model.Program, now time.Time) ([]repository.EnrollmentPlan, error) {
	id := program.ID.MustGet()
	enrollments, err := s.ProgramRepository.GetEnrollmentPlansByProgramID(ctx, &id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	plans := []repository.EnrollmentPlan{}
	for _, e := range enrollments {
		if e.Status != model.ProgramEnrollmentActive || e.NextDueAt == nil || e.NextDueAt.After(now) {
			continue
		}
		due := programPickSendTimeAfter(program, now, now, programMinNextSend(program, e.LastSendAt), s.rng())
		plans = append(plans, repository.EnrollmentPlan{ID: e.ID, RoundStartAt: now, NextDueAt: due})
	}
	return plans, nil
}

// errProgramAlreadyPaused, errProgramNotPaused and errProgramNotRunning
// refuse a pause of a paused program, a resume of one that is not paused and
// a pause of one that has not started
var (
	errProgramAlreadyPaused = errors.New("program is already paused")
	errProgramNotPaused     = errors.New("program is not paused")
	errProgramNotRunning    = errors.New("only a running program can be paused")
)

// Close ends a program for good. Sending stops at once and the program is
// marked as closing. The background close pass then cancels the pending sends
// and closes the hidden campaigns, while all results stay readable. A closed
// program cannot be resumed or edited, only deleted.
func (s *Program) Close(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	ae := NewAuditEvent("Program.Close", session)
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
	// the request only stops all sending and marks the close, the rest is
	// done by the background close pass, which audits how it ends
	defer s.invalidateResults(id)
	if err := s.withLifecycleLock(ctx, func() error {
		return s.markClosing(ctx, id, false)
	}); err != nil {
		return err
	}
	// the background pass claims the program again, see closeProgram
	s.releaseStopping(*id)
	system, err := model.NewSystemSession()
	if err != nil {
		return errs.Wrap(err)
	}
	s.noteRequester(programActionClose, *id, session)
	ae.Details["reason"] = "close started"
	s.AuditLogAuthorized(ae)
	s.closesInBackground(ctx, system, id)
	return nil
}

// errProgramClosed refuses a close of a program that is closed
var errProgramClosed = errors.New("program is already closed")

// closeProgram closes a program that is not closed: sending stops, pending
// sends are cancelled and the hidden campaigns are closed, while all results
// stay. The caller checks access and must not hold the lifecycle lock.
//
// The lock is held only for the short writes that stop all sending and mark
// the close. The sends not yet sent are cancelled, the people stopped and the
// hidden campaigns closed after the lock is let go, in chunks with a pause
// between them, so a large program does not hold up the scheduler, other
// programs or other writers. Every step can run again and closed_at is
// written last, so a close that stops half way leaves a paused program marked
// as closing, which the scheduler or a new close finishes. A request that
// ends does not stop the long part.
// atEnd is set by the scheduler: the program is then closed only when its end
// has still passed or its close began, so an end moved later moments before
// is kept.
func (s *Program) closeProgram(ctx context.Context, session *model.Session, id *uuid.UUID, reason string, atEnd bool) error {
	// the cached results may no longer hold
	defer s.invalidateResults(id)
	if err := s.withLifecycleLock(ctx, func() error {
		return s.markClosing(ctx, id, atEnd)
	}); err != nil {
		return err
	}
	defer s.releaseStopping(*id)
	ctx = context.WithoutCancel(ctx)
	// the program and its campaigns are paused and marked as closing, so the
	// scheduler adds nothing and the sender sends nothing while this runs
	if err := s.ProgramRepository.CancelAllUnsent(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	if err := repository.PauseBetweenWrites(ctx); err != nil {
		return err
	}
	if err := s.ProgramRepository.StopEnrollments(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, campaignID := range campaignIDs {
		campaign, err := s.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{
			IncludeProgramManaged: true,
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if !campaign.IsActive() {
			continue
		}
		err = s.CampaignService.closeCampaign(ctx, session, campaignID, campaign, reason)
		if err != nil && !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
			s.Logger.Errorw("failed to close program campaign", "error", err, "campaignID", campaignID.String())
			return errs.Wrap(err)
		}
	}
	// the pseudonyms link people to their results, a closed program needs no
	// link. The send times of an anonymous program are random per person and
	// match the send times of the rows, so they would link a person to a
	// pseudonym, and go too.
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if err := s.ProgramRepository.ClearEnrollmentPseudonyms(ctx, id, program.IsAnonymousOn()); err != nil {
		return errs.Wrap(err)
	}
	if program.IsAnonymousOn() {
		at := time.Now().UTC()
		if program.ClosingAt != nil {
			at = *program.ClosingAt
		}
		if err := s.unlinkAnonymousClose(ctx, id, program.Location(), at); err != nil {
			return err
		}
	}
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
		"closed_at":  time.Now().UTC(),
		"paused_at":  nil,
		"closing_at": nil,
	}); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// unlinkAnonymousClose removes what still links a person to outcomes once an
// anonymous program is closed and its sends are anonymized. While open the
// program keeps one pseudonym per person for all of its sends, so a closed
// program would keep a full history per pseudonym. The sends that were never
// sent are deleted, every person gets a new pseudonym per month of the
// program time zone, the department and title on the sends are cleared and
// the enrollment dates are rounded to their month. The month counts stay the
// same, see RotateSendPseudonyms. Each step is paced and can run again, so a
// close that stops half way is finished by the next close or tick. Every row
// rewritten gets the update time at, the time the close began, so the update
// times hold nothing about a person.
func (s *Program) unlinkAnonymousClose(ctx context.Context, id *uuid.UUID, loc *time.Location, at time.Time) error {
	if err := s.ProgramRepository.DeleteUnsentSends(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	if err := repository.PauseBetweenWrites(ctx); err != nil {
		return err
	}
	pseudonyms, err := s.ProgramRepository.GetClosePseudonyms(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	if err := s.ProgramRepository.RotateSendPseudonyms(ctx, id, pseudonyms, loc, at); err != nil {
		return errs.Wrap(err)
	}
	if err := repository.PauseBetweenWrites(ctx); err != nil {
		return err
	}
	if err := s.ProgramRepository.BlurEnrollments(ctx, id, loc, at); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// markClosing stops all sending of a program and marks it as closing, then
// claims the rest of the close. The caller holds the lifecycle lock. The
// program is paused so the scheduler adds nothing, and its open hidden
// campaigns are paused so the sender sends nothing that is queued. Both are
// one write. The sends are cancelled and the people stopped by closeProgram
// after the lock is let go.
func (s *Program) markClosing(ctx context.Context, id *uuid.UUID, atEnd bool) error {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsClosed() {
		return errs.NewValidationError(errProgramClosed)
	}
	if program.DeletingAt != nil {
		return errs.NewValidationError(errProgramStopping)
	}
	if s.isStoppingNow(*id) {
		return errs.NewValidationError(errProgramBusy)
	}
	if s.isAnonymizingNow(*id) {
		return errs.NewValidationError(errProgramAnonymizing)
	}
	now := time.Now().UTC()
	if atEnd && program.ClosingAt == nil && !program.IsEndedBy(now) {
		return errProgramNotEnded
	}
	fields := map[string]any{}
	if !program.IsPaused() {
		fields["paused_at"] = now
	}
	if program.ClosingAt == nil {
		fields["closing_at"] = now
	}
	if err := s.ProgramRepository.SetFieldsAndCampaignPause(ctx, id, fields, true); err != nil {
		return errs.Wrap(err)
	}
	// a resume under way stops, and a failure of an action no longer applies
	s.forgetProgramState(*id, false)
	// nothing else claims the program while the lock is held
	s.claimStopping(*id)
	return nil
}

// enrollmentPseudonym returns the pseudonym of an enrollment, creating it on
// first use
func (s *Program) enrollmentPseudonym(
	ctx context.Context,
	e *database.ProgramEnrollment,
) (uuid.UUID, error) {
	if e.AnonymizedID != nil {
		return *e.AnonymizedID, nil
	}
	if err := s.ProgramRepository.SetEnrollmentPseudonymIfEmpty(ctx, e.ID, uuid.New()); err != nil {
		return uuid.UUID{}, errs.Wrap(err)
	}
	// read it back so a concurrent writer and this call agree on one pseudonym
	stored, err := s.ProgramRepository.GetEnrollmentByProgramRecipient(ctx, e.ProgramID, e.RecipientID)
	if err != nil {
		return uuid.UUID{}, errs.Wrap(err)
	}
	if stored.AnonymizedID == nil {
		return uuid.UUID{}, errs.Wrap(errors.New("program pseudonym was not stored"))
	}
	e.AnonymizedID = stored.AnonymizedID
	return *stored.AnonymizedID, nil
}

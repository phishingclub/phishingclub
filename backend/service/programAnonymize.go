package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// the refusals of the anonymize actions of a program
var (
	errProgramAlreadyAnonymized     = errors.New("the program is already anonymized")
	errProgramDataAlreadyAnonymized = errors.New("the data of the program is already anonymized")
	errProgramAnonymizeNotClosed    = errors.New("close the program before anonymizing it")
	errProgramAnonymizeAnonymous    = errors.New("an anonymous program is anonymized when it closes")
	// errProgramAnonymizing refuses a close, delete or second anonymize while
	// an anonymize of the program runs
	errProgramAnonymizing = errors.New("the program is being anonymized or is importing reported emails, try again shortly")
	// errProgramReportedAnonymized refuses a reported CSV on an anonymized
	// program, its sends no longer name a person to match
	errProgramReportedAnonymized = errors.New("reported emails cannot be imported on an anonymized program")
)

// isProgramAnonymizeSkip reports a refusal the anonymize tasks pass over: the
// program is done, not closed yet or busy with a close, delete or another
// anonymize
func isProgramAnonymizeSkip(err error) bool {
	var v errs.ValidationError
	if !errors.As(err, &v) {
		return false
	}
	for _, e := range []error{
		errProgramAlreadyAnonymized,
		errProgramDataAlreadyAnonymized,
		errProgramAnonymizeNotClosed,
		errProgramAnonymizeAnonymous,
		errProgramBusy,
		errProgramStopping,
		errProgramAnonymizing,
	} {
		if v.Err == e {
			return true
		}
	}
	return false
}

// AnonymizeByID replaces the people of a closed program with pseudonyms in
// every result, the same as Anonymize All of a campaign. Each person gets one
// pseudonym for every send of the program, so the results still count each
// person once. The work goes on when the request ends.
func (s *Program) AnonymizeByID(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	ae := NewAuditEvent("Program.Anonymize", session)
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
	// the request checks the program and sets the anonymize date to now, so
	// the background pass of the anonymize dates runs it, see
	// anonymizeProgram. That pass finishes a run that stops half way.
	if err := s.withLifecycleLock(ctx, func() error {
		p, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if p.DeletingAt != nil || p.ClosingAt != nil {
			return errs.NewValidationError(errProgramStopping)
		}
		if err := anonymizeAllCheck(p); err != nil {
			return errs.NewValidationError(err)
		}
		if s.isStoppingNow(*id) {
			return errs.NewValidationError(errProgramBusy)
		}
		if s.isAnonymizingNow(*id) {
			return errs.NewValidationError(errProgramAnonymizing)
		}
		if at := p.AnonymizeAtTime(); at == nil || at.After(time.Now()) {
			if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
				"anonymize_at": time.Now().UTC(),
			}); err != nil {
				return errs.Wrap(err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s.clearActionFailure(*id)
	s.noteRequester(programActionAnonymize, *id, session)
	ae.Details["reason"] = "anonymize started"
	s.AuditLogAuthorized(ae)
	s.anonymizesInBackground(context.Background(), system, id)
	return nil
}

// anonymizeAllCheck refuses Anonymize All of a program that is anonymized,
// anonymous or not closed
func anonymizeAllCheck(p *model.Program) error {
	switch {
	case p.IsAnonymized():
		return errProgramAlreadyAnonymized
	case p.IsAnonymousOn():
		return errProgramAnonymizeAnonymous
	case !p.IsClosed():
		return errProgramAnonymizeNotClosed
	}
	return nil
}

// AnonymizeDataByID removes the submitted data, IP addresses, user agents and
// browser metadata of a program while the people stay linked, the same as
// Anonymize Data of a campaign. An open program keeps tracking and can be run
// again for newer events. The request checks and claims the program, the work
// runs in the background. A failure is shown on the program, see
// ActionFailure, and a new run passes over the campaigns already done.
func (s *Program) AnonymizeDataByID(ctx context.Context, session *model.Session, id *uuid.UUID) (err error) {
	ae := NewAuditEvent("Program.AnonymizeData", session)
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
	if err := s.claimAnonymizeData(ctx, id); err != nil {
		return err
	}
	// the mark lets the background pass finish a run that fails or that a
	// restart stops, see GetIDsToDataAnonymize
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
		"data_anonymizing_at": time.Now().UTC(),
	}); err != nil {
		s.releaseAnonymizing(*id)
		return errs.Wrap(err)
	}
	s.clearActionFailure(*id)
	s.noteRequester(programActionAnonymizeData, *id, session)
	ae.Details["reason"] = "anonymize data started"
	s.AuditLogAuthorized(ae)
	s.runInBackground(*id, programActionAnonymizeData, system, "Program.AnonymizeData", func(ctx context.Context) error {
		defer s.releaseAnonymizing(*id)
		_, err := s.anonymizeClaimedProgramData(ctx, id)
		return err
	})
	return nil
}

// programAnonymizeRunTimeout bounds one background pass of the anonymize
// dates. A program cut short is finished by a later pass.
const programAnonymizeRunTimeout = 2 * time.Hour

// programAnonymizeState is the state of the background pass of the anonymize
// dates, see AnonymizeDueInBackground
type programAnonymizeState struct {
	mu      sync.Mutex
	running bool
	// again asks the running pass to look for programs once more before it
	// ends
	again bool
	// retryAt and dataRetryAt hold back a program whose last Anonymize All or
	// data anonymize failed, so a lasting failure does not redo the whole
	// program on every tick
	retryAt     map[uuid.UUID]time.Time
	dataRetryAt map[uuid.UUID]time.Time
}

// programAnonymizeRetryEvery is how long a program whose anonymize failed
// waits before the background pass tries it again
const programAnonymizeRetryEvery = 10 * time.Minute

// programAnonymizeHeldLimit bounds how many held back programs one listing
// leaves out
const programAnonymizeHeldLimit = 500

// held returns the programs in m that still wait and drops the ones whose
// wait is over
func (st *programAnonymizeState) held(m *map[uuid.UUID]time.Time, now time.Time) []*uuid.UUID {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := []*uuid.UUID{}
	for id, at := range *m {
		if !now.Before(at) {
			delete(*m, id)
			continue
		}
		if len(out) < programAnonymizeHeldLimit {
			id := id
			out = append(out, &id)
		}
	}
	return out
}

// failed holds back a program after a failed try, ok forgets an earlier failure
func (st *programAnonymizeState) failed(m *map[uuid.UUID]time.Time, id uuid.UUID, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if *m == nil {
		*m = map[uuid.UUID]time.Time{}
	}
	(*m)[id] = now.Add(programAnonymizeRetryEvery)
}

func (st *programAnonymizeState) ok(m *map[uuid.UUID]time.Time, id uuid.UUID) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(*m, id)
}

// AnonymizeDueInBackground starts the pass that runs the anonymize dates that
// passed and finishes an Anonymize All that stopped half way, on its own
// goroutine, so a large program never holds up the task runner tick. At most
// one pass runs at a time, a call while one runs does nothing. The pass runs
// the programs one at a time and stops when ctx ends.
func (s *Program) AnonymizeDueInBackground(ctx context.Context, session *model.Session) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.AnonymizeDueInBackground", session)
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	s.anonymizesInBackground(ctx, session, nil)
	return nil
}

// anonymizesInBackground starts the background pass of the anonymize dates.
// first, when set, is a program a request set to run now, it is tried even
// when an earlier try failed a short while ago. A call while a pass runs asks
// that pass to look again before it ends. Each round of the pass stops after
// programAnonymizeRunTimeout or when ctx ends.
func (s *Program) anonymizesInBackground(ctx context.Context, session *model.Session, first *uuid.UUID) {
	st := &s.anonymizes
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
	go func() {
		ended := false
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Errorw("recovered from panic while anonymizing programs", "panic", r)
			}
			if !ended {
				st.mu.Lock()
				st.running = false
				st.mu.Unlock()
			}
		}()
		for {
			func() {
				ctx, cancel := context.WithTimeout(ctx, programAnonymizeRunTimeout)
				defer cancel()
				if err := s.HandleDataAnonymizePrograms(ctx, session); err != nil {
					s.Logger.Errorw("failed to anonymize the data of programs", "error", err)
				}
				if err := s.HandleAnonymizePrograms(ctx, session); err != nil {
					s.Logger.Errorw("failed to anonymize programs", "error", err)
				}
			}()
			st.mu.Lock()
			if !st.again || ctx.Err() != nil {
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

// waitForAnonymizes waits until no background anonymize pass runs or ctx
// ends
func (s *Program) waitForAnonymizes(ctx context.Context) error {
	for {
		s.anonymizes.mu.Lock()
		running := s.anonymizes.running
		s.anonymizes.mu.Unlock()
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

// programAnonymizeRunning reports whether an Anonymize All of the program
// began and did not finish
func programAnonymizeRunning(p *model.Program) bool {
	return p.AnonymizingAt != nil && p.AnonymizedAt == nil
}

// claimAnonymizing marks a program as being anonymized now. It reports false
// when an anonymize of it already runs.
func (s *Program) claimAnonymizing(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.anonymizing[id] {
		return false
	}
	if s.anonymizing == nil {
		s.anonymizing = map[uuid.UUID]bool{}
	}
	s.anonymizing[id] = true
	return true
}

// isAnonymizingNow reports whether an anonymize of the program runs now
func (s *Program) isAnonymizingNow(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	return s.anonymizing[id]
}

// releaseAnonymizing ends the claim of claimAnonymizing
func (s *Program) releaseAnonymizing(id uuid.UUID) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	delete(s.anonymizing, id)
}

// HandleAnonymizePrograms anonymizes the closed programs whose anonymize date
// has passed and finishes an Anonymize All that stopped half way.
func (s *Program) HandleAnonymizePrograms(ctx context.Context, session *model.Session) error {
	ae := NewAuditEvent("Program.HandleAnonymizePrograms", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	st := &s.anonymizes
	ids, err := s.ProgramRepository.GetIDsToAnonymize(ctx, time.Now(), st.held(&st.retryAt, time.Now()))
	if err != nil {
		return errs.Wrap(err)
	}
	done, failed := []string{}, []string{}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		_, err := s.anonymizeProgram(ctx, session, id)
		if isProgramAnonymizeSkip(err) {
			continue
		}
		s.finishAction(*id, programActionAnonymize, session, "Program.Anonymize", err, true)
		if err != nil {
			s.Logger.Errorw("failed to anonymize program, it is tried again later", "programID", id.String(), "error", err)
			st.failed(&st.retryAt, *id, time.Now())
			failed = append(failed, id.String())
			continue
		}
		st.ok(&st.retryAt, *id)
		done = append(done, id.String())
	}
	// written only when a program was anonymized, as for campaigns
	if len(done) > 0 {
		ae.Details["anonymizedProgramIds"] = done
		if len(failed) > 0 {
			ae.Details["failedProgramIds"] = failed
		}
		s.AuditLogAuthorized(ae)
	}
	return nil
}

// HandleDataAnonymizePrograms anonymizes the data of the programs whose data
// anonymize date has passed. A program cut short is found again on the next
// run.
func (s *Program) HandleDataAnonymizePrograms(ctx context.Context, session *model.Session) error {
	ae := NewAuditEvent("Program.HandleDataAnonymizePrograms", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	st := &s.anonymizes
	ids, err := s.ProgramRepository.GetIDsToDataAnonymize(ctx, time.Now(), st.held(&st.dataRetryAt, time.Now()))
	if err != nil {
		return errs.Wrap(err)
	}
	done, failed := []string{}, []string{}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		_, err := s.anonymizeProgramData(ctx, session, id)
		if isProgramAnonymizeSkip(err) {
			continue
		}
		s.finishAction(*id, programActionAnonymizeData, session, "Program.AnonymizeData", err, true)
		if err != nil {
			s.Logger.Errorw("failed to anonymize program data, it is tried again later", "programID", id.String(), "error", err)
			st.failed(&st.dataRetryAt, *id, time.Now())
			failed = append(failed, id.String())
			continue
		}
		st.ok(&st.dataRetryAt, *id)
		done = append(done, id.String())
	}
	if len(done) > 0 {
		ae.Details["dataAnonymizedProgramIds"] = done
		if len(failed) > 0 {
			ae.Details["failedProgramIds"] = failed
		}
		s.AuditLogAuthorized(ae)
	}
	return nil
}

// claimAnonymize checks under the lifecycle lock that check allows the
// program to be anonymized and claims it, so a close, delete or another
// anonymize of it waits until it is done. The caller releases the claim with
// releaseAnonymizing.
func (s *Program) claimAnonymize(ctx context.Context, id *uuid.UUID, check func(*model.Program) error) (*model.Program, error) {
	var program *model.Program
	err := s.withLifecycleLock(ctx, func() error {
		p, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if p.DeletingAt != nil || p.ClosingAt != nil {
			return errs.NewValidationError(errProgramStopping)
		}
		if err := check(p); err != nil {
			return errs.NewValidationError(err)
		}
		if s.isStoppingNow(*id) {
			return errs.NewValidationError(errProgramBusy)
		}
		if !s.claimAnonymizing(*id) {
			return errs.NewValidationError(errProgramAnonymizing)
		}
		program = p
		return nil
	})
	return program, err
}

// anonymizeProgram does the work of AnonymizeByID and returns how many hidden
// campaigns it went through. Every person first gets a pseudonym on the
// enrollment and every send of every hidden campaign takes it while the send
// still names the person, see linkProgramCampaign. Then the program is marked
// as being anonymized with its people counts stored, so the task finishes a
// run that stops half way, and from the mark on the results name people by
// pseudonym, which every send then has. Each hidden campaign then moves its
// events onto the pseudonyms and drops the person links, see
// anonymizeProgramCampaign. Last the enrollments are removed, so no row ties
// a person to the program or its results, and anonymized_at is written.
// Every step can run again. The lifecycle lock is held only for the check,
// the caller must not hold it.
func (s *Program) anonymizeProgram(ctx context.Context, session *model.Session, id *uuid.UUID) (int, error) {
	program, err := s.claimAnonymize(ctx, id, anonymizeAllCheck)
	if err != nil {
		return 0, err
	}
	defer s.releaseAnonymizing(*id)
	// the facts name people by recipient id, they are read again
	defer s.forgetProgramResults(id)
	// a run started by hand that stops before it is marked as begun, for
	// example when the server stops, is found again by its anonymize date and
	// finished in the background like a scheduled one
	if at := program.AnonymizeAtTime(); at == nil || at.After(time.Now()) {
		if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
			"anonymize_at": time.Now().UTC(),
		}); err != nil {
			return 0, errs.Wrap(err)
		}
	}
	if err := s.ProgramRepository.AssignEnrollmentPseudonyms(ctx, id); err != nil {
		return 0, errs.Wrap(err)
	}
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, id)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	// no send loses its person before every send has its pseudonym, so a
	// person is never named by recipient id in one campaign and by pseudonym
	// only in another
	for _, campaignID := range campaignIDs {
		if err := repository.PauseBetweenWrites(ctx); err != nil {
			return 0, err
		}
		if _, err := s.linkProgramCampaign(ctx, session, id, campaignID); err != nil {
			return 0, err
		}
	}
	if program.AnonymizingAt == nil {
		counts, err := s.ProgramRepository.CountEnrollmentsByStatus(ctx, id)
		if err != nil {
			return 0, errs.Wrap(err)
		}
		if _, err := s.ProgramRepository.BeginAnonymize(ctx, id, peopleCountsOf(counts)); err != nil {
			return 0, errs.Wrap(err)
		}
	}
	// the results read from here on name people by pseudonym
	s.forgetProgramResults(id)
	for _, campaignID := range campaignIDs {
		if err := repository.PauseBetweenWrites(ctx); err != nil {
			return 0, err
		}
		if err := s.anonymizeProgramCampaign(ctx, session, id, campaignID); err != nil {
			return 0, err
		}
	}
	// the enrollments hold who was in the program, with times and progress
	// that match the sends. The counts the pages need are stored on the
	// program, so they are removed.
	if err := s.ProgramRepository.DeleteEnrollmentsByProgramID(ctx, id); err != nil {
		return 0, errs.Wrap(err)
	}
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
		"anonymized_at":       time.Now().UTC(),
		"anonymizing_at":      nil,
		"data_anonymizing_at": nil,
	}); err != nil {
		return 0, errs.Wrap(err)
	}
	return len(campaignIDs), nil
}

// peopleCountsOf adds up enrollment counts by status into the counts stored
// when Anonymize All begins. The program is closed, so a person still active
// is counted as stopped.
func peopleCountsOf(counts []repository.ProgramEnrollmentStatusCount) repository.ProgramPeopleCounts {
	out := repository.ProgramPeopleCounts{}
	for _, c := range counts {
		out.Total += c.Count
		switch c.Status {
		case model.ProgramEnrollmentCompleted:
			out.Completed += c.Count
		case model.ProgramEnrollmentExited:
			out.Exited += c.Count
		default:
			out.Stopped += c.Count
		}
	}
	return out
}

// linkProgramCampaign closes a hidden campaign of a program when it is open
// and gives each of its sends that names a person and has no pseudonym the
// pseudonym of the person's enrollment, or a fresh one when the enrollment
// has none, see AssignMissingPseudonyms. The sends keep their person. It
// returns nil for a campaign that is gone or anonymized, which is passed
// over.
func (s *Program) linkProgramCampaign(
	ctx context.Context,
	session *model.Session,
	programID *uuid.UUID,
	campaignID *uuid.UUID,
) (*model.Campaign, error) {
	campaign, err := s.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{
		IncludeProgramManaged: true,
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if campaign.AnonymizedAt.IsSpecified() && !campaign.AnonymizedAt.IsNull() {
		return nil, nil
	}
	if campaign.IsActive() {
		err := s.CampaignService.closeCampaign(ctx, session, campaignID, campaign, "program anonymized")
		if err != nil && !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
			return nil, errs.Wrap(err)
		}
	}
	if err := s.CampaignRecipientRepository.LinkSendsToEnrollmentPseudonyms(ctx, s.ProgramRepository, programID, campaignID); err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.CampaignRecipientRepository.AssignMissingPseudonyms(ctx, campaignID); err != nil {
		return nil, errs.Wrap(err)
	}
	return campaign, nil
}

// anonymizeProgramCampaign anonymizes one hidden campaign of a program. The
// sends take their pseudonyms, see linkProgramCampaign, the events move onto
// the pseudonym of their send without their data, and then the campaign
// drops its person links and lure codes and is marked anonymized like a
// campaign. A campaign that is gone or anonymized is passed over.
func (s *Program) anonymizeProgramCampaign(
	ctx context.Context,
	session *model.Session,
	programID *uuid.UUID,
	campaignID *uuid.UUID,
) error {
	campaign, err := s.linkProgramCampaign(ctx, session, programID, campaignID)
	if err != nil || campaign == nil {
		return err
	}
	if err := s.ProgramRepository.MoveEventsToPseudonyms(ctx, campaignID); err != nil {
		return errs.Wrap(err)
	}
	return s.CampaignService.anonymizeCampaignRecipients(ctx, campaignID)
}

// dataRanAfterClose reports whether the data of a closed program, or of a
// closed hidden campaign, was anonymized after it closed, so no newer data
// can exist
func dataRanAfterClose(closedAt nullable.Nullable[time.Time], ranAt *time.Time) bool {
	closed, err := closedAt.Get()
	return err == nil && ranAt != nil && !ranAt.Before(closed)
}

// anonymizeProgramData does the work of AnonymizeDataByID and returns how
// many hidden campaigns it went through. The events of each hidden campaign
// lose their submitted data, IP address, user agent and browser metadata and
// the campaign is marked, the program last. A closed campaign whose data was
// anonymized after it closed is passed over, so a run that stops half way is
// finished by the next one. The lifecycle lock is held only for the check,
// the caller must not hold it.
func (s *Program) anonymizeProgramData(ctx context.Context, session *model.Session, id *uuid.UUID) (int, error) {
	if err := s.claimAnonymizeData(ctx, id); err != nil {
		return 0, err
	}
	defer s.releaseAnonymizing(*id)
	return s.anonymizeClaimedProgramData(ctx, id)
}

// claimAnonymizeData checks that the data of a program can be anonymized and
// claims the program, see claimAnonymize
func (s *Program) claimAnonymizeData(ctx context.Context, id *uuid.UUID) error {
	_, err := s.claimAnonymize(ctx, id, func(p *model.Program) error {
		switch {
		case p.IsAnonymized():
			return errProgramAlreadyAnonymized
		case p.AnonymizingAt != nil:
			// Anonymize All removes the data too, it is finished first
			return errProgramAnonymizing
		case dataRanAfterClose(p.ClosedAt, p.DataAnonymizedAt) && !dataDatePending(p):
			// closed and done after the close, there is no new data
			return errProgramDataAlreadyAnonymized
		}
		return nil
	})
	return err
}

// anonymizeClaimedProgramData is the work of anonymizeProgramData. The caller
// holds the claim of claimAnonymizeData and releases it.
func (s *Program) anonymizeClaimedProgramData(ctx context.Context, id *uuid.UUID) (int, error) {
	defer s.invalidateResults(id)
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, id)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	for _, campaignID := range campaignIDs {
		campaign, err := s.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{
			IncludeProgramManaged: true,
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return 0, errs.Wrap(err)
		}
		if campaign.AnonymizedAt.IsSpecified() && !campaign.AnonymizedAt.IsNull() {
			continue
		}
		var ranAt *time.Time
		if t, err := campaign.DataAnonymizedAt.Get(); err == nil {
			ranAt = &t
		}
		if dataRanAfterClose(campaign.ClosedAt, ranAt) {
			continue
		}
		if err := repository.PauseBetweenWrites(ctx); err != nil {
			return 0, err
		}
		if err := s.ProgramRepository.ScrubCampaignEventData(ctx, campaignID); err != nil {
			return 0, errs.Wrap(err)
		}
		if err := s.ProgramRepository.SetCampaignDataAnonymizedAt(ctx, campaignID); err != nil {
			return 0, errs.Wrap(err)
		}
	}
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
		"data_anonymized_at":  time.Now().UTC(),
		"data_anonymizing_at": nil,
	}); err != nil {
		return 0, errs.Wrap(err)
	}
	return len(campaignIDs), nil
}

// dataDatePending reports whether the data anonymize date of a program has
// passed and its data was not anonymized since, the programs the data task
// runs, see GetIDsToDataAnonymize
func dataDatePending(p *model.Program) bool {
	// a run by hand that did not finish is finished
	if p.DataAnonymizingAt != nil {
		return true
	}
	at := p.DataAnonymizeAtTime()
	if at == nil || at.After(time.Now()) {
		return false
	}
	return p.DataAnonymizedAt == nil || p.DataAnonymizedAt.Before(*at)
}

// forgetProgramResults forgets the cached facts, results and kept timeline
// counts of a program, after a change that renames who the sends are about
func (s *Program) forgetProgramResults(programID *uuid.UUID) {
	if programID == nil {
		return
	}
	s.resultsCache.invalidate(*programID)
	prefix := programID.String() + "|"
	programTimelineStore.Lock()
	defer programTimelineStore.Unlock()
	for k := range programTimelineStore.ranges {
		if strings.HasPrefix(k, prefix) {
			delete(programTimelineStore.ranges, k)
		}
	}
}

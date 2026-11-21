package service

import (
	"context"
	"errors"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
)

// the actions a request starts and the background finishes, see
// runInBackground and finishAction
const (
	programActionResume        = "resume"
	programActionAnonymizeData = "anonymizeData"
	programActionAnonymize     = "anonymize"
	programActionStart         = "start"
	programActionClose         = "close"
	programActionDelete        = "delete"
)

// errProgramBackgroundPanic is the reason shown when background work stopped
// on an internal error
var errProgramBackgroundPanic = errors.New("an internal error stopped it, see the server log")

// runInBackground runs fn for a program on its own goroutine, after the
// request that started it has answered, and audits how it ended, see
// finishAction
func (s *Program) runInBackground(
	id uuid.UUID,
	action string,
	session *model.Session,
	auditName string,
	fn func(ctx context.Context) error,
) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		var err error
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Errorw("recovered from panic in program background work",
					"programID", id.String(), "action", action, "panic", r, "stack", string(debug.Stack()))
				err = errProgramBackgroundPanic
			}
			s.finishAction(id, action, session, auditName, err, false)
		}()
		err = fn(context.Background())
	}()
}

// resumeEndedByOther reports a resume that a pause, close or delete ended.
// The program then is paused, closed or gone on purpose, it is not a failure.
func resumeEndedByOther(err error) bool {
	var v errs.ValidationError
	if !errors.As(err, &v) {
		return errors.Is(err, gorm.ErrRecordNotFound)
	}
	return v.Err == errProgramPausedOnResume || v.Err == errProgramStopping || v.Err == errProgramClosedResume
}

// finishAction audits how background work of a program ended, as done by
// session, the system, for the user who asked when known. A failure is kept
// to show on the program, see ActionFailure, and a success forgets an earlier
// failure of the same action. throttle audits a repeated failure at most once
// per programFailureAuditEvery, for a pass that tries again on its own.
func (s *Program) finishAction(id uuid.UUID, action string, session *model.Session, auditName string, err error, throttle bool) {
	ae := NewAuditEvent(auditName, session)
	ae.Details["id"] = id.String()
	if by := s.takeRequester(action, id, err == nil); by != "" {
		ae.Details["requestedBy"] = by
	}
	if err != nil && action == programActionResume && resumeEndedByOther(err) {
		ae.Details["reason"] = action + " stopped"
		s.AuditLogAuthorized(ae)
		return
	}
	if err == nil {
		s.stoppingMu.Lock()
		if f, ok := s.actionFailures[id]; ok && f.shown.Action == action {
			delete(s.actionFailures, id)
		}
		s.stoppingMu.Unlock()
		ae.Details["reason"] = action + " finished"
		s.AuditLogAuthorized(ae)
		return
	}
	s.Logger.Errorw("program background work failed", "programID", id.String(), "action", action, "error", err)
	s.recordActionFailure(id, action, err)
	ae.Details["reason"] = action + " failed"
	if throttle {
		s.auditBackgroundFailure(action+":"+id.String(), ae, err)
		return
	}
	s.AuditLogFailed(ae, err)
}

// recordActionFailure keeps a failed background action to show on the
// program, a validation error by its text, any other as an internal error
func (s *Program) recordActionFailure(id uuid.UUID, action string, err error) {
	reason := errProgramBackgroundPanic.Error()
	var v errs.ValidationError
	if errors.As(err, &v) && v.Err != nil {
		reason = v.Err.Error()
	}
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.actionFailures == nil {
		s.actionFailures = map[uuid.UUID]*programActionFailure{}
	}
	s.actionFailures[id] = &programActionFailure{
		shown: model.ProgramActionFailure{Action: action, Reason: reason, At: time.Now().UTC()},
		err:   err,
	}
}

// noteRequester keeps who asked for an action on a program, for the audit
// of its background part
func (s *Program) noteRequester(action string, id uuid.UUID, session *model.Session) {
	by := requesterID(session)
	if by == "" {
		return
	}
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if s.requestedBy == nil {
		s.requestedBy = map[string]string{}
	}
	s.requestedBy[action+":"+id.String()] = by
}

// takeRequester returns who asked for an action on a program, and forgets it
// when the action is done. Empty when unknown, for example after a restart.
func (s *Program) takeRequester(action string, id uuid.UUID, done bool) string {
	key := action + ":" + id.String()
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	by := s.requestedBy[key]
	if done {
		delete(s.requestedBy, key)
	}
	return by
}

// forgetProgramState forgets what is kept in memory for a program that is
// closed or deleted: a resume under way, its failures and who asked
func (s *Program) forgetProgramState(id uuid.UUID, gone bool) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	delete(s.resuming, id)
	delete(s.actionFailures, id)
	if !gone {
		return
	}
	prefix := ":" + id.String()
	for k := range s.requestedBy {
		if strings.HasSuffix(k, prefix) {
			delete(s.requestedBy, k)
		}
	}
}

// programActionFailure is a failed background action of a program, what the
// program read shows and the error itself
type programActionFailure struct {
	shown model.ProgramActionFailure
	err   error
}

// clearActionFailureOf forgets the failure of a program when it is of the
// given action
func (s *Program) clearActionFailureOf(id uuid.UUID, action string) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if f, ok := s.actionFailures[id]; ok && f.shown.Action == action {
		delete(s.actionFailures, id)
	}
}

// clearActionFailure forgets the failure of a program when an action runs
// again
func (s *Program) clearActionFailure(id uuid.UUID) {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	delete(s.actionFailures, id)
}

// isResuming reports whether a resume of the program runs now
func (s *Program) isResuming(id uuid.UUID) bool {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	_, ok := s.resuming[id]
	return ok
}

// applyRunState fills what runs in the background for a program now, and the
// last failure of an action, on a single program read
func (s *Program) applyRunState(program *model.Program) {
	id, err := program.ID.Get()
	if err != nil {
		return
	}
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	_, program.Resuming = s.resuming[id]
	_, program.Enrolling = s.enrolling[id]
	program.AnonymizeRunning = s.anonymizing[id]
	if f, ok := s.actionFailures[id]; ok {
		c := f.shown
		program.ActionFailure = &c
	}
}

// requesterID returns the user id of a session for the audit of background
// work, empty when it has none
func requesterID(session *model.Session) string {
	if session == nil || session.User == nil {
		return ""
	}
	if id, err := session.User.ID.Get(); err == nil {
		return id.String()
	}
	return ""
}

// waitForBackground waits until the work started by requests has ended, the
// enrollments, resumes and data anonymizes, or ctx ends. Tests use it.
func (s *Program) waitForBackground(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

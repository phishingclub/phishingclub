package service

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"go.uber.org/zap"
)

// deletePassRetryEvery is how long a row whose delete failed waits before the
// pass tries it again
const deletePassRetryEvery = 10 * time.Minute

// deletePassScanLimit is how many rows marked as being deleted one look of
// the pass reads to find one to delete
const deletePassScanLimit = 20

// deletePassHeldLimit bounds how many held back rows one look leaves out
const deletePassHeldLimit = 500

// deletePassFailureAuditEvery is how often a failure of the same row is
// audited
const deletePassFailureAuditEvery = time.Hour

// errDeletePanic is the failure of a delete that stopped on an internal error
var errDeletePanic = errors.New("an internal error stopped the delete, see the server log")

// deletePass finishes the deletes of rows marked as being deleted, such as
// companies and recipient groups, one at a time on its own goroutine. A
// request marks the row and starts the pass, the task runner starts it on
// every tick so a delete stopped by a restart is finished.
type deletePass struct {
	mu      sync.Mutex
	running bool
	// again asks the running pass to look for marked rows once more before
	// it ends
	again bool
	// retryAt holds back a row whose last delete failed
	retryAt map[uuid.UUID]time.Time
	// failureAuditAt is when a failure of a row was last audited
	failureAuditAt map[uuid.UUID]time.Time
	// requestedBy names the user who asked for the delete of a row, for the
	// audit of the background part
	requestedBy map[uuid.UUID]string
	// failures holds why the delete of a row waits, shown on the row until
	// the delete goes on
	failures map[uuid.UUID]string
	// wg lets a test wait for the pass to end
	wg sync.WaitGroup
}

// request notes who asked for the delete of a row
func (p *deletePass) request(id uuid.UUID, userID string) {
	if userID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.requestedBy == nil {
		p.requestedBy = map[uuid.UUID]string{}
	}
	p.requestedBy[id] = userID
}

// requester returns who asked for the delete of a row, empty when unknown,
// for example after a restart
func (p *deletePass) requester(id uuid.UUID) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requestedBy[id]
}

// auditFailure reports whether a failure of a row is to be audited now, at
// most once per deletePassFailureAuditEvery
func (p *deletePass) auditFailure(id uuid.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if last, ok := p.failureAuditAt[id]; ok && now.Sub(last) < deletePassFailureAuditEvery {
		return false
	}
	if p.failureAuditAt == nil {
		p.failureAuditAt = map[uuid.UUID]time.Time{}
	}
	p.failureAuditAt[id] = now
	return true
}

// forget removes what the pass keeps about a row that is gone or no longer
// marked
func (p *deletePass) forget(id uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.retryAt, id)
	delete(p.failureAuditAt, id)
	delete(p.requestedBy, id)
}

// deleteWaitReason is the reason shown for a delete that waits: the text of
// a validation error, such as a program that is still closing, else a plain
// note, as other errors can name internals
func deleteWaitReason(err error) string {
	var v errs.ValidationError
	if errors.As(err, &v) && v.Err != nil {
		return v.Err.Error()
	}
	return "a step failed and is tried again, see the server log"
}

// stopped keeps why the delete of a row waits
func (p *deletePass) stopped(id uuid.UUID, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures == nil {
		p.failures = map[uuid.UUID]string{}
	}
	p.failures[id] = reason
}

// failure returns why the delete of a row waits, empty when it does not
func (p *deletePass) failure(id uuid.UUID) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failures[id]
}

// clearFailure forgets why the delete of a row waited, when it goes on or
// the row is gone
func (p *deletePass) clearFailure(id uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.failures, id)
}

// held returns the rows that still wait after a failed try, at most
// deletePassHeldLimit, and drops the ones whose wait is over
func (p *deletePass) held(now time.Time) []*uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []*uuid.UUID{}
	for id, at := range p.retryAt {
		if !now.Before(at) {
			delete(p.retryAt, id)
			continue
		}
		if len(out) < deletePassHeldLimit {
			id := id
			out = append(out, &id)
		}
	}
	return out
}

// start runs the pass unless one runs, then that one is asked to look again.
// first, when set, is deleted before the others and is tried even when its
// last try failed a short while ago. list returns the marked rows that are
// not in skip, remove deletes one and audits it.
func (p *deletePass) start(
	logger *zap.SugaredLogger,
	first *uuid.UUID,
	list func(ctx context.Context, skip []*uuid.UUID) ([]*uuid.UUID, error),
	remove func(ctx context.Context, id *uuid.UUID) error,
) {
	p.mu.Lock()
	if first != nil {
		delete(p.retryAt, *first)
	}
	if p.running {
		p.again = true
		p.mu.Unlock()
		return
	}
	p.running = true
	p.again = false
	p.wg.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		ended := false
		defer func() {
			if r := recover(); r != nil {
				logger.Errorw("recovered from panic while finishing deletes",
					"panic", r, "stack", string(debug.Stack()))
			}
			if !ended {
				p.mu.Lock()
				p.running = false
				p.mu.Unlock()
			}
		}()
		ctx := context.Background()
		if first != nil {
			p.try(ctx, logger, first, remove)
		}
		for {
			for p.next(ctx, logger, list, remove) {
			}
			p.mu.Lock()
			if !p.again {
				p.running = false
				ended = true
				p.mu.Unlock()
				return
			}
			p.again = false
			p.mu.Unlock()
		}
	}()
}

// next deletes the oldest marked row that is not held back and reports
// whether it tried one
func (p *deletePass) next(
	ctx context.Context,
	logger *zap.SugaredLogger,
	list func(ctx context.Context, skip []*uuid.UUID) ([]*uuid.UUID, error),
	remove func(ctx context.Context, id *uuid.UUID) error,
) bool {
	ids, err := list(ctx, p.held(time.Now()))
	if err != nil {
		logger.Errorw("failed to list rows being deleted", "error", err)
		return false
	}
	// a held row past the bound of the skip list is left out here, so a row
	// is never tried before its wait is over
	now := time.Now()
	p.mu.Lock()
	var id *uuid.UUID
	for _, candidate := range ids {
		if at, ok := p.retryAt[*candidate]; ok && now.Before(at) {
			continue
		}
		id = candidate
		break
	}
	p.mu.Unlock()
	if id == nil {
		return false
	}
	p.try(ctx, logger, id, remove)
	return true
}

// try deletes one row and holds it back after a failure or a panic
func (p *deletePass) try(
	ctx context.Context,
	logger *zap.SugaredLogger,
	id *uuid.UUID,
	remove func(ctx context.Context, id *uuid.UUID) error,
) {
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorw("recovered from panic while deleting",
					"id", id.String(), "panic", r, "stack", string(debug.Stack()))
				err = errDeletePanic
			}
		}()
		return remove(ctx, id)
	}()
	if err != nil {
		logger.Errorw("failed to finish a delete, it is tried again later", "id", id.String(), "error", err)
		p.mu.Lock()
		if p.retryAt == nil {
			p.retryAt = map[uuid.UUID]time.Time{}
		}
		p.retryAt[*id] = time.Now().Add(deletePassRetryEvery)
		p.mu.Unlock()
		return
	}
	p.forget(*id)
}

// wait waits until no pass runs or ctx ends. Tests use it.
func (p *deletePass) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

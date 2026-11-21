package service

import (
	"context"
	"sync"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
)

// errCompanyDeleting refuses a change to a company that is being deleted, and
// new data in it
var errCompanyDeleting = errors.New("the company is being deleted")

// refuseDeletingCompany refuses new data in a company that is being deleted,
// so the background delete does not leave it behind. A nil id is the shared
// view and is never refused. On success the caller holds the read lock of
// the company until it calls the returned release, at the latest when the
// row that belongs to the company is written. The release can be called
// more than once, so a caller can release early and still defer it. Every service that writes rows of a company has
// the company repository, a nil one is only left out in tests.
func refuseDeletingCompany(ctx context.Context, companies *repository.Company, companyID *uuid.UUID) (func(), error) {
	if companies == nil || companyID == nil {
		return func() {}, nil
	}
	release := companyDeleteLock.read(companyID)
	deleting, err := companies.IsDeleting(ctx, companyID)
	if err != nil {
		release()
		return nil, errs.Wrap(err)
	}
	if deleting {
		release()
		return nil, validate.WrapErrorWithField(errCompanyDeleting, "companyID")
	}
	return release, nil
}

// errDeleteBusy refuses a delete while saves that use the company or group
// run, the user tries again
var errDeleteBusy = errors.New("saves that use it are running, try the delete again in a moment")

// deleteMarkWait is the longest a delete request waits for running saves
// before it answers busy
const deleteMarkWait = 5 * time.Second

// deleteMarkLock keeps a save that checked a company or group was not being
// deleted from writing after a delete mark of that same company or group. A
// save holds the read lock of the id from its check to the write that adds
// its row. A delete request takes the write lock of the id for its checks and
// the mark only, and never queues: it tries for deleteMarkWait and then
// answers busy, so it never holds up new saves. The locks are kept per id, an
// entry is removed when nothing holds it, so the map holds at most one entry
// per id in use.
type deleteMarkLock struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*deleteMarkEntry
}

// deleteMarkEntry is the lock of one id and how many hold or wait for it
type deleteMarkEntry struct {
	rw   sync.RWMutex
	refs int
}

// entry returns the lock of an id and counts the caller in
func (l *deleteMarkLock) entry(id uuid.UUID) *deleteMarkEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[uuid.UUID]*deleteMarkEntry{}
	}
	e, ok := l.entries[id]
	if !ok {
		e = &deleteMarkEntry{}
		l.entries[id] = e
	}
	e.refs++
	return e
}

// leave counts the caller out and removes an entry nothing holds
func (l *deleteMarkLock) leave(id uuid.UUID, e *deleteMarkEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.refs--
	if e.refs == 0 {
		delete(l.entries, id)
	}
}

// read takes the read locks of the ids for a save and returns their release.
// The release can be called more than once, so a save can release early and
// still defer it.
func (l *deleteMarkLock) read(ids ...*uuid.UUID) func() {
	held := make([]func(), 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		key := *id
		e := l.entry(key)
		e.rw.RLock()
		held = append(held, func() {
			e.rw.RUnlock()
			l.leave(key, e)
		})
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, release := range held {
				release()
			}
		})
	}
}

// mark takes the write lock of an id for a delete request and returns its
// release, or errDeleteBusy after deleteMarkWait
func (l *deleteMarkLock) mark(ctx context.Context, id uuid.UUID) (func(), error) {
	e := l.entry(id)
	deadline := time.Now().Add(deleteMarkWait)
	for {
		if e.rw.TryLock() {
			var once sync.Once
			return func() {
				once.Do(func() {
					e.rw.Unlock()
					l.leave(id, e)
				})
			}, nil
		}
		if !time.Now().Before(deadline) {
			l.leave(id, e)
			return nil, errs.NewValidationError(errDeleteBusy)
		}
		select {
		case <-ctx.Done():
			l.leave(id, e)
			return nil, errs.Wrap(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// companyDeleteLock holds the locks of company saves and company delete
// marks, groupDeleteLock the ones of saves that use a group and group delete
// marks
var (
	companyDeleteLock deleteMarkLock
	groupDeleteLock   deleteMarkLock
)

// deleteOriginKey carries the company and the user of a company delete to
// the deletes it runs, for their audit events
type deleteOriginKey struct{}

// deleteOrigin is the company and the user of a company delete
type deleteOrigin struct {
	companyID   string
	requestedBy any
}

// withDeleteOrigin marks ctx as part of the delete of a company, asked for
// by requestedBy when known
func withDeleteOrigin(ctx context.Context, companyID *uuid.UUID, requestedBy any) context.Context {
	return context.WithValue(ctx, deleteOriginKey{}, deleteOrigin{companyID: companyID.String(), requestedBy: requestedBy})
}

// addDeleteOrigin adds the company and the user of a company delete to an
// audit event of a delete it runs
func addDeleteOrigin(ctx context.Context, ae *AuditEvent) {
	o, ok := ctx.Value(deleteOriginKey{}).(deleteOrigin)
	if !ok {
		return
	}
	ae.Details["companyId"] = o.companyID
	if o.requestedBy != nil {
		ae.Details["requestedBy"] = o.requestedBy
	}
}

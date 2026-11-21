package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
)

// programFailureAuditEvery is how often a scheduler failure that repeats on
// every tick is written to the audit log for the same key
const programFailureAuditEvery = time.Hour

// programTickCounts is what one scheduler tick changed in one program
type programTickCounts struct {
	campaignsCreated int
	sendsScheduled   int
	enrolled         int
	exited           int
}

// programTickStats counts what one scheduler tick changed, for the one audit
// event of the tick. A nil value counts nothing, so code run outside a tick
// can call it.
type programTickStats struct {
	mu       sync.Mutex
	programs map[uuid.UUID]*programTickCounts
	// err is the first failure the tick logged and went on after
	err error
}

type programTickStatsKey struct{}

// withProgramTickStats returns a context that carries the counts of a tick
func withProgramTickStats(ctx context.Context, st *programTickStats) context.Context {
	return context.WithValue(ctx, programTickStatsKey{}, st)
}

// programTickStatsFrom returns the counts of the tick ctx belongs to, nil
// outside a tick
func programTickStatsFrom(ctx context.Context) *programTickStats {
	st, _ := ctx.Value(programTickStatsKey{}).(*programTickStats)
	return st
}

// add counts changes made to one program
func (st *programTickStats) add(programID *uuid.UUID, campaigns, sends, enrolled, exited int) {
	if st == nil || programID == nil || campaigns+sends+enrolled+exited == 0 {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.programs == nil {
		st.programs = map[uuid.UUID]*programTickCounts{}
	}
	c := st.programs[*programID]
	if c == nil {
		c = &programTickCounts{}
		st.programs[*programID] = c
	}
	c.campaignsCreated += campaigns
	c.sendsScheduled += sends
	c.enrolled += enrolled
	c.exited += exited
}

// fail keeps the first failure the tick went on after
func (st *programTickStats) fail(err error) {
	if st == nil || err == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err == nil {
		st.err = err
	}
}

// auditTick writes the success event of a tick when it changed a named
// program, and a failed event when it failed, see auditBackgroundFailure. The
// event names the named programs that changed with their counts. An
// anonymous program is never named, counted or flagged, as an event per tick
// shows when its people were sent to, so a tick that changed only anonymous
// programs writes no event. When the anonymous programs can not be told
// apart, every changed program is treated as anonymous.
func (s *Program) auditTick(ctx context.Context, ae *AuditEvent, session *model.Session, st *programTickStats, tickErr error) {
	st.mu.Lock()
	changed := make(map[uuid.UUID]programTickCounts, len(st.programs))
	for id, c := range st.programs {
		changed[id] = *c
	}
	if tickErr == nil {
		tickErr = st.err
	}
	st.mu.Unlock()
	if len(changed) > 0 {
		ids := make([]*uuid.UUID, 0, len(changed))
		for id := range changed {
			ids = append(ids, &id)
		}
		anonymous, err := s.ProgramRepository.GetAnonymousAmong(context.WithoutCancel(ctx), ids)
		if err != nil {
			s.Logger.Warnw("failed to read which programs are anonymous for the tick audit", "error", err)
		}
		named := []string{}
		total := programTickCounts{}
		for id, c := range changed {
			if err != nil || anonymous[id] {
				continue
			}
			named = append(named, id.String())
			total.campaignsCreated += c.campaignsCreated
			total.sendsScheduled += c.sendsScheduled
			total.enrolled += c.enrolled
			total.exited += c.exited
		}
		sort.Strings(named)
		if len(named) > 0 {
			ae.Details["programIds"] = named
			ae.Details["campaignsCreated"] = total.campaignsCreated
			ae.Details["sendsScheduled"] = total.sendsScheduled
			ae.Details["enrolled"] = total.enrolled
			ae.Details["exited"] = total.exited
			s.AuditLogAuthorized(ae)
		}
	}
	if tickErr != nil {
		s.auditBackgroundFailure("advance", NewAuditEvent("Program.Advance", session), tickErr)
	}
}

// auditBackgroundFailure writes a failed event for a scheduler failure. The
// scheduler tries again on every tick, so the same key is written at once and
// then at most once per programFailureAuditEvery, with how many were held
// back in between.
func (s *Program) auditBackgroundFailure(key string, ae *AuditEvent, err error) {
	s.failureAuditOnce.Do(func() {
		if s.failureAudit == nil {
			s.failureAudit = newAuditThrottle(programFailureAuditEvery)
		}
	})
	write, suppressed, _ := s.failureAudit.allow(key, "")
	if !write {
		return
	}
	if suppressed > 0 {
		ae.Details["suppressed"] = suppressed
	}
	s.AuditLogFailed(ae, err)
}

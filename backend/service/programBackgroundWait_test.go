package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
)

// settle waits until the background work of the program service has ended:
// the work requests started, the close, delete and anonymize passes
func (s *Program) settle(ctx context.Context) error {
	for i := 0; i < 3; i++ {
		if err := s.waitForBackground(ctx); err != nil {
			return err
		}
		if err := s.waitForCloses(ctx); err != nil {
			return err
		}
		if err := s.waitForDeletes(ctx); err != nil {
			return err
		}
		if err := s.waitForAnonymizes(ctx); err != nil {
			return err
		}
	}
	return nil
}

// lastActionErr returns the error of the last failed background action of a
// program, nil when there is none
func (s *Program) lastActionErr(id uuid.UUID) error {
	s.stoppingMu.Lock()
	defer s.stoppingMu.Unlock()
	if f, ok := s.actionFailures[id]; ok {
		return f.err
	}
	return nil
}

// closeWait closes a program and waits for the background close to end, the
// same as the close did in the request before
func (s *Program) closeWait(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	if err := s.Close(ctx, session, id); err != nil {
		return err
	}
	return s.settle(context.WithoutCancel(ctx))
}

// startWait starts a program and waits for the background enrollment
func (s *Program) startWait(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	if err := s.Start(ctx, session, id); err != nil {
		return err
	}
	return s.settle(context.WithoutCancel(ctx))
}

// resumeWait resumes a program, waits for the background resume and returns
// its failure
func (s *Program) resumeWait(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	if err := s.Resume(ctx, session, id); err != nil {
		return err
	}
	if err := s.settle(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	return s.lastActionErr(*id)
}

// anonymizeWait runs Anonymize All and waits for the background pass
func (s *Program) anonymizeWait(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	if err := s.AnonymizeByID(ctx, session, id); err != nil {
		return err
	}
	return s.settle(context.WithoutCancel(ctx))
}

// anonymizeDataWait runs Anonymize Data, waits for it and returns its failure
func (s *Program) anonymizeDataWait(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	if err := s.AnonymizeDataByID(ctx, session, id); err != nil {
		return err
	}
	if err := s.settle(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	return s.lastActionErr(*id)
}

// mustSettle fails the test when the background work does not end
func mustSettle(t *testing.T, s *Program) {
	t.Helper()
	if err := s.settle(context.Background()); err != nil {
		t.Fatalf("background work did not end: %v", err)
	}
}

// deleteWait deletes a recipient group and waits for the background delete
func (r *RecipientGroup) deleteWait(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	if err := r.DeleteByID(ctx, session, id); err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return r.deletes.wait(wait)
}

package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// LiveSession is the minimal interface the service needs to manage session lifecycle.
// The controller's concrete session type implements this; the service never needs to
// know about browser pages or WebSocket connections.
type LiveSession interface {
	GetCampaignID() uuid.UUID
	Cancel()
	IsKeepAlive() bool
}

// RemoteBrowser manages saved remote browser scripts and tracks live sessions.
type RemoteBrowser struct {
	Common
	RemoteBrowserRepository *repository.RemoteBrowser
	sessions                sync.Map // key (crID or rbID string) → LiveSession
}

// SwapSession atomically replaces the session for key, returning the previous one.
func (s *RemoteBrowser) SwapSession(key string, sess LiveSession) (LiveSession, bool) {
	prev, had := s.sessions.Swap(key, sess)
	if !had {
		return nil, false
	}
	return prev.(LiveSession), true
}

// StoreSession stores a session, overwriting any existing entry for key.
func (s *RemoteBrowser) StoreSession(key string, sess LiveSession) {
	s.sessions.Store(key, sess)
}

// LoadSession returns the session for key, if present.
func (s *RemoteBrowser) LoadSession(key string) (LiveSession, bool) {
	val, ok := s.sessions.Load(key)
	if !ok {
		return nil, false
	}
	return val.(LiveSession), true
}

// LoadAndDeleteSession atomically loads and removes the session for key.
func (s *RemoteBrowser) LoadAndDeleteSession(key string) (LiveSession, bool) {
	val, loaded := s.sessions.LoadAndDelete(key)
	if !loaded {
		return nil, false
	}
	return val.(LiveSession), true
}

// CompareAndDeleteSession removes the session for key only if it is still sess
// (pointer identity), so a newer session's cleanup never evicts its own entry.
func (s *RemoteBrowser) CompareAndDeleteSession(key string, sess LiveSession) {
	s.sessions.CompareAndDelete(key, sess)
}

// RangeSessions calls fn for every live session. Returning false stops iteration.
func (s *RemoteBrowser) RangeSessions(fn func(key string, sess LiveSession) bool) {
	s.sessions.Range(func(k, v any) bool {
		return fn(k.(string), v.(LiveSession))
	})
}

// TerminateByCampaignID cancels and removes all sessions belonging to campaignID.
// Called by service.Campaign on close/delete.
func (s *RemoteBrowser) TerminateByCampaignID(campaignID uuid.UUID) {
	s.sessions.Range(func(key, value any) bool {
		sess := value.(LiveSession)
		if sess.GetCampaignID() == campaignID {
			sess.Cancel()
			s.sessions.CompareAndDelete(key, value)
		}
		return true
	})
}

// Create saves a new remote browser script.
func (s *RemoteBrowser) Create(
	ctx context.Context,
	session *model.Session,
	rb *model.RemoteBrowser,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("RemoteBrowser.Create", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	var companyID *uuid.UUID
	if cid, err := rb.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	if err := rb.Validate(); err != nil {
		s.Logger.Errorw("failed to validate remote browser", "error", err)
		return nil, errs.Wrap(err)
	}

	name := rb.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(ctx, s.RemoteBrowserRepository.DB, "remote_browsers", name.String(), companyID, nil)
	if err != nil {
		s.Logger.Errorw("failed to check remote browser uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}

	id, err := s.RemoteBrowserRepository.Insert(ctx, rb)
	if err != nil {
		s.Logger.Errorw("failed to create remote browser", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	s.AuditLogAuthorized(ae)
	return id, nil
}

// GetAll returns all remote browsers for the given company.
func (s *RemoteBrowser) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.RemoteBrowserOption,
) (*model.Result[model.RemoteBrowser], error) {
	result := model.NewEmptyResult[model.RemoteBrowser]()
	ae := NewAuditEvent("RemoteBrowser.GetAll", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = s.RemoteBrowserRepository.GetAll(ctx, companyID, options)
	if err != nil {
		s.Logger.Errorw("failed to get remote browsers", "error", err)
		return result, errs.Wrap(err)
	}
	return result, nil
}

// GetAllOverview returns lightweight overview rows.
func (s *RemoteBrowser) GetAllOverview(
	companyID *uuid.UUID,
	ctx context.Context,
	session *model.Session,
	options *repository.RemoteBrowserOption,
) (*model.Result[model.RemoteBrowserOverview], error) {
	result := model.NewEmptyResult[model.RemoteBrowserOverview]()
	ae := NewAuditEvent("RemoteBrowser.GetAllOverview", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = s.RemoteBrowserRepository.GetAllSubset(ctx, companyID, options)
	if err != nil {
		s.Logger.Errorw("failed to get remote browser overview", "error", err)
		return result, errs.Wrap(err)
	}
	return result, nil
}

// GetByID returns a single remote browser by ID.
func (s *RemoteBrowser) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.RemoteBrowserOption,
) (*model.RemoteBrowser, error) {
	ae := NewAuditEvent("RemoteBrowser.GetByID", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	rb, err := s.RemoteBrowserRepository.GetByID(ctx, id, options)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		s.Logger.Errorw("failed to get remote browser by ID", "error", err)
		return nil, errs.Wrap(err)
	}
	return rb, nil
}

// UpdateByID updates mutable fields on a remote browser.
func (s *RemoteBrowser) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	rb *model.RemoteBrowser,
) error {
	ae := NewAuditEvent("RemoteBrowser.UpdateByID", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	current, err := s.RemoteBrowserRepository.GetByID(ctx, id, &repository.RemoteBrowserOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err != nil {
		s.Logger.Errorw("failed to get remote browser for update", "error", err)
		return err
	}

	if _, err := rb.Name.Get(); err == nil {
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := rb.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(ctx, s.RemoteBrowserRepository.DB, "remote_browsers", name.String(), companyID, id)
		if err != nil {
			s.Logger.Errorw("failed to check remote browser name uniqueness on update", "error", err)
			return errs.Wrap(err)
		}
		if !isOK {
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
	}

	if rb.Config.IsSpecified() {
		if cfg, err := rb.Config.Get(); err == nil {
			if cfg.Mode != "" && cfg.Mode != "local" && cfg.Mode != "remote" {
				return fmt.Errorf("config.mode must be 'local' or 'remote'")
			}
			if cfg.Mode == "remote" && cfg.Remote == "" {
				return fmt.Errorf("config.remote is required when mode is 'remote'")
			}
		}
	}

	if err := s.RemoteBrowserRepository.UpdateByID(ctx, id, rb); err != nil {
		s.Logger.Errorw("failed to update remote browser", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID removes a remote browser.
func (s *RemoteBrowser) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("RemoteBrowser.DeleteByID", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.RemoteBrowserRepository.DeleteByID(ctx, id); err != nil {
		s.Logger.Errorw("failed to delete remote browser", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

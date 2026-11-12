package service

import (
	"context"
	"fmt"
	"time"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// Session is a service for Session
type Session struct {
	Common
	SessionRepository *repository.Session
}

// GetSession returns a session if one exists associated with the request
// if the session exists it will extend the session expiry date
// else it will invalidate the session cookie if provided
// modifies the response headers
func (s *Session) GetAndExtendSession(g *gin.Context) (*model.Session, error) {
	session, err := s.validateAndExtendSession(g)
	hasErr := errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errs.ErrSessionCookieNotFound)
	if hasErr {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		// TODO audit log? if the error is because the session IP changed
		s.Logger.Debugw("failed to validate and extend session", "error", err)
		return nil, errs.Wrap(err)
	}
	return session, nil
}

// GetByID returns a session by ID
func (s *Session) GetByID(
	ctx context.Context,
	sessionID *uuid.UUID,
	options *repository.SessionOption,
) (*model.Session, error) {
	session, err := s.SessionRepository.GetByID(
		ctx,
		sessionID,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}

	return session, nil
}

// GetSessionsByUserID returns all sessions by user ID
func (s *Session) GetSessionsByUserID(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
	options *repository.SessionOption,
) (*model.Result[model.Session], error) {
	result := model.NewEmptyResult[model.Session]()
	ae := NewAuditEvent("Session.GetSessionsByUserID", session)
	ae.Details["userId"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get all sessions by user ID
	result, err = s.SessionRepository.GetAllActiveSessionByUserID(
		ctx,
		userID,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, gorm.ErrRecordNotFound
	}
	if err != nil {
		s.Logger.Errorw("failed to get sessions by user ID", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read

	return result, nil
}

// validateAndExtendSession returns a session if one exists associated with the request
func (s *Session) validateAndExtendSession(g *gin.Context) (*model.Session, error) {
	cookie, err := g.Cookie(data.SessionCookieKey)
	if err != nil {
		return nil, errs.ErrSessionCookieNotFound
	}
	id, err := uuid.Parse(cookie)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// checks that the session is not expired
	ctx := g.Request.Context()
	session, err := s.SessionRepository.GetByID(ctx, &id, &repository.SessionOption{
		WithUser:        true,
		WithUserRole:    true,
		WithUserCompany: true,
	})
	// there is a valid session cookie but no valid session, so we expire the session cookie
	if errors.Is(err, gorm.ErrRecordNotFound) {
		g.SetCookie(
			data.SessionCookieKey,
			"",
			-1,
			"/",
			"",
			true,
			true,
		)
		return nil, errs.Wrap(err)
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// handle session and that IP has not changed
	// if it has changed - we expire the session
	sessionIP := session.IP
	clientIP := g.ClientIP()
	if session.IP != clientIP {
		err := s.Expire(ctx, session.ID)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to expire session upon changed IP (%s != %s): %s",
				sessionIP,
				clientIP,
				err,
			)
		}
		// audit log - session invliad due to IP change
		ae := NewAuditEvent("Session.Renew", session)
		ae.Details["reason"] = "ipChanged"
		ae.Details["previousIp"] = sessionIP
		ae.Details["newIp"] = clientIP
		s.AuditLogNotAuthorized(ae)
		return nil, fmt.Errorf(
			"session IP changed (%s != %s)",
			sessionIP,
			clientIP,
		)
	}
	// session is valid - update the session expiry date
	session.Renew(model.SessionIdleTimeout)
	err = s.SessionRepository.UpdateExpiry(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("failed to update session expiry: %s", err)
	}

	return session, nil
}

// Create creates a new session
// no auth - anyone can create a session
func (s *Session) Create(
	ctx context.Context,
	user *model.User,
	ip string,
) (*model.Session, error) {
	now := time.Now()
	expiredAt := now.Add(model.SessionIdleTimeout).UTC()
	maxAgeAt := now.Add(model.SessionMaxAgeAt).UTC()
	id := uuid.New()
	newSession := &model.Session{
		ID:        &id,
		User:      user,
		IP:        ip,
		ExpiresAt: &expiredAt,
		MaxAgeAt:  &maxAgeAt,
	}
	sessionID, err := s.SessionRepository.Insert(
		ctx,
		newSession,
	)
	if err != nil {
		s.Logger.Errorw("failed to insert session when creating a new session", "error", err)
		return nil, errs.Wrap(err)
	}
	createdSession, err := s.SessionRepository.GetByID(
		ctx,
		sessionID,
		&repository.SessionOption{
			WithUser:        true,
			WithUserRole:    true,
			WithUserCompany: true,
		},
	)
	if err != nil {
		s.Logger.Errorw("failed to get session after creating it", "error", err)
		return nil, errs.Wrap(err)
	}
	// a session is only created after all login checks have passed,
	// including MFA and SSO, so this event marks a fully completed login
	ae := NewAuditEvent("User.Login", createdSession)
	// matches the sessionIdPrefix of the User.Logout event for this session
	ae.Details["sessionIdPrefix"] = sessionIDPrefix(sessionID)
	if username, err := user.Username.Get(); err == nil {
		ae.Details["username"] = username.String()
	}
	s.AuditLogAuthorized(ae)
	return createdSession, nil
}

// Expire expires a session
func (s *Session) Expire(
	ctx context.Context,
	sessionID *uuid.UUID,
) error {
	err := s.SessionRepository.Expire(ctx, sessionID)
	if err != nil {
		s.Logger.Errorw("failed to expire session", "error", err)
		return err
	}
	return nil
}

// sessionIDPrefix returns the first 8 characters of a session id. The full id
// is the session cookie, so it is never written to the audit log. The prefix
// is enough to match an event to a session in the session list.
func sessionIDPrefix(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()[:8]
}

// Logout expires the session of a user who logs out and audits it. An id that
// is not an active session is expired without an event.
func (s *Session) Logout(
	ctx context.Context,
	sessionID *uuid.UUID,
	ip string,
) error {
	// the session is expired even when the lookup fails, logout must not
	// leave a session active. The lookup only names the user in the event
	existing, lookupErr := s.SessionRepository.GetByID(
		ctx,
		sessionID,
		&repository.SessionOption{WithUser: true},
	)
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("failed to get session on logout", "error", lookupErr)
	}
	if err := s.SessionRepository.Expire(ctx, sessionID); err != nil {
		s.Logger.Errorw("failed to expire session on logout", "error", err)
		return errs.Wrap(err)
	}
	if lookupErr == nil && existing != nil {
		ae := NewAuditEvent("User.Logout", existing)
		ae.IP = ip
		ae.Details["sessionIdPrefix"] = sessionIDPrefix(sessionID)
		s.AuditLogAuthorized(ae)
	}
	return nil
}

// ExpireByID expires any session as an administrator and audits it.
func (s *Session) ExpireByID(
	ctx context.Context,
	session *model.Session,
	sessionID *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("Session.Expire", session)
	defer s.auditDone(ae, &err)
	ae.Details["sessionIdPrefix"] = sessionIDPrefix(sessionID)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// the lookup only names the owner in the event, a failed lookup does not
	// stop the expire
	target, err := s.SessionRepository.GetByID(
		ctx,
		sessionID,
		&repository.SessionOption{WithUser: true},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("failed to get session to expire", "error", err)
	}
	if err == nil && target != nil && target.User != nil {
		if id, err := target.User.ID.Get(); err == nil {
			ae.Details["userId"] = id.String()
		}
	}
	if err := s.SessionRepository.Expire(ctx, sessionID); err != nil {
		s.Logger.Errorw("failed to expire session", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// ExpireAllByUserID expires all sessions by user ID
func (s *Session) ExpireAllByUserID(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("Session.ExpireAllByUserID", session)
	defer s.auditDone(ae, &err)
	ae.Details["userId"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if session.User == nil {
		s.Logger.Errorw("failed to get user from session when expiring session", "error", err)
		return err
	}
	sessions, err := s.SessionRepository.GetAllActiveSessionByUserID(
		ctx,
		userID,
		&repository.SessionOption{},
	)
	if err != nil {
		s.Logger.Errorw("failed to get user sessions when expiring session", "error", err)
		return err
	}
	if len(sessions.Rows) == 0 {
		s.Logger.Debugw("no sessions to remove", "userID", userID.String())
	}
	for _, session := range sessions.Rows {
		err = s.SessionRepository.Expire(ctx, session.ID)
		if err != nil {
			s.Logger.Errorw("failed a users expiring session", "error", err)
			return err
		}
	}
	s.AuditLogAuthorized(ae)

	return nil
}

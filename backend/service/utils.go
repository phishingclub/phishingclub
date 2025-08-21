package service

import (
	"fmt"

	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"go.uber.org/zap"
)

// Common holds commonly used service utils
type Common struct {
	Logger *zap.SugaredLogger
}

func (c *Common) LogAuthError(err error) {
	c.Logger.Errorw("failed to check permission", "error", err)
}

type AuditEvent struct {
	Name       string // ex. User.Create
	IP         string // ip of the user performing the action
	UserID     string // user performing the action
	Authorized bool
	Details    map[string]interface{}
}

func NewAuditEvent(name string, session *model.Session) *AuditEvent {
	userID := ""
	clientIP := ""
	if session != nil {
		if usr := session.User; usr != nil {
			userID = usr.ID.MustGet().String()
		}
		clientIP = session.IP
	}
	return &AuditEvent{
		Name:    name,
		UserID:  userID,
		IP:      clientIP,
		Details: map[string]interface{}{},
	}
}

func (c *Common) auditLog(ae *AuditEvent) {
	c.Logger.Infow("audit", ae.LogFields()...)
}

func (c *Common) AuditLogAuthorized(e *AuditEvent) {
	e.Authorized = true
	c.auditLog(e)
}

func (c *Common) AuditLogNotAuthorized(e *AuditEvent) {
	e.Authorized = false
	c.auditLog(e)
}

func isLoaded(
	session *model.Session,
) (*model.User, *model.Role, error) {
	user := session.User
	if user == nil {
		return nil, nil, fmt.Errorf("user is not loaded but required")
	}
	role := user.Role
	if role == nil {
		return nil, nil, fmt.Errorf("role is not loaded but required")
	}
	return user, role, nil
}

// IsAuthorized checks if the session is authorized to perform the permission
func IsAuthorized(
	session *model.Session,
	permission string,
) (bool, error) {
	_, role, err := isLoaded(session)
	if err != nil {
		return false, errs.Wrap(err)
	}
	return role.IsAuthorized(permission), nil
}

func (ae *AuditEvent) LogFields() []interface{} {
	return []interface{}{
		"name", ae.Name,
		"ip", ae.IP,
		"userId", ae.UserID,
		"authorized", ae.Authorized,
		"details", ae.Details,
	}
}

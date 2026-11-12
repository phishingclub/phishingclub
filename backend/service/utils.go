package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Common holds commonly used service utils
type Common struct {
	Logger *zap.SugaredLogger
	// AuditLogger writes audit events. It ignores the runtime log level so
	// audit events can not be turned off. When nil, Logger is used.
	AuditLogger *zap.SugaredLogger
}

func (c *Common) LogAuthError(err error) {
	c.Logger.Errorw("failed to check permission", "error", err)
}

// audit results, each event is written with exactly one of them
const (
	AuditResultSuccess = "success"
	AuditResultDenied  = "denied"
	AuditResultFailed  = "failed"
)

type AuditEvent struct {
	Name       string // ex. User.Create
	IP         string // ip of the user performing the action
	UserID     string // user performing the action
	Authorized bool
	// Result is success, denied or failed
	Result  string
	Details map[string]interface{}
	// written is set once the event is in the log, so auditDone does not
	// write it a second time
	written bool
	// system is set for actions run by the server itself, such as timer jobs
	system bool
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
	ae := &AuditEvent{
		Name:    name,
		UserID:  userID,
		IP:      clientIP,
		Details: map[string]interface{}{},
		system:  isSystemSession(session),
	}
	// actions the server runs on its own, such as timer jobs
	if ae.system {
		ae.Details["actor"] = "system"
	}
	// actions made with an API key instead of a login
	if session != nil && session.ID != nil && session.ID.String() == data.APISessionID {
		ae.Details["actor"] = "api"
	}
	return ae
}

// authorizeAndAudit checks the session holds the global permission and writes
// ae as success or denied. It is for actions where the caller does the work
// after the check, such as building an export file, so success here means the
// action was allowed and started.
func (c *Common) authorizeAndAudit(session *model.Session, ae *AuditEvent) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		c.AuditLogFailed(ae, err)
		return err
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	c.AuditLogAuthorized(ae)
	return nil
}

// skip marks the event as handled without writing it, for an outcome that is
// a question back to the user, such as a lure code that is already taken.
// The answer is written as its own event.
func (ae *AuditEvent) skip() {
	ae.written = true
}

// isSystemSession reports if the session is the one the server uses for its
// own actions
func isSystemSession(session *model.Session) bool {
	return session != nil && session.ID != nil && session.ID.String() == data.SystemSessionID
}

func (c *Common) auditLog(ae *AuditEvent) {
	logger := c.AuditLogger
	if logger == nil {
		logger = c.Logger
	}
	ae.written = true
	logger.Infow("audit", ae.LogFields()...)
}

// AuditLogAuthorized writes the event as a success
func (c *Common) AuditLogAuthorized(e *AuditEvent) {
	e.Authorized = true
	e.Result = AuditResultSuccess
	c.auditLog(e)
}

// AuditLogNotAuthorized writes the event as denied
func (c *Common) AuditLogNotAuthorized(e *AuditEvent) {
	e.Authorized = false
	e.Result = AuditResultDenied
	c.auditLog(e)
}

// AuditLogFailed writes the event as an action that did not complete. Only
// the kind of error is written, see auditErrorClass. Authorization errors and
// wrong credentials are written as denied instead. For a failed event
// authorized is true, the result field is what tells the outcome.
func (c *Common) AuditLogFailed(e *AuditEvent, err error) {
	if errors.Is(err, errs.ErrAuthorizationFailed) {
		c.AuditLogNotAuthorized(e)
		return
	}
	if isWrongCredential(err) {
		if _, has := e.Details["reason"]; !has {
			e.Details["reason"] = "wrongCredential"
		}
		c.AuditLogNotAuthorized(e)
		return
	}
	e.Authorized = true
	e.Result = AuditResultFailed
	e.Details["errorClass"] = auditErrorClass(err)
	c.auditLog(e)
}

// auditDone writes the event as failed when the function returns an error
// and the event was not already written. Success and denied are still
// written by the function itself. Call it deferred right after the event is
// created, with a pointer to the named error result:
//
//	ae := NewAuditEvent("Thing.Create", session)
//	defer s.auditDone(ae, &err)
//
// Failures of system actions are not written here. Timer jobs retry every
// few seconds and would write the same failure again on each run, they are
// in the normal error log instead.
func (c *Common) auditDone(ae *AuditEvent, errp *error) {
	if ae == nil || ae.written || ae.system || errp == nil || *errp == nil {
		return
	}
	c.AuditLogFailed(ae, *errp)
}

// isCampaignStateError reports a refusal because the campaign is not in a
// state that allows the action, such as pausing a paused campaign
func isCampaignStateError(err error) bool {
	for _, e := range []error{
		errs.ErrCampaignAlreadySetToClose,
		errs.ErrCampaignAlreadyClosed,
		errs.ErrCampaignAlreadyAnonymized,
		errs.ErrCampaignDataAlreadyAnonymized,
		errs.ErrCampaignAlreadyPaused,
		errs.ErrCampaignNotPaused,
		errs.ErrCampaignCannotPause,
		errs.ErrCampaignNothingToSend,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// isWrongCredential reports errors for a wrong password or code
func isWrongCredential(err error) bool {
	return errors.Is(err, errs.ErrAuthenticationFailed) ||
		errors.Is(err, errs.ErrUserWrongPasword) ||
		errors.Is(err, errs.ErrUserWrongTOTP) ||
		errors.Is(err, errs.ErrUserWrongRecoveryCode)
}

// auditErrorClass names the kind of a failure for the audit log. The error text
// is not logged: it can carry values the user supplied, such as a substituted
// secret variable inside a header, a url or a template, and those must not end
// up in the audit log. The full error still goes back to the caller.
func auditErrorClass(err error) string {
	var ve errs.ValidationError
	var se errs.SyntaxError
	var ce errs.ConflictError
	var oe errs.OperationalError
	var cu errs.CustomError
	switch {
	case isCampaignStateError(err):
		return "state"
	case errors.As(err, &ve):
		return "validation"
	case errors.As(err, &se):
		return "syntax"
	case errors.As(err, &ce):
		return "conflict"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "notFound"
	case errors.As(err, &oe):
		return "operational"
	case errors.As(err, &cu):
		return "custom"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	default:
		return "internal"
	}
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
		"result", ae.Result,
		"details", ae.Details,
	}
}

// auditURL returns u without any user name or password in it, so a URL typed
// as https://user:pass@host does not put the password in the audit log
func auditURL(u string) string {
	parsed, err := url.Parse(u)
	// without a host the user info is not split out, the whole value could
	// hold a password
	if err != nil || parsed.Host == "" {
		return "[invalid url]"
	}
	if parsed.User != nil {
		parsed.User = nil
	}
	return parsed.String()
}

// auditThrottle limits how often an event for one key is written, for events
// that anyone can cause, such as a wrong SCIM token or API key. The keys must
// be a bounded set, such as company ids or a fixed name.
type auditThrottle struct {
	interval time.Duration
	mu       sync.Mutex
	byKey    map[string]*auditThrottleState
}

type auditThrottleState struct {
	last       time.Time
	suppressed int
	// lastIP is the IP of the most recent event that was not written
	lastIP string
}

func newAuditThrottle(interval time.Duration) *auditThrottle {
	return &auditThrottle{interval: interval, byKey: map[string]*auditThrottleState{}}
}

// allow reports if an event for key may be written now. When it may, it also
// returns how many were held back since the last written one and the IP of
// the last held back event.
func (t *auditThrottle) allow(key string, ip string) (bool, int, string) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.byKey[key]
	if !ok {
		st = &auditThrottleState{}
		t.byKey[key] = st
	}
	if !st.last.IsZero() && now.Sub(st.last) < t.interval {
		st.suppressed++
		st.lastIP = ip
		return false, 0, ""
	}
	suppressed, lastIP := st.suppressed, st.lastIP
	st.last = now
	st.suppressed = 0
	st.lastIP = ""
	return true, suppressed, lastIP
}

// addThrottleDetails records events a throttle held back
func addThrottleDetails(ae *AuditEvent, suppressed int, lastSuppressedIP string) {
	if suppressed > 0 {
		ae.Details["suppressed"] = suppressed
		ae.Details["lastSuppressedIp"] = lastSuppressedIP
	}
}

package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/version"
)

// Version is a service for application service
type Version struct {
	Common
}

// Get gets the application service
func (o *Version) Get(
	ctx context.Context,
	session *model.Session,
) (string, error) {
	ae := NewAuditEvent("Version.Get", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	return version.Get(), nil
}

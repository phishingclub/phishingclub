package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

type Identifier struct {
	Common
	IdentifierRepository *repository.Identifier
}

func (i *Identifier) GetAll(
	ctx context.Context,
	session *model.Session,
	options *repository.IdentifierOption,
) (*model.Result[model.Identifier], error) {
	result := model.NewEmptyResult[model.Identifier]()
	ae := NewAuditEvent("Identifier.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		i.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		i.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get
	result, err = i.IdentifierRepository.GetAll(ctx, options)
	if err != nil {
		i.Logger.Errorw("failed to get all identifiers", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

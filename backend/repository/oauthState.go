package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
)

// OAuthState repository
type OAuthState struct {
	DB *gorm.DB
}

// Insert inserts a new oauth state token
func (r *OAuthState) Insert(
	ctx context.Context,
	state *model.OAuthState,
) (*uuid.UUID, error) {
	id := uuid.New()
	now := time.Now()
	dbState := &database.OAuthState{
		ID:              id,
		CreatedAt:       &now,
		StateToken:      state.StateToken.MustGet().String(),
		OAuthProviderID: state.OAuthProviderID.MustGet(),
		ExpiresAt:       state.ExpiresAt,
		Used:            false,
	}

	result := r.DB.WithContext(ctx).Create(dbState)
	if result.Error != nil {
		return nil, errs.Wrap(result.Error)
	}

	return &id, nil
}

// GetByStateToken retrieves an oauth state by state token
func (r *OAuthState) GetByStateToken(
	ctx context.Context,
	stateToken string,
) (*model.OAuthState, error) {
	var dbState database.OAuthState
	result := r.DB.WithContext(ctx).
		Where("state_token = ?", stateToken).
		First(&dbState)

	if result.Error != nil {
		return nil, errs.Wrap(result.Error)
	}

	return r.toModel(&dbState), nil
}

// ErrOAuthStateAlreadyUsed is returned when a state token was used before
var ErrOAuthStateAlreadyUsed = errors.New("oauth state already used")

// MarkAsUsed marks a state token as used. The update only matches an unused
// row, so of two concurrent callbacks with the same state only one wins and
// the other gets ErrOAuthStateAlreadyUsed.
func (r *OAuthState) MarkAsUsed(
	ctx context.Context,
	id uuid.UUID,
) error {
	now := time.Now()
	result := r.DB.WithContext(ctx).
		Model(&database.OAuthState{}).
		Where("id = ? AND used = ?", id, false).
		Updates(map[string]interface{}{
			"used":    true,
			"used_at": &now,
		})

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrOAuthStateAlreadyUsed
	}

	return nil
}

// MarkFailedByStateToken marks an unused state token as used and failed and
// returns the provider it belongs to. Returns ErrOAuthStateAlreadyUsed when
// the token is unknown or already used.
func (r *OAuthState) MarkFailedByStateToken(
	ctx context.Context,
	stateToken string,
) (uuid.UUID, error) {
	var dbState database.OAuthState
	err := r.DB.WithContext(ctx).
		Where("state_token = ?", stateToken).
		First(&dbState).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, ErrOAuthStateAlreadyUsed
		}
		return uuid.Nil, errs.Wrap(err)
	}
	now := time.Now()
	result := r.DB.WithContext(ctx).
		Model(&database.OAuthState{}).
		Where("id = ? AND used = ?", dbState.ID, false).
		Updates(map[string]interface{}{
			"used":    true,
			"used_at": &now,
			"result":  database.OAUTH_STATE_RESULT_FAILED,
		})
	if result.Error != nil {
		return uuid.Nil, errs.Wrap(result.Error)
	}
	if result.RowsAffected != 1 {
		return uuid.Nil, ErrOAuthStateAlreadyUsed
	}
	return dbState.OAuthProviderID, nil
}

// SetResult records the outcome of the flow for a state
func (r *OAuthState) SetResult(
	ctx context.Context,
	id uuid.UUID,
	stateResult string,
) error {
	result := r.DB.WithContext(ctx).
		Model(&database.OAuthState{}).
		Where("id = ?", id).
		Update("result", stateResult)
	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// DeleteExpired deletes expired oauth state tokens
func (r *OAuthState) DeleteExpired(ctx context.Context) error {
	now := time.Now()
	result := r.DB.WithContext(ctx).
		Where("expires_at < ?", now).
		Delete(&database.OAuthState{})

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}

	return nil
}

// toModel converts database model to domain model
func (r *OAuthState) toModel(dbState *database.OAuthState) *model.OAuthState {
	return model.OAuthStateFromDB(dbState)
}

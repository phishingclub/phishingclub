package repository

import (
	"context"
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

// MarkAsUsed marks a state token as used
func (r *OAuthState) MarkAsUsed(
	ctx context.Context,
	id uuid.UUID,
) error {
	now := time.Now()
	result := r.DB.WithContext(ctx).
		Model(&database.OAuthState{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"used":    true,
			"used_at": &now,
		})

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

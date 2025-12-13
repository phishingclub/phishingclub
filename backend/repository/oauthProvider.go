package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// OAuthProvider is the repository for oauth providers
type OAuthProvider struct {
	DB *gorm.DB
}

// OAuthProviderOption is the option for getting oauth providers
type OAuthProviderOption struct {
	Limit  *int
	Offset *int
	Search *string
}

// Insert inserts a new oauth provider
func (o *OAuthProvider) Insert(ctx context.Context, provider *model.OAuthProvider) (*uuid.UUID, error) {
	m := provider.ToDBMap()
	now := time.Now()
	m["created_at"] = now
	m["updated_at"] = now
	id := uuid.New()
	m["id"] = id

	if err := o.DB.WithContext(ctx).Table("oauth_providers").Create(m).Error; err != nil {
		return nil, err
	}

	return &id, nil
}

// GetAll gets all oauth providers with pagination
func (o *OAuthProvider) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	option *OAuthProviderOption,
) (*model.Result[model.OAuthProvider], error) {
	var dbProviders []database.OAuthProvider
	var totalCount int64

	query := o.DB.WithContext(ctx).Table("oauth_providers")

	if companyID != nil {
		query = query.Where("company_id = ? OR company_id IS NULL", companyID)
	} else {
		query = query.Where("company_id IS NULL")
	}

	if option.Search != nil && *option.Search != "" {
		search := "%" + *option.Search + "%"
		query = query.Where("name ILIKE ?", search)
	}

	if err := query.Count(&totalCount).Error; err != nil {
		return nil, err
	}

	query = query.Order("created_at DESC")

	if option.Limit != nil {
		query = query.Limit(*option.Limit)
	}

	if option.Offset != nil {
		query = query.Offset(*option.Offset)
	}

	if err := query.Find(&dbProviders).Error; err != nil {
		return nil, err
	}

	// convert database types to model types
	providers := make([]*model.OAuthProvider, len(dbProviders))
	for i := range dbProviders {
		providers[i] = ToOAuthProvider(&dbProviders[i])
	}

	hasNextPage := false
	if option.Limit != nil && option.Offset != nil {
		hasNextPage = int64(*option.Offset+*option.Limit) < totalCount
	}

	return &model.Result[model.OAuthProvider]{
		Rows:        providers,
		HasNextPage: hasNextPage,
	}, nil
}

// GetByID gets an oauth provider by id
func (o *OAuthProvider) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*model.OAuthProvider, error) {
	var dbProvider database.OAuthProvider

	if err := o.DB.WithContext(ctx).
		Table("oauth_providers").
		Where("id = ?", id).
		First(&dbProvider).Error; err != nil {
		return nil, err
	}

	return ToOAuthProvider(&dbProvider), nil
}

// GetByNameAndCompanyID gets an oauth provider by name and company id
func (o *OAuthProvider) GetByNameAndCompanyID(
	ctx context.Context,
	name string,
	companyID *uuid.UUID,
) (*model.OAuthProvider, error) {
	var dbProvider database.OAuthProvider

	query := o.DB.WithContext(ctx).
		Table("oauth_providers").
		Where("name = ?", name)

	if companyID != nil {
		query = query.Where("company_id = ?", companyID)
	} else {
		query = query.Where("company_id IS NULL")
	}

	if err := query.First(&dbProvider).Error; err != nil {
		return nil, err
	}

	return ToOAuthProvider(&dbProvider), nil
}

// UpdateByID updates an oauth provider by id
func (o *OAuthProvider) UpdateByID(
	ctx context.Context,
	id uuid.UUID,
	provider *model.OAuthProvider,
) error {
	m := provider.ToDBMap()
	m["updated_at"] = time.Now()

	return o.DB.WithContext(ctx).
		Table("oauth_providers").
		Where("id = ?", id).
		Updates(m).Error
}

// UpdateTokens updates the oauth tokens for a provider
func (o *OAuthProvider) UpdateTokens(
	ctx context.Context,
	id uuid.UUID,
	accessToken string,
	refreshToken string,
	expiresAt time.Time,
) error {
	updates := map[string]interface{}{
		"access_token":     accessToken,
		"refresh_token":    refreshToken,
		"token_expires_at": expiresAt,
		"is_authorized":    true,
		"authorized_at":    time.Now(),
		"updated_at":       time.Now(),
	}

	return o.DB.WithContext(ctx).
		Table("oauth_providers").
		Where("id = ?", id).
		Updates(updates).Error
}

// RemoveAuthorization removes authorization tokens from a provider
func (o *OAuthProvider) RemoveAuthorization(
	ctx context.Context,
	id uuid.UUID,
) error {
	updates := map[string]interface{}{
		"access_token":     nil,
		"refresh_token":    nil,
		"token_expires_at": nil,
		"is_authorized":    false,
		"authorized_at":    nil,
		"authorized_email": nil,
		"updated_at":       time.Now(),
	}

	return o.DB.WithContext(ctx).
		Table("oauth_providers").
		Where("id = ?", id).
		Updates(updates).Error
}

// DeleteByID deletes an oauth provider by id
func (o *OAuthProvider) DeleteByID(
	ctx context.Context,
	id uuid.UUID,
) error {
	return o.DB.WithContext(ctx).
		Table("oauth_providers").
		Where("id = ?", id).
		Delete(&model.OAuthProvider{}).Error
}

// ToOAuthProvider converts database type to model type
func ToOAuthProvider(row *database.OAuthProvider) *model.OAuthProvider {
	id := nullable.NewNullableWithValue(row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString127Must(row.Name))
	authURL := nullable.NewNullableWithValue(*vo.NewString512Must(row.AuthURL))
	tokenURL := nullable.NewNullableWithValue(*vo.NewString512Must(row.TokenURL))
	scopes := nullable.NewNullableWithValue(*vo.NewString512Must(row.Scopes))
	clientID := nullable.NewNullableWithValue(*vo.NewString255Must(row.ClientID))
	clientSecret := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.ClientSecret))
	accessToken := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(row.AccessToken))
	refreshToken := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(row.RefreshToken))
	authorizedEmail := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.AuthorizedEmail))
	isAuthorized := nullable.NewNullableWithValue(row.IsAuthorized)
	isImported := nullable.NewNullableWithValue(row.IsImported)

	return &model.OAuthProvider{
		ID:              id,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		CompanyID:       companyID,
		Name:            name,
		AuthURL:         authURL,
		TokenURL:        tokenURL,
		Scopes:          scopes,
		ClientID:        clientID,
		ClientSecret:    clientSecret,
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		TokenExpiresAt:  row.TokenExpiresAt,
		AuthorizedEmail: authorizedEmail,
		AuthorizedAt:    row.AuthorizedAt,
		IsAuthorized:    isAuthorized,
		IsImported:      isImported,
		Company:         nil,
	}
}

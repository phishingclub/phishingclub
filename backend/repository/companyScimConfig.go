package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
)

// CompanyScimConfig is the repository for company SCIM configuration
type CompanyScimConfig struct {
	DB *gorm.DB
}

// Insert inserts a new company SCIM config row with a pre-computed bcrypt token hash
func (r *CompanyScimConfig) Insert(
	ctx context.Context,
	config *model.CompanyScimConfig,
	tokenHash string,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := map[string]any{}
	row["id"] = id
	AddTimestamps(row)

	row["token_hash"] = tokenHash

	if companyID, err := config.CompanyID.Get(); err == nil {
		row["company_id"] = companyID.String()
	} else {
		return nil, fmt.Errorf("company_id is required")
	}

	if tokenPrefix, err := config.TokenPrefix.Get(); err == nil {
		row["token_prefix"] = tokenPrefix
	} else {
		row["token_prefix"] = ""
	}

	row["enabled"] = config.Enabled

	res := r.DB.
		Model(&database.CompanyScimConfig{}).
		Create(row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetByCompanyID fetches the SCIM config for a given company
func (r *CompanyScimConfig) GetByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) (*model.CompanyScimConfig, error) {
	var row database.CompanyScimConfig
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.COMPANY_SCIM_CONFIG_TABLE, "company_id"),
			),
			companyID.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToCompanyScimConfig(&row), nil
}

// GetByID fetches the SCIM config by its primary key
func (r *CompanyScimConfig) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.CompanyScimConfig, error) {
	var row database.CompanyScimConfig
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_SCIM_CONFIG_TABLE),
			),
			id.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToCompanyScimConfig(&row), nil
}

// UpdateByID performs a partial update on the SCIM config via ToDBMap
func (r *CompanyScimConfig) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	config *model.CompanyScimConfig,
) error {
	row := config.ToDBMap()
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.CompanyScimConfig{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_SCIM_CONFIG_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// UpdateTokenByID replaces the token hash and prefix and bumps updated_at
func (r *CompanyScimConfig) UpdateTokenByID(
	ctx context.Context,
	id *uuid.UUID,
	tokenHash string,
	tokenPrefix string,
) error {
	row := map[string]any{
		"token_hash":   tokenHash,
		"token_prefix": tokenPrefix,
	}
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.CompanyScimConfig{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_SCIM_CONFIG_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// UpdateLastSyncAt sets last_sync_at to the current UTC time for the given config ID
func (r *CompanyScimConfig) UpdateLastSyncAt(
	ctx context.Context,
	id *uuid.UUID,
) error {
	now := time.Now().UTC()
	row := map[string]any{
		"last_sync_at": now,
	}
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.CompanyScimConfig{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_SCIM_CONFIG_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// GetWithTokenHashByCompanyID fetches the full config row and the token hash in a
// single query, used during bearer-token verification.
func (r *CompanyScimConfig) GetWithTokenHashByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) (*model.CompanyScimConfig, string, error) {
	var row database.CompanyScimConfig
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.COMPANY_SCIM_CONFIG_TABLE, "company_id"),
			),
			companyID.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, "", errs.Wrap(res.Error)
	}
	return ToCompanyScimConfig(&row), row.TokenHash, nil
}

// DeleteByCompanyID removes the SCIM config for a given company
func (r *CompanyScimConfig) DeleteByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) error {
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.COMPANY_SCIM_CONFIG_TABLE, "company_id"),
			),
			companyID.String(),
		).
		Delete(&database.CompanyScimConfig{})

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// ToCompanyScimConfig maps a database row to the business model.
// the token field is intentionally left empty — it is never read back from storage.
func ToCompanyScimConfig(row *database.CompanyScimConfig) *model.CompanyScimConfig {
	id := nullable.NewNullableWithValue(*row.ID)

	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}

	tokenPrefix := nullable.NewNullableWithValue(row.TokenPrefix)

	return &model.CompanyScimConfig{
		ID:          id,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		CompanyID:   companyID,
		TokenPrefix: tokenPrefix,
		Enabled:     row.Enabled,
		LastSyncAt:  row.LastSyncAt,
	}
}

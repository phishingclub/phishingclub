package repository

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CampaignDefaults is the repository for the new campaign form defaults
type CampaignDefaults struct {
	DB *gorm.DB
}

// GetByCompanyID returns the defaults row of a company, or the global row
// when companyID is nil
func (r *CampaignDefaults) GetByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) (*model.CampaignDefaults, error) {
	var row database.CampaignDefaults
	res := whereCompany(r.DB.WithContext(ctx), database.CAMPAIGN_DEFAULTS_TABLE, companyID).
		First(&row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToCampaignDefaults(&row), nil
}

// Upsert stores the defaults of a company, or the global row when companyID
// is nil, and returns the stored row
func (r *CampaignDefaults) Upsert(
	ctx context.Context,
	companyID *uuid.UUID,
	defaults *model.CampaignDefaults,
) (*model.CampaignDefaults, error) {
	row := defaults.ToDBMap()
	db := r.DB.WithContext(ctx)
	update := func(id *uuid.UUID) error {
		AddUpdatedAt(row)
		res := db.Model(&database.CampaignDefaults{}).
			Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_DEFAULTS_TABLE, Name: "id"}, Value: id.String()}).
			Updates(row)
		return errs.Wrap(res.Error)
	}
	existing, err := r.GetByCompanyID(ctx, companyID)
	switch {
	case err == nil:
		if err := update(existing.ID); err != nil {
			return nil, err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		insert := map[string]any{}
		for k, v := range row {
			insert[k] = v
		}
		insert["id"] = uuid.New()
		// a NULL company_id is the global row
		insert["company_id"] = nil
		if companyID != nil {
			insert["company_id"] = companyID.String()
		}
		AddTimestamps(insert)
		if res := db.Model(&database.CampaignDefaults{}).Create(insert); res.Error != nil {
			// another save made the row first, the unique index refused this
			// one, so it updates that row instead
			again, getErr := r.GetByCompanyID(ctx, companyID)
			if getErr != nil {
				return nil, errs.Wrap(res.Error)
			}
			if err := update(again.ID); err != nil {
				return nil, err
			}
		}
	default:
		return nil, err
	}
	return r.GetByCompanyID(ctx, companyID)
}

// DeleteByCompanyID removes the defaults of a company, or the global row when
// companyID is nil
func (r *CampaignDefaults) DeleteByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) error {
	res := whereCompany(r.DB.WithContext(ctx), database.CAMPAIGN_DEFAULTS_TABLE, companyID).
		Delete(&database.CampaignDefaults{})
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// ToCampaignDefaults maps a database row to the model
func ToCampaignDefaults(row *database.CampaignDefaults) *model.CampaignDefaults {
	return &model.CampaignDefaults{
		ID:                     row.ID,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		CompanyID:              row.CompanyID,
		CloseAfterDays:         row.CloseAfterDays,
		AnonymizeAfterDays:     row.AnonymizeAfterDays,
		DataAnonymizeAfterDays: row.DataAnonymizeAfterDays,
		IsAnonymous:            row.IsAnonymous,
		SaveSubmittedData:      row.SaveSubmittedData,
	}
}

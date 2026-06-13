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
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// CompanyReportConfig is the repository for company report delivery configuration
type CompanyReportConfig struct {
	DB *gorm.DB
}

// Insert inserts a new company report config row
func (r *CompanyReportConfig) Insert(
	ctx context.Context,
	config *model.CompanyReportConfig,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := config.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	// a NULL company_id is the global default config
	row["company_id"] = nil
	if companyID, err := config.CompanyID.Get(); err == nil {
		row["company_id"] = companyID.String()
	}

	res := r.DB.
		Model(&database.CompanyReportConfig{}).
		Create(row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetByCompanyID fetches the report config for a given company, or the global
// default config when companyID is nil.
func (r *CompanyReportConfig) GetByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) (*model.CompanyReportConfig, error) {
	var row database.CompanyReportConfig
	db := r.DB
	if companyID == nil {
		db = whereCompanyIsNull(db, database.COMPANY_REPORT_CONFIG_TABLE)
	} else {
		db = db.Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.COMPANY_REPORT_CONFIG_TABLE, "company_id"),
			),
			companyID.String(),
		)
	}
	res := db.First(&row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToCompanyReportConfig(&row), nil
}

// GetByID fetches the report config by its primary key
func (r *CompanyReportConfig) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.CompanyReportConfig, error) {
	var row database.CompanyReportConfig
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_REPORT_CONFIG_TABLE),
			),
			id.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToCompanyReportConfig(&row), nil
}

// UpdateByID performs a partial update on the report config via ToDBMap
func (r *CompanyReportConfig) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	config *model.CompanyReportConfig,
) error {
	row := config.ToDBMap()
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.CompanyReportConfig{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_REPORT_CONFIG_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// UpdateLastSentAt sets last_sent_at to the current UTC time for the given config ID
func (r *CompanyReportConfig) UpdateLastSentAt(
	ctx context.Context,
	id *uuid.UUID,
) error {
	now := time.Now().UTC()
	row := map[string]any{
		"last_sent_at": now,
	}
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.CompanyReportConfig{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.COMPANY_REPORT_CONFIG_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// DeleteByCompanyID removes the report config for a given company, or the global
// default config when companyID is nil.
func (r *CompanyReportConfig) DeleteByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) error {
	db := r.DB
	if companyID == nil {
		db = whereCompanyIsNull(db, database.COMPANY_REPORT_CONFIG_TABLE)
	} else {
		db = db.Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.COMPANY_REPORT_CONFIG_TABLE, "company_id"),
			),
			companyID.String(),
		)
	}
	res := db.Delete(&database.CompanyReportConfig{})

	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// ToCompanyReportConfig maps a database row to the business model
func ToCompanyReportConfig(row *database.CompanyReportConfig) *model.CompanyReportConfig {
	id := nullable.NewNullableWithValue(*row.ID)

	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}

	recipientGroupID := nullable.NewNullNullable[uuid.UUID]()
	if row.RecipientGroupID != nil {
		recipientGroupID.Set(*row.RecipientGroupID)
	}

	smtpConfigurationID := nullable.NewNullNullable[uuid.UUID]()
	if row.SMTPConfigurationID != nil {
		smtpConfigurationID.Set(*row.SMTPConfigurationID)
	}

	senderEmail := nullable.NewNullNullable[vo.Email]()
	if row.SenderEmail != "" {
		if email, err := vo.NewEmail(row.SenderEmail); err == nil {
			senderEmail.Set(*email)
		}
	}

	emailSubject := nullable.NewNullableWithValue(row.EmailSubject)
	emailBody := nullable.NewNullableWithValue(row.EmailBody)

	return &model.CompanyReportConfig{
		ID:                  id,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		CompanyID:           companyID,
		Enabled:             row.Enabled,
		SendOnFinish:        row.SendOnFinish,
		RecipientGroupID:    recipientGroupID,
		SMTPConfigurationID: smtpConfigurationID,
		SenderEmail:         senderEmail,
		EmailSubject:        emailSubject,
		EmailBody:           emailBody,
		LastSentAt:          row.LastSentAt,
	}
}

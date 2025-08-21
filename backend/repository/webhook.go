package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var webhookAllowedColumns = assignTableToColumns(database.WEBHOOK_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"allowed",
})

type WebhookOption struct {
	*vo.QueryArgs
}

type Webhook struct {
	DB *gorm.DB
}

// Insert inserts a new webhook
func (r *Webhook) Insert(
	ctx context.Context,
	webhook *model.Webhook,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := webhook.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.
		Model(&database.Webhook{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll gets all webhooks
func (r *Webhook) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *WebhookOption,
) (*model.Result[model.Webhook], error) {
	result := model.NewEmptyResult[model.Webhook]()
	db := withCompanyIncludingNullContext(r.DB, companyID, database.WEBHOOK_TABLE)
	db, err := useQuery(db, database.WEBHOOK_TABLE, options.QueryArgs, webhookAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var rows []*database.Webhook
	res := db.
		Find(&rows)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.WEBHOOK_TABLE, options.QueryArgs, webhookAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, row := range rows {
		result.Rows = append(result.Rows, ToWebhook(row))
	}
	return result, nil
}

// GetAllByCompanyID gets all webhooks
func (r *Webhook) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *WebhookOption,
) ([]*model.Webhook, error) {
	out := []*model.Webhook{}
	db := whereCompany(r.DB, database.WEBHOOK_TABLE, companyID)
	db, err := useQuery(db, database.WEBHOOK_TABLE, options.QueryArgs, webhookAllowedColumns...)
	if err != nil {
		return out, errs.Wrap(err)
	}
	var rows []*database.Webhook
	res := db.
		Find(&rows)

	if res.Error != nil {
		return out, res.Error
	}
	for _, row := range rows {
		out = append(out, ToWebhook(row))
	}
	return out, nil
}

// GetByID gets a webhook by id
func (r *Webhook) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.Webhook, error) {
	var row database.Webhook
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.WEBHOOK_TABLE),
			),
			id.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, res.Error
	}

	return ToWebhook(&row), nil
}

// GetByNames gets webhooks by names
func (r *Webhook) GetByName(
	ctx context.Context,
	name *vo.String127,
) (*model.Webhook, error) {
	var row database.Webhook
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnName(database.WEBHOOK_TABLE),
			),
			name.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, res.Error
	}

	return ToWebhook(&row), nil
}

// UpdateByID updates a webhook by id
func (r *Webhook) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	webhook *model.Webhook,
) error {
	row := webhook.ToDBMap()
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.Webhook{}).
		Where("id = ?", id).
		Updates(row)

	return res.Error
}

// DeleteByID deletes a webhook by id
func (r *Webhook) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ?", id).
		Delete(&database.Webhook{})

	return res.Error
}

func ToWebhook(
	row *database.Webhook,
) *model.Webhook {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString127Must(row.Name))
	url := nullable.NewNullableWithValue(*vo.NewString1024Must(row.URL))
	secret := nullable.NewNullableWithValue(*vo.NewOptionalString1024Must(row.Secret))

	return &model.Webhook{
		ID:        id,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		CompanyID: companyID,
		Name:      name,
		URL:       url,
		Secret:    secret,
	}
}

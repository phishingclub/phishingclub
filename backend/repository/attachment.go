package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var attachmentAllowedColumns = assignTableToColumns(database.ATTACHMENT_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"description",
	"embedded_content",
	"filename",
})

// Attachment is a attachment repository
type Attachment struct {
	DB *gorm.DB
}

// Insert inserts a new attachment
func (r *Attachment) Insert(
	ctx context.Context,
	attachment *model.Attachment,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := attachment.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.Model(&database.Attachment{}).Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAllByContext gets all attachments by global context and company id
func (r *Attachment) GetAllByContext(
	ctx context.Context,
	companyID *uuid.UUID,
	query *vo.QueryArgs,
) (*model.Result[model.Attachment], error) {
	result := model.NewEmptyResult[model.Attachment]()
	db, err := useQuery(r.DB, database.ATTACHMENT_TABLE, query, attachmentAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbModels []database.Attachment
	dbRes := db.
		Where("(company_id = ? OR company_id IS NULL)", companyID).
		Find(&dbModels)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.ATTACHMENT_TABLE, query, attachmentAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbModel := range dbModels {
		result.Rows = append(result.Rows, ToAttachment(&dbModel))
	}
	return result, nil
}

// GetAllByGlobalContext gets all global attachments
func (r *Attachment) GetAllByGlobalContext(
	ctx context.Context,
	query *vo.QueryArgs,
) (*model.Result[model.Attachment], error) {
	result := model.NewEmptyResult[model.Attachment]()
	var dbModels []database.Attachment
	db, err := useQuery(r.DB, database.ATTACHMENT_TABLE, query, attachmentAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.
		Where("company_id IS NULL").
		Find(&dbModels)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.ATTACHMENT_TABLE, query, attachmentAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbModel := range dbModels {
		result.Rows = append(result.Rows, ToAttachment(&dbModel))
	}
	return result, nil
}

// GetByID gets an attachment by id
func (r *Attachment) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.Attachment, error) {
	var dbModel database.Attachment
	result := r.DB.Where("id = ?", id).First(&dbModel)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToAttachment(&dbModel), nil
}

// UpdateByID updates an attachment by id
func (r *Attachment) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	attachment *model.Attachment,
) error {
	row := attachment.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.Model(&database.Attachment{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes an attachment by id
func (r *Attachment) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := r.DB.Where("id = ?", id).Delete(&database.Attachment{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}

// ToAttachment converts a attachment database row to a model
func ToAttachment(row *database.Attachment) *model.Attachment {
	id := nullable.NewNullableWithValue(*row.ID)
	name := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Name),
	)
	description := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.Description),
	)
	filename := nullable.NewNullableWithValue(
		*vo.NewFileNameMust(row.Filename),
	)
	embeddedContent := nullable.NewNullableWithValue(row.EmbeddedContent)
	attachment := &model.Attachment{
		ID:              id,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Name:            name,
		Description:     description,
		FileName:        filename,
		EmbeddedContent: embeddedContent,
	}

	attachment.CompanyID = nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		attachment.CompanyID.Set(*row.CompanyID)
	}

	return attachment
}

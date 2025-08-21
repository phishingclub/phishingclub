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

var RecipientGroupAllowedColumns = assignTableToColumns(database.RECIPIENT_GROUP_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
})

// RecipientGroupOption is a recipient group option
type RecipientGroupOption struct {
	*vo.QueryArgs

	WithCompany        bool
	WithRecipients     bool
	WithRecipientCount bool
}

// RecipientGroup is a recipient group repository
type RecipientGroup struct {
	DB *gorm.DB
}

// preload loads relational data
func (rg *RecipientGroup) preload(
	options *RecipientGroupOption,
	db *gorm.DB,
) *gorm.DB {
	if options.WithCompany {
		db = db.Preload("Company")
	}
	if options.WithRecipients {
		db = db.Preload("Recipients")
	}
	return db
}

// Insert inserts a new recipient group
func (rg *RecipientGroup) Insert(
	ctx context.Context,
	recipientGroup *model.RecipientGroup,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := recipientGroup.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := rg.DB.
		Model(&database.RecipientGroup{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// AddRecipients adds recipients to a recipient group
func (rg *RecipientGroup) AddRecipients(
	ctx context.Context,
	groupID *uuid.UUID,
	recipients []*uuid.UUID,
) error {
	for _, recipientID := range recipients {
		/* when performing the below optizmie we can handle the whole batch as a single operation...
		batch = append(batch, database.RecipientGroupRecipient{
			RecipientID:      recipientID,
			RecipientGroupID: groupID,
		})
		*/

		var c int64
		// check if the recipient already exists, if so, skip
		res := rg.DB.
			Model(&database.RecipientGroupRecipient{}).
			Where("recipient_id = ? AND recipient_group_id = ?", recipientID, groupID).
			Count(&c)

		if res.Error != nil {
			return res.Error
		}
		// if already in group, skip it
		if c > 0 {
			continue
		}
		res = rg.DB.
			Model(&database.RecipientGroupRecipient{}).
			Create(&database.RecipientGroupRecipient{
				RecipientID:      recipientID,
				RecipientGroupID: groupID,
			})
		if res.Error != nil {
			return res.Error
		}
	}

	/* TODO OPTIMIZE - This very slow implementation is written like this because it was faster to write
		   than it is to setup and handle different databases sqlite, mysql, postgres.
	       Optmize this away by checking which db type we are using and using the correct query such as
	       IGNORE on mysql and postgres and ON CONFLICT IGNORE on sqlite or something like that..
	*/
	// clause.Insert ignores unique constraint violations so they do not get created, but they do not error
	// does not work in sqlite
	/*
		result := db.Clauses(clause.Insert{Modifier: "OR IGNORE"}).Create(&batch)
		if result.Error != nil {
			return result.Error
		}
	*/

	return nil
}

// countRecipients gets the count of recipient groups
func (rg *RecipientGroup) countRecipients(
	ctx context.Context,
	group *database.RecipientGroup,
	options *RecipientGroupOption,
) (int64, error) {
	count := model.RECIPIENT_COUNT_NOT_LOADED
	if options.WithRecipientCount {
		// if recipients is loaded then we can get the count from the slice
		if options.WithRecipients {
			count = int64(len(group.Recipients))
		} else {
			// otherwise we need to query the storage
			c, err := rg.GetRecipientCount(ctx, group.ID)
			if err != nil {
				return count, errs.Wrap(err)
			}
			count = c
		}
	}
	return count, nil
}

// GetAll gets all recipient groups with pagination
func (rg *RecipientGroup) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RecipientGroupOption,
) (*model.Result[model.RecipientGroup], error) {
	result := model.NewEmptyResult[model.RecipientGroup]()
	db := rg.preload(options, rg.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.RECIPIENT_GROUP_TABLE)
	db, err := useQuery(
		db,
		database.RECIPIENT_GROUP_TABLE,
		options.QueryArgs,
		RecipientGroupAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}

	var rows []database.RecipientGroup
	dbRes := db.Find(&rows)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.RECIPIENT_GROUP_TABLE,
		options.QueryArgs,
		RecipientGroupAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, recipientGroup := range rows {
		count, err := rg.countRecipients(ctx, &recipientGroup, options)
		if err != nil {
			return result, errs.Wrap(err)
		}
		recipient, err := ToRecipientGroup(&recipientGroup)
		if err != nil {
			return nil, errs.Wrap(err)
		}

		c := nullable.NewNullNullable[int64]()
		if count != model.RECIPIENT_COUNT_NOT_LOADED {
			c.Set(count)
		}
		recipient.RecipientCount = c
		result.Rows = append(result.Rows, recipient)

	}

	return result, nil
}

// GetAllByCompanyID gets all recipient groups with pagination by company ID
func (rg *RecipientGroup) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RecipientGroupOption,
) ([]*model.RecipientGroup, error) {
	recipientGroups := []*model.RecipientGroup{}
	var rows []database.RecipientGroup
	db := rg.preload(options, rg.DB)
	db = whereCompany(db, database.RECIPIENT_GROUP_TABLE, companyID)
	db, err := useQuery(
		db,
		database.RECIPIENT_GROUP_TABLE,
		options.QueryArgs,
		RecipientGroupAllowedColumns...,
	)
	if err != nil {
		return recipientGroups, errs.Wrap(err)
	}
	result := db.Find(&rows)

	if result.Error != nil {
		return []*model.RecipientGroup{}, result.Error
	}
	for _, recipientGroup := range rows {
		count, err := rg.countRecipients(ctx, &recipientGroup, options)
		if err != nil {
			return recipientGroups, errs.Wrap(err)
		}
		recipient, err := ToRecipientGroup(&recipientGroup)
		if err != nil {
			return nil, errs.Wrap(err)
		}

		c := nullable.NewNullNullable[int64]()
		if count != model.RECIPIENT_COUNT_NOT_LOADED {
			c.Set(count)
		}
		recipient.RecipientCount = c
		recipientGroups = append(recipientGroups, recipient)

	}
	return recipientGroups, nil
}

// GetRecipientCount gets the recipient count of a recipient group
func (rg *RecipientGroup) GetRecipientCount(
	ctx context.Context,
	groupID *uuid.UUID,
) (int64, error) {
	var count int64
	result := rg.DB.
		Model(&database.RecipientGroupRecipient{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.RECIPIENT_GROUP_RECIPIENT_TABLE, "recipient_group_id"),
			),
			groupID.String(),
		).
		Count(&count)
	if result.Error != nil {
		return 0, result.Error
	}
	return count, nil
}

// GetByID gets a recipient group by id
func (rg *RecipientGroup) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *RecipientGroupOption,
) (*model.RecipientGroup, error) {
	var rows database.RecipientGroup
	db := rg.preload(options, rg.DB)
	result := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.RECIPIENT_GROUP_TABLE),
			),
			id.String(),
		).
		First(&rows)

	if result.Error != nil {
		return nil, result.Error
	}
	count, err := rg.countRecipients(
		ctx,
		&rows,
		options,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	recipientGroup, err := ToRecipientGroup(&rows)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	c := nullable.NewNullNullable[int64]()
	if count != model.RECIPIENT_COUNT_NOT_LOADED {
		c.Set(count)
	}
	recipientGroup.RecipientCount = c
	return recipientGroup, nil
}

// GetByNameAndCompanyID gets a recipient group by name
func (rg *RecipientGroup) GetByNameAndCompanyID(
	ctx context.Context,
	name string,
	companyID *uuid.UUID,
	options *RecipientGroupOption,
) (*model.RecipientGroup, error) {
	var recipientGroup database.RecipientGroup
	db := rg.preload(options, rg.DB)
	whereCompany := fmt.Sprintf(
		"%s IS NULL",
		TableColumn(database.RECIPIENT_GROUP_TABLE, "company_id"),
	)
	if companyID != nil {
		whereCompany = fmt.Sprintf(
			"%s = ?",
			TableColumn(database.RECIPIENT_GROUP_TABLE, "company_id"),
		)
	}
	result := db.
		Where(
			fmt.Sprintf(
				"%s = ? AND %s",
				TableColumnName(database.RECIPIENT_GROUP_TABLE),
				whereCompany,
			),
			name,
			companyID,
		).
		First(&recipientGroup)

	if result.Error != nil {
		return nil, result.Error
	}
	count, err := rg.countRecipients(
		ctx,
		&recipientGroup,
		options,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	recpGroup, err := ToRecipientGroup(&recipientGroup)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	c := nullable.NewNullNullable[int64]()
	if count != model.RECIPIENT_COUNT_NOT_LOADED {
		c.Set(count)
	}
	recpGroup.RecipientCount = c
	return recpGroup, nil
}

// GetRecipientsByGroupID gets recipients by recipient group id
func (rg *RecipientGroup) GetRecipientsByGroupID(
	ctx context.Context,
	id *uuid.UUID,
	options *RecipientOption,
) (*model.Result[model.Recipient], error) {
	result := model.NewEmptyResult[model.Recipient]()
	db := rg.DB
	var recipients []database.Recipient
	if options.WithCompany {
		db = db.Preload("Company")
	}
	db, err := useQuery(db, database.RECIPIENT_TABLE, options.QueryArgs, allowdRecipientColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.
		Model(&database.Recipient{}).
		Joins("JOIN recipient_group_recipients ON recipient_group_recipients.recipient_id = recipients.id").
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.RECIPIENT_GROUP_RECIPIENT_TABLE, "recipient_group_id"),
			),
			id.String(),
		).
		Find(&recipients)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(
		db, database.RECIPIENT_TABLE, options.QueryArgs, allowdRecipientColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, recipient := range recipients {
		r, err := ToRecipient(&recipient)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, r)
	}
	return result, nil
}

// UpdateByID updates a recipient group by id
func (rg *RecipientGroup) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	recipientGroup *model.RecipientGroup,
) error {
	row := recipientGroup.ToDBMap()
	AddUpdatedAt(row)
	res := rg.DB.
		Model(&database.RecipientGroup{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.RECIPIENT_GROUP_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveRecipientByIDFromAllGroups removes a recipient from all recipient groups
func (rg *RecipientGroup) RemoveRecipientByIDFromAllGroups(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	result := rg.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.RECIPIENT_GROUP_RECIPIENT_TABLE, "recipient_id"),
			),
			recipientID.String(),
		).
		Delete(&database.RecipientGroupRecipient{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}

// RemoveRecipients removes a recipient from a recipient group
func (rg *RecipientGroup) RemoveRecipients(
	ctx context.Context,
	groupID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) error {
	result := rg.DB.
		Where("recipient_group_id = ? AND recipient_id IN ?", groupID, recipientIDs).
		Delete(&database.RecipientGroupRecipient{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}

// DeleteByID deletes a recipient group by id
func (rg *RecipientGroup) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	// delete recipients
	res := rg.DB.
		Where("recipient_group_id = ?", id).
		Delete(&database.RecipientGroupRecipient{})

	if res.Error != nil {
		return res.Error
	}
	// delete recipient group
	res = rg.DB.
		Where("id = ?", id).
		Delete(&database.RecipientGroup{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

func ToRecipientGroup(row *database.RecipientGroup) (*model.RecipientGroup, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString127Must(row.Name))
	recipients := []*model.Recipient{}
	if len(row.Recipients) > 0 {
		for _, recipient := range row.Recipients {
			r, err := ToRecipient(&recipient)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			recipients = append(recipients, r)
		}
	}

	return &model.RecipientGroup{
		ID:             id,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		Name:           name,
		CompanyID:      companyID,
		Recipients:     recipients,
		RecipientCount: nullable.NewNullNullable[int64](),
	}, nil
}

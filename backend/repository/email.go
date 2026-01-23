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

var allowedEmailOrderBy = assignTableToColumns(database.EMAIL_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"mail_from", // envelope from
	"from",
	"subject",
	"add_tracking_pixel",
})

// EmailOption is for deciding if we should load full email entities
type EmailOption struct {
	*vo.QueryArgs
	WithCompany     bool
	WithAttachments bool
}

// Email is a Email repository
type Email struct {
	DB *gorm.DB
}

// load preloads the table relations
func (m *Email) load(
	db *gorm.DB,
	options *EmailOption,
) *gorm.DB {
	if options.WithCompany {
		db = db.Preload("Company")
	}
	if options.WithAttachments {
		db = db.Preload("Attachments")
	}
	return db
}

// AddAttachment adds an attachment to a email
func (m *Email) AddAttachment(
	ctx context.Context,
	emailID *uuid.UUID,
	attachmentID *uuid.UUID,
	isInline bool,
) error {
	result := m.DB.Create(
		&database.EmailAttachment{
			EmailID:      emailID,
			AttachmentID: attachmentID,
			IsInline:     isInline,
		},
	)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// RemoveAttachment removes an attachment from a email
func (m *Email) RemoveAttachment(
	ctx context.Context,
	emailID *uuid.UUID,
	attachmentID *uuid.UUID,
) error {
	result := m.DB.Delete(
		&database.EmailAttachment{},
		"email_id = ? AND attachment_id = ?",
		emailID,
		attachmentID,
	)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// GetAttachmentIDsByEmailID gets all attachment IDs associated with an email
func (m *Email) GetAttachmentIDsByEmailID(
	ctx context.Context,
	emailID uuid.UUID,
) ([]*uuid.UUID, error) {
	var emailAttachments []database.EmailAttachment
	result := m.DB.Where("email_id = ?", emailID).Find(&emailAttachments)
	if result.Error != nil {
		return nil, result.Error
	}

	attachmentIDs := make([]*uuid.UUID, len(emailAttachments))
	for i, ea := range emailAttachments {
		attachmentIDs[i] = ea.AttachmentID
	}
	return attachmentIDs, nil
}

// GetEmailAttachments gets all email-attachment relationships for an email including isInline status
func (m *Email) GetEmailAttachments(
	ctx context.Context,
	emailID uuid.UUID,
) ([]database.EmailAttachment, error) {
	var emailAttachments []database.EmailAttachment
	result := m.DB.Where("email_id = ?", emailID).Find(&emailAttachments)
	if result.Error != nil {
		return nil, result.Error
	}
	return emailAttachments, nil
}

// RemoveAttachment removes an attachments from a email by attachment ID
func (m *Email) RemoveAttachmentsByAttachmentID(
	ctx context.Context,
	attachmentID *uuid.UUID,
) error {
	result := m.DB.Delete(
		&database.EmailAttachment{},
		"attachment_id = ?",
		attachmentID,
	)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// Insert inserts a new email
func (m *Email) Insert(
	ctx context.Context,
	email *model.Email,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := email.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := m.DB.
		Model(&database.Email{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetByID gets a email by ID
func (m *Email) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *EmailOption,
) (*model.Email, error) {
	dbEmail := database.Email{}
	db := m.load(m.DB, options)
	result := db.
		Where("id = ?", id).
		First(&dbEmail)
	if result.Error != nil {
		return nil, result.Error
	}
	return ToEmail(&dbEmail), nil
}

// GetAll gets all emails
func (m *Email) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *EmailOption,
) (*model.Result[model.Email], error) {
	result := model.NewEmptyResult[model.Email]()
	dbEmails := []database.Email{}
	db := m.load(m.DB, options)
	db = withCompanyIncludingNullContext(db, companyID, database.EMAIL_TABLE)
	db, err := useQuery(db, database.EMAIL_TABLE, options.QueryArgs, allowedEmailOrderBy...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.Find(&dbEmails)
	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.EMAIL_TABLE, options.QueryArgs, allowedEmailOrderBy...)

	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbEmail := range dbEmails {
		em := ToEmail(&dbEmail)
		result.Rows = append(result.Rows, em)
	}
	return result, nil
}

// GetAllByCompanyID gets all emails by company id
func (m *Email) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *EmailOption,
) (*model.Result[model.Email], error) {
	result := model.NewEmptyResult[model.Email]()
	dbEmails := []database.Email{}
	db := m.load(m.DB, options)
	db = whereCompany(db, database.EMAIL_TABLE, companyID)
	db, err := useQuery(db, database.EMAIL_TABLE, options.QueryArgs, allowedEmailOrderBy...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.Find(&dbEmails)
	if dbRes.Error != nil {
		return nil, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.EMAIL_TABLE, options.QueryArgs, allowedEmailOrderBy...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbEmail := range dbEmails {
		em := ToEmail(&dbEmail)
		result.Rows = append(result.Rows, em)
	}
	return result, nil
}

// GetOverviews gets all emails but without content
func (m *Email) GetOverviews(
	ctx context.Context,
	companyID *uuid.UUID,
	options *EmailOption,
) (*model.Result[model.Email], error) {
	result := model.NewEmptyResult[model.Email]()
	dbEmails := []database.Email{}
	db := m.load(m.DB, options)
	db = withCompanyIncludingNullContext(db, companyID, database.EMAIL_TABLE)
	db, err := useQuery(db, database.EMAIL_TABLE, options.QueryArgs, allowedEmailOrderBy...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.
		Omit(
			TableColumn(database.EMAIL_TABLE, "content"),
		).
		Find(&dbEmails)
	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.EMAIL_TABLE, options.QueryArgs, allowedEmailOrderBy...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbEmail := range dbEmails {
		em := ToEmail(&dbEmail)
		result.Rows = append(result.Rows, em)
	}
	return result, nil
}

// GetByNameAndCompanyID gets a email by name
func (m *Email) GetByNameAndCompanyID(
	ctx context.Context,
	name *vo.String64,
	companyID *uuid.UUID, // can be null
	options *EmailOption,
) (*model.Email, error) {
	dbEmail := database.Email{}
	db := m.load(m.DB, options)
	var result *gorm.DB
	if companyID == nil {
		result = db.
			Where(
				fmt.Sprintf(
					"%s = ? AND %s IS NULL",
					TableColumn(database.EMAIL_TABLE, "name"),
					TableColumn(database.EMAIL_TABLE, "company_id"),
				),
				name.String(),
			).
			First(&dbEmail)
	} else {
		result = db.
			Where(
				fmt.Sprintf(
					"%s = ? AND %s = ?",
					TableColumn(database.EMAIL_TABLE, "name"),
					TableColumn(database.EMAIL_TABLE, "company_id"),
				),
				name.String(),
				companyID.String(),
			).
			First(&dbEmail)
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return ToEmail(&dbEmail), nil
}

// UpdateByID updates a email by ID
func (m *Email) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	email *model.Email,
) error {
	row := email.ToDBMap()
	AddUpdatedAt(row)
	res := m.DB.
		Model(&database.Email{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a email by ID
func (m *Email) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := m.DB.
		Delete(&database.Email{}, id)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func ToEmail(row *database.Email) *model.Email {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	subject := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.Subject))
	envelopeFrom := nullable.NewNullableWithValue(*vo.NewMailEnvelopeFromMust(row.MailFrom))
	from := nullable.NewNullableWithValue(*vo.NewEmailMust(row.From))
	content := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(row.Content))
	addTrackingPixel := nullable.NewNullableWithValue(row.AddTrackingPixel)

	// attachments are loaded separately via loadEmailAttachmentsWithContext
	// which properly loads EmailAttachment with isInline status from junction table
	return &model.Email{
		ID:                id,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		Name:              name,
		MailHeaderSubject: subject,
		MailEnvelopeFrom:  envelopeFrom,
		MailHeaderFrom:    from,
		Content:           content,
		AddTrackingPixel:  addTrackingPixel,
		CompanyID:         companyID,
		Attachments:       []*model.EmailAttachment{},
	}
}

func ToEmailOverview(row *database.Email) *model.EmailOverview {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	subject := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.Subject))
	envelopeFrom := nullable.NewNullableWithValue(*vo.NewMailEnvelopeFromMust(row.MailFrom))
	from := nullable.NewNullableWithValue(*vo.NewEmailMust(row.From))
	addTrackingPixel := nullable.NewNullableWithValue(row.AddTrackingPixel)

	return &model.EmailOverview{
		ID:                id,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		Name:              name,
		MailHeaderSubject: subject,
		MailEnvelopeFrom:  envelopeFrom,
		MailHeaderFrom:    from,
		AddTrackingPixel:  addTrackingPixel,
		CompanyID:         companyID,
	}
}

package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProgramEnrollmentRecipientAlias is the alias GORM gives the joined recipient
// of an enrollment, used to sort and search the people of a program.
const ProgramEnrollmentRecipientAlias = "Recipient"

// ProgramEnrollmentSortColumns are the columns the program people table may be
// sorted by, keyed by the name the frontend sends.
var ProgramEnrollmentSortColumns = map[string]string{
	"email":       TableColumn(ProgramEnrollmentRecipientAlias, "email"),
	"name":        TableColumn(ProgramEnrollmentRecipientAlias, "first_name"),
	"status":      TableColumn(database.PROGRAM_ENROLLMENT_TABLE, "status"),
	"next_due_at": TableColumn(database.PROGRAM_ENROLLMENT_TABLE, "next_due_at"),
}

var allowedProgramEnrollmentColumns = func() []string {
	out := make([]string, 0, len(ProgramEnrollmentSortColumns))
	for _, v := range ProgramEnrollmentSortColumns {
		out = append(out, v)
	}
	return out
}()

// ProgramCampaignInfo is one hidden campaign of a program
type ProgramCampaignInfo struct {
	ID                 uuid.UUID
	IsTraining         bool
	CampaignTemplateID *uuid.UUID
}

// ProgramSendRow is one sent message of a program, phishing or training
type ProgramSendRow struct {
	ID           uuid.UUID
	CampaignID   uuid.UUID
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	SentAt       time.Time
}

// ProgramEventRow is one event on a program campaign
type ProgramEventRow struct {
	CampaignID   uuid.UUID
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	EventID      uuid.UUID
	CreatedAt    time.Time
}

// GetCampaignInfos returns every hidden campaign of a program with its kind and
// the template it sends
func (r *Program) GetCampaignInfos(ctx context.Context, programID *uuid.UUID) ([]ProgramCampaignInfo, error) {
	rows := []ProgramCampaignInfo{}
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("id", "is_training", "campaign_template_id").
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// idValues turns ids into query values, skipping nil ones
func idValues(ids []*uuid.UUID) []any {
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		if id != nil {
			values = append(values, *id)
		}
	}
	return values
}

// GetSentRows returns every sent message of a program. When recipientIDs is
// not nil only those recipients' messages are returned.
func (r *Program) GetSentRows(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) ([]ProgramSendRow, error) {
	rows := []ProgramSendRow{}
	db := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "campaign_id", "recipient_id", "anonymized_id", "sent_at").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil})
	if recipientIDs != nil {
		db = db.Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: idValues(recipientIDs)})
	}
	if err := db.Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// GetEventRows returns the given events on every campaign of a program. When
// recipientIDs is not nil only those recipients' events are returned.
func (r *Program) GetEventRows(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	eventIDs []*uuid.UUID,
) ([]ProgramEventRow, error) {
	rows := []ProgramEventRow{}
	values := make([]any, 0, len(eventIDs))
	for _, id := range eventIDs {
		if id != nil {
			values = append(values, *id)
		}
	}
	if len(values) == 0 {
		return rows, nil
	}
	db := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Select("campaign_id", "recipient_id", "anonymized_id", "event_id", "created_at").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(clause.IN{Column: clause.Column{Name: "event_id"}, Values: values})
	if recipientIDs != nil {
		db = db.Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: idValues(recipientIDs)})
	}
	if err := db.Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// programPhishingCampaignIDs selects the ids of a program's phishing campaigns
func (r *Program) programPhishingCampaignIDs(programID *uuid.UUID) *gorm.DB {
	return r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "is_training"}, Value: false})
}

// GetLatestPhishingSend returns the latest sent phishing message of a program
// to one person, sent at or before the given time when it is set. The person is
// matched by recipient id or by pseudonym, as an anonymous program row may only
// carry the pseudonym.
func (r *Program) GetLatestPhishingSend(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
	before *time.Time,
) (*model.CampaignRecipient, error) {
	who := []clause.Expression{}
	if recipientID != nil {
		who = append(who, clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID})
	}
	if anonymizedID != nil {
		who = append(who, clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: anonymizedID})
	}
	if len(who) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	db := r.DB.WithContext(ctx).
		Where("campaign_id IN (?)", r.programPhishingCampaignIDs(programID)).
		Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Where(clause.Or(who...))
	if before != nil {
		// sent_at is stored as RFC 3339 text, so compare in that form
		db = db.Where(clause.Lte{Column: clause.Column{Name: "sent_at"}, Value: utils.RFC3339UTC(*before)})
	}
	row := database.CampaignRecipient{}
	res := db.Order(clause.OrderByColumn{Column: clause.Column{Name: "sent_at"}, Desc: true}).
		Limit(1).
		Find(&row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return ToCampaignRecipient(&row)
}

// ProgramEnrollmentStatusCount is how many enrollments of a program have a status
type ProgramEnrollmentStatusCount struct {
	Status string
	Count  int
}

// GetEnrollmentStatusCounts counts the enrollments of a program by status
func (r *Program) GetEnrollmentStatusCounts(
	ctx context.Context,
	programID *uuid.UUID,
) ([]ProgramEnrollmentStatusCount, error) {
	rows := []ProgramEnrollmentStatusCount{}
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("status", "COUNT(*) AS count").
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Group("status").
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// GetEnrollmentPage returns enrollments of a program with their recipient,
// searched on email and name and sorted by an allowlisted column. A query
// without a limit returns every match. The count is the number of matches.
func (r *Program) GetEnrollmentPage(
	ctx context.Context,
	programID *uuid.UUID,
	q *vo.QueryArgs,
) ([]*database.ProgramEnrollment, int64, error) {
	base := func() *gorm.DB {
		db := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Joins(ProgramEnrollmentRecipientAlias).
			Where(clause.Eq{
				Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"},
				Value:  programID,
			})
		if q != nil && strings.TrimSpace(q.Search) != "" {
			search := strings.ReplaceAll(strings.TrimSpace(q.Search), "%", " ")
			search = strings.ReplaceAll(search, "_", " ")
			like := "%" + search + "%"
			db = db.Where(clause.Or(
				clause.Like{Column: clause.Column{Table: ProgramEnrollmentRecipientAlias, Name: "email"}, Value: like},
				clause.Like{Column: clause.Column{Table: ProgramEnrollmentRecipientAlias, Name: "first_name"}, Value: like},
				clause.Like{Column: clause.Column{Table: ProgramEnrollmentRecipientAlias, Name: "last_name"}, Value: like},
			))
		}
		return db
	}
	var count int64
	if err := base().Count(&count).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	db := base()
	if q != nil {
		if q.OrderBy != "" {
			var err error
			db, err = WithOrderBy(db, q.OrderBy, q.Desc, allowedProgramEnrollmentColumns...)
			if err != nil {
				return nil, 0, errs.NewValidationError(err)
			}
		}
		db = withOffsetLimit(db, q.Offset, q.Limit)
	}
	// a stable tie break so pages do not overlap
	db = db.Order(clause.OrderByColumn{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "id"}})
	rows := []*database.ProgramEnrollment{}
	if err := db.Find(&rows).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return rows, count, nil
}

// GetEnrollmentsByRecipientID returns every enrollment of a recipient
func (r *Program) GetEnrollmentsByRecipientID(
	ctx context.Context,
	recipientID *uuid.UUID,
) ([]*database.ProgramEnrollment, error) {
	rows := []*database.ProgramEnrollment{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

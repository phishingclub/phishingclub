package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// recipientProgramAlias is the alias GORM gives the joined program of an
// enrollment
const recipientProgramAlias = "Program"

// RecipientProgramSortColumns are the sort names the programs of a recipient
// may be sorted by, keyed by the name the frontend sends
var RecipientProgramSortColumns = map[string]string{
	"name": "name",
}

// recipientProgramOrder maps a sort name onto its column
var recipientProgramOrder = map[string]clause.Column{
	"name": {Table: recipientProgramAlias, Name: "name"},
}

// RecipientProgramRow is one program a recipient is enrolled in with the
// enrollment
type RecipientProgramRow struct {
	Program    *model.Program
	Enrollment *database.ProgramEnrollment
}

// GetRecipientProgramPage returns one page of the programs a recipient is
// enrolled in, read with the programs in one query. When recipientCompany is
// set only programs of that company and global programs are listed. The
// order is one of RecipientProgramSortColumns, by name when none is given.
// An anonymized program is left out. It reports whether a next page exists.
func (r *Program) GetRecipientProgramPage(
	ctx context.Context,
	recipientID *uuid.UUID,
	recipientCompany *uuid.UUID,
	q *vo.QueryArgs,
) ([]*RecipientProgramRow, bool, error) {
	db := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Joins(recipientProgramAlias).
		Where(clause.Eq{
			Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
			Value:  recipientID,
		})
	// an anonymized program no longer links a person to it, as an anonymized
	// campaign
	db = db.Where(clause.Eq{Column: clause.Column{Table: recipientProgramAlias, Name: "anonymized_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Table: recipientProgramAlias, Name: "anonymizing_at"}, Value: nil})
	if recipientCompany != nil {
		column := clause.Column{Table: recipientProgramAlias, Name: "company_id"}
		db = db.Where(clause.Or(
			clause.Eq{Column: column, Value: nil},
			clause.Eq{Column: column, Value: recipientCompany},
		))
	}
	orderBy := "name"
	desc := false
	if q != nil {
		if q.OrderBy != "" {
			orderBy = q.OrderBy
		}
		desc = q.Desc
	}
	column, ok := recipientProgramOrder[orderBy]
	if !ok {
		return nil, false, errs.NewValidationError(errors.New("unknown sort column"))
	}
	// the enrollment id is a stable tie break so pages do not overlap
	order := "? COLLATE NOCASE ASC, ?"
	if desc {
		order = "? COLLATE NOCASE DESC, ?"
	}
	db = db.Order(clause.OrderBy{Expression: clause.Expr{
		SQL:  order,
		Vars: []any{column, clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "id"}},
	}})
	if q != nil && q.Offset > 0 {
		db = db.Offset(q.Offset)
	}
	rows := []*database.ProgramEnrollment{}
	if err := withPeek(db, q).Find(&rows).Error; err != nil {
		return nil, false, errs.Wrap(err)
	}
	hasNextPage := trimPeek(&rows, q)
	out := make([]*RecipientProgramRow, 0, len(rows))
	for _, row := range rows {
		if row.Program == nil {
			continue
		}
		program, err := ToProgram(row.Program)
		if err != nil {
			return nil, false, errs.Wrap(err)
		}
		out = append(out, &RecipientProgramRow{Program: program, Enrollment: row})
	}
	return out, hasNextPage, nil
}

// ProgramCampaignOf is a hidden campaign with the program that owns it
type ProgramCampaignOf struct {
	ID         uuid.UUID
	ProgramID  uuid.UUID
	IsTraining bool
}

// campaignIDsOfPrograms selects the ids of the hidden campaigns of programs
func (r *Program) campaignIDsOfPrograms(programIDs []*uuid.UUID) *gorm.DB {
	return r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.IN{Column: clause.Column{Name: "program_id"}, Values: uuidValues(programIDs)})
}

// GetCampaignsOfPrograms returns the hidden campaigns of programs with their
// program and kind
func (r *Program) GetCampaignsOfPrograms(ctx context.Context, programIDs []*uuid.UUID) ([]ProgramCampaignOf, error) {
	rows := []ProgramCampaignOf{}
	if len(programIDs) == 0 {
		return rows, nil
	}
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("id", "program_id", "is_training").
		Where(clause.IN{Column: clause.Column{Name: "program_id"}, Values: uuidValues(programIDs)}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// EachRecipientSentRow calls fn with every sent message of one recipient in
// the hidden campaigns of programs. The rows are found through the recipient
// index. The row passed to fn is reused, fn must copy what it keeps.
func (r *Program) EachRecipientSentRow(
	ctx context.Context,
	recipientID *uuid.UUID,
	programIDs []*uuid.UUID,
	fn func(*ProgramSendRow) error,
) error {
	if len(programIDs) == 0 {
		return nil
	}
	rows, err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "campaign_id", "recipient_id", "anonymized_id", "sent_at", "notable_event_id", "language").
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), r.campaignIDsOfPrograms(programIDs)}}).
		Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	return scanSentRows(rows, fn)
}

// EachRecipientEventRow calls fn with the given events of one recipient in the
// hidden campaigns of programs. The rows are found through the recipient
// index. The row passed to fn is reused, fn must copy what it keeps.
func (r *Program) EachRecipientEventRow(
	ctx context.Context,
	recipientID *uuid.UUID,
	programIDs []*uuid.UUID,
	eventIDs []*uuid.UUID,
	fn func(*ProgramEventRow) error,
) error {
	values := uuidValues(eventIDs)
	if len(values) == 0 || len(programIDs) == 0 {
		return nil
	}
	rows, err := selectEventRow(r.DB.WithContext(ctx).Model(&database.CampaignEvent{})).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Where(clause.IN{Column: clause.Column{Name: "event_id"}, Values: values}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), r.campaignIDsOfPrograms(programIDs)}}).
		Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	return scanEventRows(rows, fn)
}

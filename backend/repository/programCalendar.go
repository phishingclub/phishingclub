package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm/clause"
)

// GetCalendarEntries returns the programs in scope that run some time between
// start and end, without their status, which the service sets. A program runs from its start to its close, or to its end
// when it is not closed, or on with neither. Drafts have no start and are left
// out. Without a company it reads every company, like the campaign calendar.
func (r *Program) GetCalendarEntries(
	ctx context.Context,
	companyID *uuid.UUID,
	start time.Time,
	end time.Time,
	options *ProgramOption,
) (*model.Result[model.ProgramCalendarEntry], error) {
	result := model.NewEmptyResult[model.ProgramCalendarEntry]()
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	db := r.programStatsScope(ctx, companyID).
		Preload("Company").
		Where(clause.Neq{Column: col("start_at"), Value: nil}).
		Where(clause.Lte{Column: col("start_at"), Value: end.UTC()}).
		Where(clause.Or(
			clause.And(
				clause.Neq{Column: col("closed_at"), Value: nil},
				clause.Gte{Column: col("closed_at"), Value: start.UTC()},
			),
			clause.And(
				clause.Eq{Column: col("closed_at"), Value: nil},
				clause.Or(
					clause.Eq{Column: col("end_at"), Value: nil},
					clause.Gte{Column: col("end_at"), Value: start.UTC()},
				),
			),
		))
	// the search is added here on the text columns only, useQuery is given
	// the paging and order alone
	query := options.QueryArgs
	search := ""
	if query != nil && query.Search != "" {
		search = query.Search
		copied := *query
		copied.Search = ""
		query = &copied
	}
	db, err := useQuery(db, database.PROGRAM_TABLE, query, allowedProgramColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if search != "" {
		db = db.Where(programSearch(search))
	}
	var rows []database.Program
	if err := withPeek(db, options.QueryArgs).Find(&rows).Error; err != nil {
		return result, errs.Wrap(err)
	}
	hasNextPage := trimPeek(&rows, options.QueryArgs)
	result.HasNextPage = hasNextPage
	for i := range rows {
		// a program that has started shows from its first enrollment
		start := *rows[i].StartAt
		if rows[i].StartedAt != nil {
			start = *rows[i].StartedAt
		}
		entry := &model.ProgramCalendarEntry{
			ID:       *rows[i].ID,
			Name:     rows[i].Name,
			StartAt:  start,
			EndAt:    rows[i].EndAt,
			ClosedAt: rows[i].ClosedAt,
			PausedAt: rows[i].PausedAt,
			// the planned start, the status is read from it
			PlannedStartAt: *rows[i].StartAt,
		}
		if rows[i].Company != nil {
			entry.CompanyName = rows[i].Company.Name
		}
		result.Rows = append(result.Rows, entry)
	}
	return result, nil
}

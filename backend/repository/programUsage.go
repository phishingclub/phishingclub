package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RemoveRecipientGroupFromPrograms removes a recipient group from every
// program it is assigned to
func (r *Program) RemoveRecipientGroupFromPrograms(
	ctx context.Context,
	groupID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{
			Column: clause.Column{Table: database.PROGRAM_RECIPIENT_GROUP_TABLE, Name: "recipient_group_id"},
			Value:  groupID,
		}).
		Delete(&database.ProgramRecipientGroup{})
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}

// GetOpenNamesByFilterPage returns the names of at most limit programs that
// are not closed and use a page as deny or evasion page, sorted by name, and
// how many such programs there are
func (r *Program) GetOpenNamesByFilterPage(
	ctx context.Context,
	pageID *uuid.UUID,
	limit int,
) ([]string, int64, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	q := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: col("closed_at"), Value: nil}).
		Where(clause.Or(
			clause.Eq{Column: col("deny_page_id"), Value: pageID},
			clause.Eq{Column: col("evasion_page_id"), Value: pageID},
		))
	var count int64
	if err := q.Session(&gorm.Session{}).Count(&count).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	names := []string{}
	if count == 0 {
		return names, 0, nil
	}
	if err := q.Session(&gorm.Session{}).
		Order(clause.OrderByColumn{Column: col("name")}).
		Limit(limit).
		Pluck("name", &names).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return names, count, nil
}

// RemovePageFromProgramFilters clears a page that is being deleted from every
// closed program, the same way Campaign.RemovePageFromFilters does for
// campaigns. A program that used it as deny page also loses its evasion page
// and its allow or deny lists, as both need a deny page. A program that used
// it as evasion page loses only the evasion page. A program that is not
// closed keeps the page, the delete is refused while one uses it, see
// GetOpenNamesByFilterPage.
func (r *Program) RemovePageFromProgramFilters(
	ctx context.Context,
	pageID *uuid.UUID,
) error {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	closed := clause.Neq{Column: col("closed_at"), Value: nil}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		usingAsDenyPage := tx.Session(&gorm.Session{NewDB: true}).
			Table(database.PROGRAM_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{col("id")}}).
			Where(clause.Eq{Column: col("deny_page_id"), Value: pageID}).
			Where(closed)
		if err := tx.
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{
				clause.Column{Table: database.PROGRAM_ALLOW_DENY_TABLE, Name: "program_id"},
				usingAsDenyPage,
			}}).
			Delete(&database.ProgramAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Model(&database.Program{}).
			Where(clause.Eq{Column: col("deny_page_id"), Value: pageID}).
			Where(closed).
			Updates(map[string]any{"deny_page_id": nil, "evasion_page_id": nil}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Model(&database.Program{}).
			Where(clause.Eq{Column: col("evasion_page_id"), Value: pageID}).
			Where(closed).
			Updates(map[string]any{"evasion_page_id": nil}).Error; err != nil {
			return errs.Wrap(err)
		}
		return nil
	})
}

// RemoveAllowDenyFromProgramFilters removes an allow or deny list that is
// being deleted from every program, the same way
// Campaign.RemoveAllowDenyFromFilters does for campaigns. A program left with
// no list and no evasion page also loses its deny page.
func (r *Program) RemoveAllowDenyFromProgramFilters(
	ctx context.Context,
	allowDenyID *uuid.UUID,
) error {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	listCol := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_ALLOW_DENY_TABLE, Name: name}
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		newDB := tx.Session(&gorm.Session{NewDB: true})
		holders := newDB.Table(database.PROGRAM_ALLOW_DENY_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{listCol("program_id")}}).
			Where(clause.Eq{Column: listCol("allow_deny_id"), Value: allowDenyID})
		otherHolders := newDB.Table(database.PROGRAM_ALLOW_DENY_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{listCol("program_id")}}).
			Where(clause.Neq{Column: listCol("allow_deny_id"), Value: allowDenyID})
		if err := tx.Model(&database.Program{}).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{col("id"), holders}}).
			Where(clause.Expr{SQL: "? NOT IN (?)", Vars: []any{col("id"), otherHolders}}).
			Where(clause.Eq{Column: col("evasion_page_id"), Value: nil}).
			Updates(map[string]any{"deny_page_id": nil}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.
			Where(clause.Eq{Column: listCol("allow_deny_id"), Value: allowDenyID}).
			Delete(&database.ProgramAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		return nil
	})
}

// ProgramUse is an open program that uses a resource
type ProgramUse struct {
	ID   uuid.UUID
	Name string
}

// getOpenProgramsWhere returns the open programs matching the condition,
// sorted by name. A closed program never counts as using a resource.
func (r *Program) getOpenProgramsWhere(ctx context.Context, condition clause.Expression) ([]ProgramUse, error) {
	var rows []database.Program
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Select("id", "name").
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "closed_at"}, Value: nil}).
		Where(condition).
		Order(clause.OrderByColumn{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "name"}}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]ProgramUse, 0, len(rows))
	for _, row := range rows {
		if row.ID != nil {
			out = append(out, ProgramUse{ID: *row.ID, Name: row.Name})
		}
	}
	return out, nil
}

// programIDIn is programs.id IN (subquery)
func programIDIn(subquery *gorm.DB) clause.Expression {
	return clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Table: database.PROGRAM_TABLE, Name: "id"}, subquery}}
}

// GetStartedUsingTemplate returns the programs that have started and are not
// closed with a step using the campaign template. Their open campaigns keep
// the lure and training settings the template had when they were made.
func (r *Program) GetStartedUsingTemplate(ctx context.Context, templateID *uuid.UUID) ([]ProgramUse, error) {
	steps := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.PROGRAM_STEP_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.PROGRAM_STEP_TABLE, Name: "program_id"}}}).
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: "campaign_template_id"}, Value: templateID})
	return r.getOpenProgramsWhere(ctx, clause.And(
		programIDIn(steps),
		clause.Neq{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "started_at"}, Value: nil},
	))
}

// GetOpenUsingTemplates returns the open programs with a step using one of
// the campaign templates
func (r *Program) GetOpenUsingTemplates(ctx context.Context, templateIDs []*uuid.UUID) ([]ProgramUse, error) {
	if len(templateIDs) == 0 {
		return []ProgramUse{}, nil
	}
	values := make([]any, 0, len(templateIDs))
	for _, id := range templateIDs {
		values = append(values, id)
	}
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: name}
	}
	steps := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.PROGRAM_STEP_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{col("program_id")}}).
		Where(clause.IN{Column: col("campaign_template_id"), Values: values})
	return r.getOpenProgramsWhere(ctx, programIDIn(steps))
}

// GetOpenUsingAllowDeny returns the open programs filtering with the list
func (r *Program) GetOpenUsingAllowDeny(ctx context.Context, allowDenyID *uuid.UUID) ([]ProgramUse, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_ALLOW_DENY_TABLE, Name: name}
	}
	holders := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.PROGRAM_ALLOW_DENY_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{col("program_id")}}).
		Where(clause.Eq{Column: col("allow_deny_id"), Value: allowDenyID})
	return r.getOpenProgramsWhere(ctx, programIDIn(holders))
}

// GetOpenUsingWebhook returns the open programs sending to the webhook
func (r *Program) GetOpenUsingWebhook(ctx context.Context, webhookID *uuid.UUID) ([]ProgramUse, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_WEBHOOK_TABLE, Name: name}
	}
	holders := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.PROGRAM_WEBHOOK_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{col("program_id")}}).
		Where(clause.Eq{Column: col("webhook_id"), Value: webhookID})
	return r.getOpenProgramsWhere(ctx, programIDIn(holders))
}

// GetOpenUsingPage returns the open programs using the page as deny or
// evasion page
func (r *Program) GetOpenUsingPage(ctx context.Context, pageID *uuid.UUID) ([]ProgramUse, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	return r.getOpenProgramsWhere(ctx, clause.Or(
		clause.Eq{Column: col("deny_page_id"), Value: pageID},
		clause.Eq{Column: col("evasion_page_id"), Value: pageID},
	))
}

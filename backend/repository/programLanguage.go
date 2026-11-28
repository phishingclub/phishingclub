package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// stepTemplateColumn is the template of a step row, used by the subqueries
// that look at the steps of a program row
var stepTemplateColumn = clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: "campaign_template_id"}

// stepCanSendLanguage is the condition that the template of the step row can
// send the language in code: code is the template language, or the template
// has a usable language row for it. code is a column of the outer query.
func (r *Program) stepCanSendLanguage(code clause.Column) clause.Expression {
	templateLanguage := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignTemplate{}).
		Select("language").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, stepTemplateColumn,
		}})
	usableRow := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignTemplateLanguage{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{templateLanguageColumn("campaign_template_id"), stepTemplateColumn}}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{templateLanguageColumn("language"), code}}).
		Where(clause.Eq{Column: templateLanguageColumn("is_usable"), Value: true})
	return clause.Or(
		clause.Expr{SQL: "? = (?)", Vars: []any{code, templateLanguage}},
		clause.Expr{SQL: "EXISTS (?)", Vars: []any{usableRow}},
	)
}

// stepLanguageBroken is the condition that the program row picks languages
// by a field and the template of the step row can not send one of the
// languages it can pick: the default or the language of a route
func (r *Program) stepLanguageBroken() clause.Expression {
	routeLanguage := clause.Column{Table: database.PROGRAM_LANGUAGE_ROUTE_TABLE, Name: "language"}
	brokenRoute := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.ProgramLanguageRoute{}).
		Select("program_id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.PROGRAM_LANGUAGE_ROUTE_TABLE, Name: "program_id"}, programsColumn("id"),
		}}).
		Where(clause.Expr{SQL: "NOT (?)", Vars: []any{r.stepCanSendLanguage(routeLanguage)}})
	return clause.And(
		clause.Neq{Column: programsColumn("language_route_by"), Value: ""},
		clause.Or(
			clause.And(
				clause.Neq{Column: programsColumn("language_default"), Value: ""},
				clause.Expr{SQL: "NOT (?)", Vars: []any{r.stepCanSendLanguage(programsColumn("language_default"))}},
			),
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{brokenRoute}},
		),
	)
}

// brokenLanguageStep selects the steps of the program row whose template can
// not send a language the program picks, see stepLanguageBroken
func (r *Program) brokenLanguageStep() *gorm.DB {
	return r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.ProgramStep{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: "program_id"}, programsColumn("id")}}).
		Where(clause.Neq{Column: stepTemplateColumn, Value: nil}).
		Where(r.stepLanguageBroken())
}

// GetProgramsWithBrokenLanguage returns which of the programs pick a language
// by a field that one of their step templates can not send, see
// stepLanguageBroken. A program that is not in the result has none.
func (r *Program) GetProgramsWithBrokenLanguage(
	ctx context.Context,
	programIDs []*uuid.UUID,
) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for start := 0; start < len(programIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(programIDs))
		var ids []uuid.UUID
		err := r.DB.WithContext(ctx).
			Model(&database.Program{}).
			Where(clause.IN{Column: programsColumn("id"), Values: uuidValues(programIDs[start:end])}).
			Where(clause.Neq{Column: programsColumn("language_route_by"), Value: ""}).
			Where(clause.Expr{SQL: "EXISTS (?)", Vars: []any{r.brokenLanguageStep()}}).
			Pluck("id", &ids).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, id := range ids {
			out[id] = true
		}
	}
	return out, nil
}

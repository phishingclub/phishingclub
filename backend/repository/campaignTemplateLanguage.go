package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrTemplateLanguageUnavailable is returned by GetByIDForLanguage when the
// template has no usable row for the asked language. The default content is
// never given in its place.
var ErrTemplateLanguageUnavailable = errors.New("the template has no usable content in this language")

func templateLanguageColumn(name string) clause.Column {
	return clause.Column{Table: database.CAMPAIGN_TEMPLATE_LANGUAGE_TABLE, Name: name}
}

// templatesUsingContent matches the templates that hold id in one of the
// columns, on the template row or on one of its language rows. The columns
// are fixed names given by the caller, never request data.
func (r *CampaignTemplate) templatesUsingContent(id *uuid.UUID, columns []string) clause.Expression {
	onTemplate := make([]clause.Expression, 0, len(columns))
	onLanguage := make([]clause.Expression, 0, len(columns))
	for _, column := range columns {
		onTemplate = append(onTemplate, clause.Eq{
			Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: column},
			Value:  id.String(),
		})
		onLanguage = append(onLanguage, clause.Eq{Column: templateLanguageColumn(column), Value: id.String()})
	}
	viaLanguage := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignTemplateLanguage{}).
		Select("campaign_template_id").
		Where(clause.Or(onLanguage...))
	return clause.Or(
		clause.Or(onTemplate...),
		clause.Expr{
			SQL:  "? IN (?)",
			Vars: []any{clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, viaLanguage},
		},
	)
}

// TemplateLanguageUse is a language row of a template that uses some content
type TemplateLanguageUse struct {
	TemplateID uuid.UUID
	Language   string
}

// getLanguageUsesByContentID returns the template id and code of every
// language row that holds id in one of the columns, ordered by template and
// code. The columns are fixed names given by the caller.
func (r *CampaignTemplate) getLanguageUsesByContentID(
	ctx context.Context,
	id *uuid.UUID,
	columns []string,
) ([]TemplateLanguageUse, error) {
	out := []TemplateLanguageUse{}
	if id == nil {
		return out, nil
	}
	conds := make([]clause.Expression, 0, len(columns))
	for _, column := range columns {
		conds = append(conds, clause.Eq{Column: templateLanguageColumn(column), Value: id.String()})
	}
	var rows []database.CampaignTemplateLanguage
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignTemplateLanguage{}).
		Select("campaign_template_id", "language").
		Where(clause.Or(conds...)).
		Order(clause.OrderByColumn{Column: templateLanguageColumn("campaign_template_id")}).
		Order(clause.OrderByColumn{Column: templateLanguageColumn("language")}).
		Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for _, row := range rows {
		if row.CampaignTemplateID == nil {
			continue
		}
		out = append(out, TemplateLanguageUse{TemplateID: *row.CampaignTemplateID, Language: row.Language})
	}
	return out, nil
}

// GetLanguageUsesByEmailID returns the language rows that use the email
func (r *CampaignTemplate) GetLanguageUsesByEmailID(
	ctx context.Context,
	emailID *uuid.UUID,
) ([]TemplateLanguageUse, error) {
	return r.getLanguageUsesByContentID(ctx, emailID, []string{"email_id"})
}

// GetLanguageUsesByPageID returns the language rows that use the page in a
// stage their template has as a page, ordered by template and code. A row
// page for a stage the template no longer has as a page is not served, so
// that row is not a use.
func (r *CampaignTemplate) GetLanguageUsesByPageID(
	ctx context.Context,
	pageID *uuid.UUID,
) ([]TemplateLanguageUse, error) {
	out := []TemplateLanguageUse{}
	if pageID == nil {
		return out, nil
	}
	db := r.DB.WithContext(ctx)
	var rows []database.CampaignTemplateLanguage
	if err := db.
		Model(&database.CampaignTemplateLanguage{}).
		Where(clause.Or(
			clause.Eq{Column: templateLanguageColumn("before_landing_page_id"), Value: pageID.String()},
			clause.Eq{Column: templateLanguageColumn("landing_page_id"), Value: pageID.String()},
			clause.Eq{Column: templateLanguageColumn("after_landing_page_id"), Value: pageID.String()},
		)).
		Order(clause.OrderByColumn{Column: templateLanguageColumn("campaign_template_id")}).
		Order(clause.OrderByColumn{Column: templateLanguageColumn("language")}).
		Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	templates := map[uuid.UUID]*database.CampaignTemplate{}
	for _, row := range rows {
		if row.CampaignTemplateID == nil {
			continue
		}
		tmpl, ok := templates[*row.CampaignTemplateID]
		if !ok {
			var found database.CampaignTemplate
			if err := db.Session(&gorm.Session{NewDB: true}).
				Model(&database.CampaignTemplate{}).
				Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, Value: row.CampaignTemplateID.String()}).
				First(&found).Error; err != nil {
				return nil, errs.Wrap(err)
			}
			tmpl = &found
			templates[*row.CampaignTemplateID] = tmpl
		}
		holds := func(rowPage, templatePage *uuid.UUID) bool {
			return rowPage != nil && *rowPage == *pageID && templatePage != nil
		}
		if holds(row.BeforeLandingPageID, tmpl.BeforeLandingPageID) ||
			holds(row.LandingPageID, tmpl.LandingPageID) ||
			holds(row.AfterLandingPageID, tmpl.AfterLandingPageID) {
			out = append(out, TemplateLanguageUse{TemplateID: *row.CampaignTemplateID, Language: row.Language})
		}
	}
	return out, nil
}

// clearLanguageContentTx sets the columns of every language row that hold id
// to null. A row stays usable only when it still has an email and a page for
// every stage its template has as a page, so clearing a page of a stage the
// template no longer has keeps the row usable. The columns are fixed names
// given by the caller. Runs inside the caller's transaction, after the
// template's own columns were cleared.
func clearLanguageContentTx(tx *gorm.DB, id *uuid.UUID, columns []string) error {
	conds := make([]clause.Expression, 0, len(columns))
	for _, column := range columns {
		conds = append(conds, clause.Eq{Column: templateLanguageColumn(column), Value: id.String()})
	}
	var rows []database.CampaignTemplateLanguage
	if err := tx.
		Model(&database.CampaignTemplateLanguage{}).
		Where(clause.Or(conds...)).
		Find(&rows).Error; err != nil {
		return errs.Wrap(err)
	}
	stagesOf := map[uuid.UUID]model.TemplateStages{}
	for _, row := range rows {
		if row.CampaignTemplateID == nil {
			continue
		}
		update := map[string]any{}
		for _, column := range columns {
			switch column {
			case "email_id":
				if row.EmailID != nil && *row.EmailID == *id {
					update[column], row.EmailID = nil, nil
				}
			case "before_landing_page_id":
				if row.BeforeLandingPageID != nil && *row.BeforeLandingPageID == *id {
					update[column], row.BeforeLandingPageID = nil, nil
				}
			case "landing_page_id":
				if row.LandingPageID != nil && *row.LandingPageID == *id {
					update[column], row.LandingPageID = nil, nil
				}
			case "after_landing_page_id":
				if row.AfterLandingPageID != nil && *row.AfterLandingPageID == *id {
					update[column], row.AfterLandingPageID = nil, nil
				}
			}
		}
		if len(update) == 0 {
			continue
		}
		stages, ok := stagesOf[*row.CampaignTemplateID]
		if !ok {
			var tmpl database.CampaignTemplate
			if err := tx.
				Model(&database.CampaignTemplate{}).
				Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, Value: row.CampaignTemplateID.String()}).
				First(&tmpl).Error; err != nil {
				return errs.Wrap(err)
			}
			stages = model.TemplateStages{
				BeforeLandingPage: tmpl.BeforeLandingPageID != nil,
				LandingPage:       tmpl.LandingPageID != nil,
				AfterLandingPage:  tmpl.AfterLandingPageID != nil,
			}
			stagesOf[*row.CampaignTemplateID] = stages
		}
		update["is_usable"] = row.IsUsable && stages.Usable(ToCampaignTemplateLanguage(&row))
		AddUpdatedAt(update)
		if err := tx.
			Model(&database.CampaignTemplateLanguage{}).
			Where(clause.Eq{Column: templateLanguageColumn("id"), Value: row.ID.String()}).
			Updates(update).Error; err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// GetLanguagesByTemplateID returns the language rows of a template, ordered
// by code. With withNames the names of the email and pages are filled in.
func (r *CampaignTemplate) GetLanguagesByTemplateID(
	ctx context.Context,
	templateID *uuid.UUID,
	withNames bool,
) ([]*model.CampaignTemplateLanguage, error) {
	return getTemplateLanguagesTx(r.DB.WithContext(ctx), templateID, withNames)
}

func getTemplateLanguagesTx(
	db *gorm.DB,
	templateID *uuid.UUID,
	withNames bool,
) ([]*model.CampaignTemplateLanguage, error) {
	out := []*model.CampaignTemplateLanguage{}
	if templateID == nil {
		return out, nil
	}
	var rows []database.CampaignTemplateLanguage
	if err := db.
		Model(&database.CampaignTemplateLanguage{}).
		Where(clause.Eq{Column: templateLanguageColumn("campaign_template_id"), Value: templateID.String()}).
		Order(clause.OrderByColumn{Column: templateLanguageColumn("language")}).
		Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for i := range rows {
		out = append(out, ToCampaignTemplateLanguage(&rows[i]))
	}
	if withNames && len(rows) > 0 {
		if err := fillTemplateLanguageNames(db, rows, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// fillTemplateLanguageNames reads the names of the emails and pages of rows
// in one query per table
func fillTemplateLanguageNames(
	db *gorm.DB,
	rows []database.CampaignTemplateLanguage,
	out []*model.CampaignTemplateLanguage,
) error {
	emailIDs := []any{}
	pageIDs := []any{}
	for _, row := range rows {
		if row.EmailID != nil {
			emailIDs = append(emailIDs, row.EmailID.String())
		}
		for _, id := range []*uuid.UUID{row.BeforeLandingPageID, row.LandingPageID, row.AfterLandingPageID} {
			if id != nil {
				pageIDs = append(pageIDs, id.String())
			}
		}
	}
	names := func(model any, table string, ids []any) (map[string]string, error) {
		m := map[string]string{}
		if len(ids) == 0 {
			return m, nil
		}
		type idName struct {
			ID   string
			Name string
		}
		var found []idName
		if err := db.Session(&gorm.Session{NewDB: true}).
			Model(model).
			Select("id", "name").
			Where(clause.IN{Column: clause.Column{Table: table, Name: "id"}, Values: ids}).
			Scan(&found).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for _, f := range found {
			m[f.ID] = f.Name
		}
		return m, nil
	}
	emailNames, err := names(&database.Email{}, database.EMAIL_TABLE, emailIDs)
	if err != nil {
		return err
	}
	pageNames, err := names(&database.Page{}, database.PAGE_TABLE, pageIDs)
	if err != nil {
		return err
	}
	nameOf := func(m map[string]string, id *uuid.UUID) string {
		if id == nil {
			return ""
		}
		return m[id.String()]
	}
	for i, row := range rows {
		out[i].EmailName = nameOf(emailNames, row.EmailID)
		out[i].BeforeLandingPageName = nameOf(pageNames, row.BeforeLandingPageID)
		out[i].LandingPageName = nameOf(pageNames, row.LandingPageID)
		out[i].AfterLandingPageName = nameOf(pageNames, row.AfterLandingPageID)
	}
	return nil
}

// replaceTemplateLanguagesTx removes every language row of a template and
// writes languages in their place. stages decides which page columns a row
// keeps and whether it is usable. Runs inside the caller's transaction.
func replaceTemplateLanguagesTx(
	tx *gorm.DB,
	templateID *uuid.UUID,
	stages model.TemplateStages,
	languages []*model.CampaignTemplateLanguage,
) error {
	if err := deleteTemplateLanguagesTx(tx, templateID); err != nil {
		return err
	}
	if len(languages) == 0 {
		return nil
	}
	rows := make([]database.CampaignTemplateLanguage, 0, len(languages))
	for _, l := range languages {
		if l == nil {
			continue
		}
		id := uuid.New()
		row := database.CampaignTemplateLanguage{
			ID:                 &id,
			CampaignTemplateID: templateID,
			Language:           l.Language,
			EmailID:            nullableUUIDPtr(l.EmailID),
		}
		if stages.BeforeLandingPage {
			row.BeforeLandingPageID = nullableUUIDPtr(l.BeforeLandingPageID)
		}
		if stages.LandingPage {
			row.LandingPageID = nullableUUIDPtr(l.LandingPageID)
		}
		if stages.AfterLandingPage {
			row.AfterLandingPageID = nullableUUIDPtr(l.AfterLandingPageID)
		}
		row.IsUsable = stages.Usable(ToCampaignTemplateLanguage(&row))
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i := range rows {
		rows[i].CreatedAt = &now
		rows[i].UpdatedAt = &now
	}
	return errs.Wrap(tx.Create(&rows).Error)
}

// syncTemplateLanguageStagesTx keeps the language rows of a template in the
// shape of its page flow: the stages in cleared lose their page on every row,
// and a row that misses a page stage or its email is marked as not usable.
// Reads go through tx.
func syncTemplateLanguageStagesTx(
	tx *gorm.DB,
	templateID *uuid.UUID,
	stages model.TemplateStages,
	cleared model.TemplateStages,
) error {
	var rows []database.CampaignTemplateLanguage
	if err := tx.
		Model(&database.CampaignTemplateLanguage{}).
		Where(clause.Eq{Column: templateLanguageColumn("campaign_template_id"), Value: templateID.String()}).
		Find(&rows).Error; err != nil {
		return errs.Wrap(err)
	}
	for _, row := range rows {
		update := map[string]any{}
		if cleared.BeforeLandingPage && row.BeforeLandingPageID != nil {
			update["before_landing_page_id"] = nil
			row.BeforeLandingPageID = nil
		}
		if cleared.LandingPage && row.LandingPageID != nil {
			update["landing_page_id"] = nil
			row.LandingPageID = nil
		}
		if cleared.AfterLandingPage && row.AfterLandingPageID != nil {
			update["after_landing_page_id"] = nil
			row.AfterLandingPageID = nil
		}
		usable := stages.Usable(ToCampaignTemplateLanguage(&row))
		if usable != row.IsUsable {
			update["is_usable"] = usable
		}
		if len(update) == 0 {
			continue
		}
		AddUpdatedAt(update)
		if err := tx.
			Model(&database.CampaignTemplateLanguage{}).
			Where(clause.Eq{Column: templateLanguageColumn("id"), Value: row.ID.String()}).
			Updates(update).Error; err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// deleteTemplateLanguagesTx removes every language row of a template
func deleteTemplateLanguagesTx(tx *gorm.DB, templateID *uuid.UUID) error {
	return errs.Wrap(tx.
		Where(clause.Eq{Column: templateLanguageColumn("campaign_template_id"), Value: templateID.String()}).
		Delete(&database.CampaignTemplateLanguage{}).Error)
}

// GetByIDForLanguage loads a template like GetByID and, when code is set and
// is not the template language, puts the email and pages of that language in
// place of the template's own: the ids and, when options ask for them, the
// loaded email and pages. Proxy stages and every other setting stay the
// template's. The language row is read by the loaded template id, so another
// template's content can never be applied. A missing or not usable row
// returns ErrTemplateLanguageUnavailable.
func (r *CampaignTemplate) GetByIDForLanguage(
	ctx context.Context,
	id *uuid.UUID,
	code string,
	options *CampaignTemplateOption,
) (*model.CampaignTemplate, error) {
	tmpl, err := r.GetByID(ctx, id, options)
	if err != nil {
		return nil, err
	}
	if code == "" || code == tmpl.TemplateLanguage() {
		return tmpl, nil
	}
	var row database.CampaignTemplateLanguage
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignTemplateLanguage{}).
		Where(clause.Eq{Column: templateLanguageColumn("campaign_template_id"), Value: id.String()}).
		Where(clause.Eq{Column: templateLanguageColumn("language"), Value: code}).
		Limit(1).
		Find(&row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if res.RowsAffected == 0 || !row.IsUsable || row.EmailID == nil {
		return nil, errs.Wrap(fmt.Errorf("%w: %s", ErrTemplateLanguageUnavailable, code))
	}
	stages := model.StagesOf(tmpl)
	pages := []struct {
		page   bool
		rowID  *uuid.UUID
		id     *nullable.Nullable[uuid.UUID]
		loaded **model.Page
		load   bool
	}{
		{stages.BeforeLandingPage, row.BeforeLandingPageID, &tmpl.BeforeLandingPageID, &tmpl.BeforeLandingePage, options != nil && options.WithBeforeLandingPage},
		{stages.LandingPage, row.LandingPageID, &tmpl.LandingPageID, &tmpl.LandingPage, options != nil && options.WithLandingPage},
		{stages.AfterLandingPage, row.AfterLandingPageID, &tmpl.AfterLandingPageID, &tmpl.AfterLandingPage, options != nil && options.WithAfterLandingPage},
	}
	for _, p := range pages {
		if !p.page {
			continue
		}
		if p.rowID == nil {
			return nil, errs.Wrap(fmt.Errorf("%w: %s", ErrTemplateLanguageUnavailable, code))
		}
		p.id.Set(*p.rowID)
		*p.loaded = nil
		if p.load {
			page, err := r.getPageByID(ctx, p.rowID)
			if err != nil {
				return nil, err
			}
			*p.loaded = page
		}
	}
	tmpl.EmailID.Set(*row.EmailID)
	tmpl.Email = nil
	if options != nil && options.WithEmail {
		var email database.Email
		if err := r.DB.WithContext(ctx).
			Where(clause.Eq{Column: clause.Column{Table: database.EMAIL_TABLE, Name: "id"}, Value: row.EmailID.String()}).
			First(&email).Error; err != nil {
			return nil, err
		}
		tmpl.Email = ToEmail(&email)
	}
	return tmpl, nil
}

func (r *CampaignTemplate) getPageByID(ctx context.Context, id *uuid.UUID) (*model.Page, error) {
	var page database.Page
	if err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Table: database.PAGE_TABLE, Name: "id"}, Value: id.String()}).
		First(&page).Error; err != nil {
		return nil, err
	}
	return ToPage(&page)
}

// ToCampaignTemplateLanguage converts a language row
func ToCampaignTemplateLanguage(row *database.CampaignTemplateLanguage) *model.CampaignTemplateLanguage {
	l := &model.CampaignTemplateLanguage{
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		Language:            row.Language,
		EmailID:             uuidPtrNullable(row.EmailID),
		BeforeLandingPageID: uuidPtrNullable(row.BeforeLandingPageID),
		LandingPageID:       uuidPtrNullable(row.LandingPageID),
		AfterLandingPageID:  uuidPtrNullable(row.AfterLandingPageID),
		IsUsable:            row.IsUsable,
	}
	if row.ID != nil {
		l.ID = nullable.NewNullableWithValue(*row.ID)
	}
	if row.CampaignTemplateID != nil {
		l.CampaignTemplateID = nullable.NewNullableWithValue(*row.CampaignTemplateID)
	}
	return l
}

// LanguageCodes returns the codes of languages, sorted
func LanguageCodes(languages []*model.CampaignTemplateLanguage) []string {
	out := make([]string, 0, len(languages))
	for _, l := range languages {
		if l != nil {
			out = append(out, l.Language)
		}
	}
	sort.Strings(out)
	return out
}

func uuidPtrNullable(id *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if id == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*id)
}

func nullableUUIDPtr(id nullable.Nullable[uuid.UUID]) *uuid.UUID {
	if v, err := id.Get(); err == nil {
		return &v
	}
	return nil
}

// TemplateLanguageSet is the name, the template language and the language
// rows of one template
type TemplateLanguageSet struct {
	TemplateID uuid.UUID
	Name       string
	// Language is the template language, empty when not set
	Language string
	// Rows are the language rows ordered by code
	Rows []*model.CampaignTemplateLanguage
}

// Row returns the language row for code, nil when the template has none
func (s *TemplateLanguageSet) Row(code string) *model.CampaignTemplateLanguage {
	for _, row := range s.Rows {
		if row.Language == code {
			return row
		}
	}
	return nil
}

// CanSend reports whether the template has usable content in code: the code
// is the template language or a usable language row
func (s *TemplateLanguageSet) CanSend(code string) bool {
	if code == "" || code == s.Language {
		return true
	}
	row := s.Row(code)
	return row != nil && row.IsUsable
}

// GetLanguageSetsByIDs returns the language set of each template that exists,
// keyed by template id, in one query for the templates and one for the rows.
// It reads only names and language codes, never content.
func (r *CampaignTemplate) GetLanguageSetsByIDs(
	ctx context.Context,
	ids []uuid.UUID,
) (map[uuid.UUID]*TemplateLanguageSet, error) {
	out := map[uuid.UUID]*TemplateLanguageSet{}
	if len(ids) == 0 {
		return out, nil
	}
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	type templateRow struct {
		ID       uuid.UUID
		Name     string
		Language string
	}
	var templates []templateRow
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignTemplate{}).
		Select("id", "name", "language").
		Where(clause.IN{Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, Values: values}).
		Scan(&templates).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for _, t := range templates {
		out[t.ID] = &TemplateLanguageSet{
			TemplateID: t.ID,
			Name:       t.Name,
			Language:   t.Language,
			Rows:       []*model.CampaignTemplateLanguage{},
		}
	}
	rows, err := r.GetLanguageRowsByTemplateIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, set := range out {
		if list, ok := rows[id]; ok {
			set.Rows = list
		}
	}
	return out, nil
}

// GetLanguageRowsByTemplateIDs returns the language rows of the templates,
// keyed by template id and ordered by code, in one query
func (r *CampaignTemplate) GetLanguageRowsByTemplateIDs(
	ctx context.Context,
	ids []uuid.UUID,
) (map[uuid.UUID][]*model.CampaignTemplateLanguage, error) {
	out := map[uuid.UUID][]*model.CampaignTemplateLanguage{}
	if len(ids) == 0 {
		return out, nil
	}
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	var rows []database.CampaignTemplateLanguage
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignTemplateLanguage{}).
		Where(clause.IN{Column: templateLanguageColumn("campaign_template_id"), Values: values}).
		Order(clause.OrderByColumn{Column: templateLanguageColumn("language")}).
		Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for i := range rows {
		if rows[i].CampaignTemplateID == nil {
			continue
		}
		id := *rows[i].CampaignTemplateID
		out[id] = append(out[id], ToCampaignTemplateLanguage(&rows[i]))
	}
	return out, nil
}

// GetMailFromByEmailIDs returns the envelope sender (MAIL FROM) of each email
// that exists, keyed by email id, in one query
func (r *CampaignTemplate) GetMailFromByEmailIDs(
	ctx context.Context,
	ids []uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	type emailRow struct {
		ID       uuid.UUID
		MailFrom string
	}
	var rows []emailRow
	if err := r.DB.WithContext(ctx).
		Model(&database.Email{}).
		Select("id", "mail_from").
		Where(clause.IN{Column: clause.Column{Table: database.EMAIL_TABLE, Name: "id"}, Values: values}).
		Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for _, row := range rows {
		out[row.ID] = row.MailFrom
	}
	return out, nil
}

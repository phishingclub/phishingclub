package repository

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// languageRouteBatch is how many route rows one insert writes, three bind
// values each
const languageRouteBatch = 100

func sortedRouteKeys(routes map[string]string) []string {
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// replaceCampaignLanguageRoutesTx replaces the language routes of a campaign.
// The keys are already normalized and checked by the model.
func replaceCampaignLanguageRoutesTx(tx *gorm.DB, campaignID *uuid.UUID, routes map[string]string) error {
	if err := deleteCampaignLanguageRoutesTx(tx, campaignID); err != nil {
		return err
	}
	if len(routes) == 0 {
		return nil
	}
	rows := make([]database.CampaignLanguageRoute, 0, len(routes))
	for _, key := range sortedRouteKeys(routes) {
		rows = append(rows, database.CampaignLanguageRoute{
			CampaignID: campaignID,
			MatchKey:   key,
			Language:   routes[key],
		})
	}
	return errs.Wrap(tx.CreateInBatches(&rows, languageRouteBatch).Error)
}

func deleteCampaignLanguageRoutesTx(tx *gorm.DB, campaignID *uuid.UUID) error {
	return errs.Wrap(tx.
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_LANGUAGE_ROUTE_TABLE, Name: "campaign_id"}, Value: campaignID.String()}).
		Delete(&database.CampaignLanguageRoute{}).Error)
}

// GetLanguageRoutesByCampaignID returns the language routes of a campaign,
// normalized value to language code
func (r *Campaign) GetLanguageRoutesByCampaignID(ctx context.Context, campaignID *uuid.UUID) (map[string]string, error) {
	var rows []database.CampaignLanguageRoute
	if err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_LANGUAGE_ROUTE_TABLE, Name: "campaign_id"}, Value: campaignID.String()}).
		Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.MatchKey] = row.Language
	}
	return out, nil
}

// replaceProgramLanguageRoutesTx replaces the language routes of a program,
// see replaceCampaignLanguageRoutesTx
func replaceProgramLanguageRoutesTx(tx *gorm.DB, programID *uuid.UUID, routes map[string]string) error {
	if err := tx.
		Where(programIDEq(clause.Column{Table: database.PROGRAM_LANGUAGE_ROUTE_TABLE, Name: "program_id"}, programID)).
		Delete(&database.ProgramLanguageRoute{}).Error; err != nil {
		return errs.Wrap(err)
	}
	if len(routes) == 0 {
		return nil
	}
	rows := make([]database.ProgramLanguageRoute, 0, len(routes))
	for _, key := range sortedRouteKeys(routes) {
		rows = append(rows, database.ProgramLanguageRoute{
			ProgramID: programID,
			MatchKey:  key,
			Language:  routes[key],
		})
	}
	return errs.Wrap(tx.CreateInBatches(&rows, languageRouteBatch).Error)
}

// GetLanguageRoutesByProgramID returns the language routes of a program,
// normalized value to language code
func (r *Program) GetLanguageRoutesByProgramID(ctx context.Context, programID *uuid.UUID) (map[string]string, error) {
	var rows []database.ProgramLanguageRoute
	if err := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Table: database.PROGRAM_LANGUAGE_ROUTE_TABLE, Name: "program_id"}, programID)).
		Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.MatchKey] = row.Language
	}
	return out, nil
}

// GetOpenCampaignNamesUsingLanguage returns the names of open campaigns that
// use one of codes of the template, at most limit of them, and the number of
// other open campaigns that use them. A campaign uses a code when a recipient
// row on the template is stamped with it, sent or not (the pages of sent rows
// are still served; a row cancelled before it was sent does not count), or
// when the campaign has the template in its pool and its language default or
// a route picks the code. With withNoLanguage a row on the template with no
// language counts too, for an update that stamps those rows with one of
// codes. A hidden campaign of a program is named and counted by its program.
// Only campaigns of companyID, or global campaigns when companyID is nil, are
// named; the others are only counted.
func (r *Campaign) GetOpenCampaignNamesUsingLanguage(
	ctx context.Context,
	templateID *uuid.UUID,
	codes []string,
	withNoLanguage bool,
	companyID *uuid.UUID,
	limit int,
) ([]string, int64, error) {
	names := []string{}
	if templateID == nil || len(codes) == 0 {
		return names, 0, nil
	}
	id := templateID.String()
	global := companyID == nil
	scope := uuid.Nil.String()
	if companyID != nil {
		scope = companyID.String()
	}
	err := r.DB.WithContext(ctx).Raw(`SELECT DISTINCT COALESCE(p.name, c.name)
		FROM campaigns c
		LEFT JOIN programs p ON p.id = c.program_id
		WHERE c.closed_at IS NULL
		  AND ((? AND c.company_id IS NULL) OR c.company_id = ?)
		  AND (EXISTS (SELECT 1 FROM campaign_recipients cr
		      WHERE cr.campaign_id = c.id
		        AND (cr.language IN ? OR (? AND cr.language IS NULL))
		        AND COALESCE(cr.campaign_template_id, c.campaign_template_id) = ?
		        AND (cr.sent_at IS NOT NULL OR cr.cancelled_at IS NULL))
		    OR (c.language_route_by <> ''
		      AND (c.campaign_template_id = ?
		        OR EXISTS (SELECT 1 FROM campaign_template_pool tp
		          WHERE tp.campaign_id = c.id AND tp.campaign_template_id = ?))
		      AND (c.language_default IN ?
		        OR EXISTS (SELECT 1 FROM campaign_language_routes lr
		          WHERE lr.campaign_id = c.id AND lr.language IN ?))))
		LIMIT ?`, global, scope, codes, withNoLanguage, id, id, id, codes, codes, limit).
		Scan(&names).Error
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	var others int64
	err = r.DB.WithContext(ctx).Raw(`SELECT COUNT(DISTINCT COALESCE(p.id, c.id))
		FROM campaigns c
		LEFT JOIN programs p ON p.id = c.program_id
		WHERE c.closed_at IS NULL
		  AND ((c.company_id IS NULL AND NOT ?) OR (c.company_id IS NOT NULL AND c.company_id <> ?))
		  AND (EXISTS (SELECT 1 FROM campaign_recipients cr
		      WHERE cr.campaign_id = c.id
		        AND (cr.language IN ? OR (? AND cr.language IS NULL))
		        AND COALESCE(cr.campaign_template_id, c.campaign_template_id) = ?
		        AND (cr.sent_at IS NOT NULL OR cr.cancelled_at IS NULL))
		    OR (c.language_route_by <> ''
		      AND (c.campaign_template_id = ?
		        OR EXISTS (SELECT 1 FROM campaign_template_pool tp
		          WHERE tp.campaign_id = c.id AND tp.campaign_template_id = ?))
		      AND (c.language_default IN ?
		        OR EXISTS (SELECT 1 FROM campaign_language_routes lr
		          WHERE lr.campaign_id = c.id AND lr.language IN ?))))`,
		global, scope, codes, withNoLanguage, id, id, id, codes, codes).
		Scan(&others).Error
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return names, others, nil
}

// GetOpenUsingTemplateLanguages returns the programs that are not closed,
// drafts included, with a step using the template and language routing that
// picks one of codes, as default or by a route
func (r *Program) GetOpenUsingTemplateLanguages(
	ctx context.Context,
	templateID *uuid.UUID,
	codes []string,
) ([]ProgramUse, error) {
	if templateID == nil || len(codes) == 0 {
		return []ProgramUse{}, nil
	}
	values := make([]any, 0, len(codes))
	for _, code := range codes {
		values = append(values, code)
	}
	steps := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.PROGRAM_STEP_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.PROGRAM_STEP_TABLE, Name: "program_id"}}}).
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: "campaign_template_id"}, Value: templateID})
	routes := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.PROGRAM_LANGUAGE_ROUTE_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.PROGRAM_LANGUAGE_ROUTE_TABLE, Name: "program_id"}}}).
		Where(clause.IN{Column: clause.Column{Table: database.PROGRAM_LANGUAGE_ROUTE_TABLE, Name: "language"}, Values: values})
	return r.getOpenProgramsWhere(ctx, clause.And(
		programIDIn(steps),
		clause.Neq{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "language_route_by"}, Value: ""},
		clause.Or(
			clause.IN{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "language_default"}, Values: values},
			programIDIn(routes),
		),
	))
}

// usesLanguageBatch is how many campaign ids one lookup binds
const usesLanguageBatch = 500

// FilterCampaignsUsingTemplateLanguage returns the ids among campaignIDs of
// the campaigns that can reach a language of a template: the language is the
// campaign's language default, a route maps to it, or a recipient row on the
// template is stamped with it. Sent rows count too, since their pages are
// still served.
func (r *Campaign) FilterCampaignsUsingTemplateLanguage(
	ctx context.Context,
	campaignIDs []uuid.UUID,
	templateID uuid.UUID,
	code string,
) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	if len(campaignIDs) == 0 || code == "" {
		return out, nil
	}
	for start := 0; start < len(campaignIDs); start += usesLanguageBatch {
		end := min(start+usesLanguageBatch, len(campaignIDs))
		ids := make([]string, 0, end-start)
		for _, id := range campaignIDs[start:end] {
			ids = append(ids, id.String())
		}
		found := []uuid.UUID{}
		err := r.DB.WithContext(ctx).Raw(`SELECT c.id FROM campaigns c
			WHERE c.id IN ?
			  AND (c.language_default = ?
			    OR EXISTS (SELECT 1 FROM campaign_language_routes lr
			      WHERE lr.campaign_id = c.id AND lr.language = ?)
			    OR EXISTS (SELECT 1 FROM campaign_recipients cr
			      WHERE cr.campaign_id = c.id AND cr.language = ?
			        AND COALESCE(cr.campaign_template_id, c.campaign_template_id) = ?))`,
			ids, code, code, code, templateID.String()).
			Scan(&found).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, found...)
	}
	return out, nil
}

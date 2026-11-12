package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// SetTemplatePool replaces a campaign's template pool and points
// campaign_template_id at the first entry, in one transaction
func (r *Campaign) SetTemplatePool(
	ctx context.Context,
	campaignID *uuid.UUID,
	templateIDs []uuid.UUID,
) error {
	return r.ReplaceTemplatePool(ctx, campaignID, templateIDs, nil, nil, nil)
}

// ReplaceTemplatePool replaces a campaign's template pool, points
// campaign_template_id at the first entry and writes recipient templates, all in
// one transaction. Every read inside it goes through the transaction, and a
// template deleted meanwhile fails the write. When fillEmpty is set, every
// recipient of the campaign without a template is first given it in one
// statement. stamps then give the listed recipients a template whatever their
// state. moves give a recipient another template and only apply while that
// recipient is still not sent to and has no event, so a send that lands between
// planning and this write keeps its template.
func (r *Campaign) ReplaceTemplatePool(
	ctx context.Context,
	campaignID *uuid.UUID,
	templateIDs []uuid.UUID,
	fillEmpty *uuid.UUID,
	stamps map[uuid.UUID][]uuid.UUID,
	moves map[uuid.UUID][]uuid.UUID,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if fillEmpty != nil {
			row := map[string]any{"campaign_template_id": fillEmpty.String()}
			AddUpdatedAt(row)
			if err := tx.
				Model(&database.CampaignRecipient{}).
				Where("campaign_id = ?", campaignID.String()).
				Where("campaign_template_id IS NULL").
				Updates(row).Error; err != nil {
				return err
			}
		}
		if err := setTemplateForIDsTx(tx, campaignID, stamps, false); err != nil {
			return err
		}
		if err := setTemplateForIDsTx(tx, campaignID, moves, true); err != nil {
			return err
		}
		return setTemplatePoolTx(tx, campaignID, templateIDs)
	})
}

// setTemplatePoolTx writes the pool rows and the campaign's first template
func setTemplatePoolTx(
	tx *gorm.DB,
	campaignID *uuid.UUID,
	templateIDs []uuid.UUID,
) error {
	if err := tx.
		Where("campaign_id = ?", campaignID.String()).
		Delete(&database.CampaignTemplatePool{}).Error; err != nil {
		return err
	}
	if len(templateIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(templateIDs))
	for _, id := range templateIDs {
		ids = append(ids, id.String())
	}
	var found int64
	if err := tx.
		Model(&database.CampaignTemplate{}).
		Where("id IN ?", ids).
		Count(&found).Error; err != nil {
		return err
	}
	if found != int64(len(templateIDs)) {
		return validate.WrapErrorWithField(errors.New("a selected template was deleted"), "templateIDs")
	}
	rows := make([]database.CampaignTemplatePool, 0, len(templateIDs))
	for i := range templateIDs {
		rows = append(rows, database.CampaignTemplatePool{
			CampaignID:         campaignID,
			CampaignTemplateID: &templateIDs[i],
			Position:           i,
		})
	}
	if err := tx.Create(&rows).Error; err != nil {
		return err
	}
	update := map[string]any{"campaign_template_id": templateIDs[0].String()}
	AddUpdatedAt(update)
	return tx.
		Model(&database.Campaign{}).
		Where("id = ?", campaignID.String()).
		Updates(update).Error
}

// GetTemplatePoolIDs returns a campaign's pool in order
func (r *Campaign) GetTemplatePoolIDs(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]uuid.UUID, error) {
	var rows []database.CampaignTemplatePool
	res := r.DB.WithContext(ctx).
		Where("campaign_id = ?", campaignID.String()).
		Order("position ASC").
		Find(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.CampaignTemplateID != nil {
			out = append(out, *row.CampaignTemplateID)
		}
	}
	return out, nil
}

// GetTemplatePools returns the pool of each campaign in order, with the template
// names, in one query
func (r *Campaign) GetTemplatePools(
	ctx context.Context,
	campaignIDs []*uuid.UUID,
) (map[uuid.UUID][]*model.CampaignTemplateRef, error) {
	out := map[uuid.UUID][]*model.CampaignTemplateRef{}
	if len(campaignIDs) == 0 {
		return out, nil
	}
	type poolRow struct {
		CampaignID         uuid.UUID
		CampaignTemplateID uuid.UUID
		Name               *string
	}
	var rows []poolRow
	res := r.DB.WithContext(ctx).
		Table("campaign_template_pool AS p").
		Select("p.campaign_id AS campaign_id, p.campaign_template_id AS campaign_template_id, t.name AS name").
		Joins("LEFT JOIN campaign_templates t ON t.id = p.campaign_template_id").
		Where("p.campaign_id IN ?", UUIDsToStrings(campaignIDs)).
		Order("p.campaign_id ASC, p.position ASC").
		Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	for _, row := range rows {
		name := ""
		if row.Name != nil {
			name = *row.Name
		}
		out[row.CampaignID] = append(out[row.CampaignID], &model.CampaignTemplateRef{
			ID:   row.CampaignTemplateID,
			Name: name,
		})
	}
	return out, nil
}

// GetTemplateNamesByIDs returns the name of each template that still exists
func (r *Campaign) GetTemplateNamesByIDs(
	ctx context.Context,
	templateIDs []*uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(templateIDs) == 0 {
		return out, nil
	}
	var rows []database.CampaignTemplate
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignTemplate{}).
		Select("id", "name").
		Where("id IN ?", UUIDsToStrings(templateIDs)).
		Find(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	for _, row := range rows {
		if row.ID != nil {
			out[*row.ID] = row.Name
		}
	}
	return out, nil
}

// RemoveTemplateFromPools removes a template from every pool. A campaign whose
// first template it was gets the next template in its pool, or null when the
// pool held only this one. Recipients keep the id they were given.
func (r *Campaign) RemoveTemplateFromPools(
	ctx context.Context,
	templateID *uuid.UUID,
) error {
	next := r.DB.
		Table("campaign_template_pool AS p").
		Select("p.campaign_template_id").
		Where("p.campaign_id = campaigns.id AND p.campaign_template_id <> ?", templateID.String()).
		Order("p.position ASC").
		Limit(1)
	update := map[string]any{"campaign_template_id": next}
	AddUpdatedAt(update)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Model(&database.Campaign{}).
			Where("campaign_template_id = ?", templateID.String()).
			Updates(update).Error; err != nil {
			return err
		}
		return tx.
			Where("campaign_template_id = ?", templateID.String()).
			Delete(&database.CampaignTemplatePool{}).Error
	})
}

// RemoveTemplatePoolByCampaignID removes a campaign's pool rows
func (r *Campaign) RemoveTemplatePoolByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	return r.DB.WithContext(ctx).
		Where("campaign_id = ?", campaignID.String()).
		Delete(&database.CampaignTemplatePool{}).Error
}

// whereUsesTemplates limits a campaign query to the campaigns that use any of the
// templates, as the first template or in the pool. A template kept only by
// recipients already sent to after it left the pool does not count, so removing
// it does not close a campaign that no longer sends it.
func (r *Campaign) whereUsesTemplates(db *gorm.DB, templateIDs []string) *gorm.DB {
	inPool := r.DB.
		Model(&database.CampaignTemplatePool{}).
		Select("campaign_id").
		Where("campaign_template_id IN ?", templateIDs)
	return db.Where(
		"campaigns.campaign_template_id IN ? OR campaigns.id IN (?)",
		templateIDs,
		inPool,
	)
}

// GetOpenCampaignNamesSharingTemplate returns the names of campaigns that are
// not closed and hold the template in a pool together with other templates, at
// most limit of them
func (r *Campaign) GetOpenCampaignNamesSharingTemplate(
	ctx context.Context,
	templateID *uuid.UUID,
	limit int,
) ([]string, error) {
	withTemplate := r.DB.
		Model(&database.CampaignTemplatePool{}).
		Select("campaign_id").
		Where("campaign_template_id = ?", templateID.String())
	shared := r.DB.
		Model(&database.CampaignTemplatePool{}).
		Select("campaign_id").
		Group("campaign_id").
		Having("COUNT(*) > 1")
	names := []string{}
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where("closed_at IS NULL").
		Where("id IN (?)", withTemplate).
		Where("id IN (?)", shared).
		Order("name ASC").
		Limit(limit).
		Pluck("name", &names)
	if res.Error != nil {
		return nil, res.Error
	}
	return names, nil
}

// GetUsedTemplateNames returns the names of the templates a campaign's
// recipients were given, using the campaign's first template for recipients
// without one, at most limit of them. A deleted template is named
// "(deleted template)".
func (r *Campaign) GetUsedTemplateNames(
	ctx context.Context,
	campaignID *uuid.UUID,
	limit int,
) ([]string, error) {
	used := r.DB.
		Table("campaign_recipients AS cr").
		Select("DISTINCT COALESCE(cr.campaign_template_id, c.campaign_template_id) AS template_id").
		Joins("JOIN campaigns c ON c.id = cr.campaign_id").
		Where("cr.campaign_id = ?", campaignID.String())
	type nameRow struct {
		Name *string
	}
	var rows []nameRow
	res := r.DB.WithContext(ctx).
		Table("(?) AS used", used).
		Select("t.name AS name").
		Joins("LEFT JOIN campaign_templates t ON t.id = used.template_id").
		Where("used.template_id IS NOT NULL").
		Order("t.name ASC").
		Limit(limit).
		Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Name == nil {
			out = append(out, "(deleted template)")
			continue
		}
		out = append(out, *row.Name)
	}
	return out, nil
}

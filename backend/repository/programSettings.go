package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProgramCampaignSettings is what a program copies to each of its hidden
// campaigns: the deny and evasion page, allow or deny lists, webhooks and
// whether submitted data is saved.
type ProgramCampaignSettings struct {
	DenyPageID        *uuid.UUID
	EvasionPageID     *uuid.UUID
	AllowDenyIDs      []*uuid.UUID
	Webhooks          []*model.CampaignWebhook
	SaveSubmittedData bool
}

// applyProgramSettingsRow fills the page ids of a program read from its row.
func applyProgramSettingsRow(program *model.Program, row *database.Program) {
	program.DenyPageID = nullable.NewNullNullable[uuid.UUID]()
	if row.DenyPageID != nil {
		program.DenyPageID.Set(*row.DenyPageID)
	}
	program.EvasionPageID = nullable.NewNullNullable[uuid.UUID]()
	if row.EvasionPageID != nil {
		program.EvasionPageID.Set(*row.EvasionPageID)
	}
}

// GetAllowDenyIDs returns the allow or deny list ids set on a program.
func (r *Program) GetAllowDenyIDs(ctx context.Context, programID *uuid.UUID) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.ProgramAllowDeny{}).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)), "allow_deny_id")
}

// GetWebhooks returns the webhooks set on a program with their settings.
func (r *Program) GetWebhooks(ctx context.Context, programID *uuid.UUID) ([]*model.CampaignWebhook, error) {
	var rows []database.ProgramWebhook
	res := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*model.CampaignWebhook, 0, len(rows))
	for _, row := range rows {
		out = append(out, &model.CampaignWebhook{
			WebhookID:          nullable.NewNullableWithValue(*row.WebhookID),
			WebhookIncludeData: nullable.NewNullableWithValue(row.WebhookIncludeData),
			WebhookEvents:      nullable.NewNullableWithValue(row.WebhookEvents),
		})
	}
	return out, nil
}

// ReplaceAllowDenies sets the allow or deny lists of a program.
func (r *Program) ReplaceAllowDenies(ctx context.Context, programID *uuid.UUID, ids []*uuid.UUID) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceAllowDeniesTx(tx, programID, ids)
	})
}

// replaceAllowDeniesTx sets the allow or deny lists of a program inside a
// transaction
func replaceAllowDeniesTx(tx *gorm.DB, programID *uuid.UUID, ids []*uuid.UUID) error {
	rows := programAllowDenyRows(programID, ids)
	if err := tx.Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Delete(&database.ProgramAllowDeny{}).Error; err != nil {
		return errs.Wrap(err)
	}
	if len(rows) == 0 {
		return nil
	}
	return errs.Wrap(tx.Create(&rows).Error)
}

// ReplaceWebhooks sets the webhooks of a program.
func (r *Program) ReplaceWebhooks(ctx context.Context, programID *uuid.UUID, webhooks []*model.CampaignWebhook) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceWebhooksTx(tx, programID, webhooks)
	})
}

// replaceWebhooksTx sets the webhooks of a program inside a transaction
func replaceWebhooksTx(tx *gorm.DB, programID *uuid.UUID, webhooks []*model.CampaignWebhook) error {
	rows := []database.ProgramWebhook{}
	seen := map[uuid.UUID]bool{}
	for _, wh := range webhooks {
		if wh == nil {
			continue
		}
		id, err := wh.WebhookID.Get()
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		webhookID := id
		rows = append(rows, database.ProgramWebhook{
			ProgramID:          programID,
			WebhookID:          &webhookID,
			WebhookIncludeData: wh.GetWebhookIncludeDataOrDefault(),
			WebhookEvents:      wh.GetWebhookEventsOrDefault(),
		})
	}
	if err := tx.Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Delete(&database.ProgramWebhook{}).Error; err != nil {
		return errs.Wrap(err)
	}
	if len(rows) == 0 {
		return nil
	}
	return errs.Wrap(tx.Create(&rows).Error)
}

// GetOpenCampaignIDs returns the hidden campaigns of a program that are not closed.
func (r *Program) GetOpenCampaignIDs(ctx context.Context, programID *uuid.UUID) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}), "id")
}

// ApplyCampaignSettings writes the program settings onto hidden campaigns,
// replacing their pages, allow or deny lists, webhooks and data saving in one
// transaction.
func (r *Program) ApplyCampaignSettings(
	ctx context.Context,
	campaignIDs []*uuid.UUID,
	settings *ProgramCampaignSettings,
) error {
	if len(campaignIDs) == 0 {
		return nil
	}
	allowDenyRows := []database.CampaignAllowDeny{}
	webhookRows := []database.CampaignWebhook{}
	for _, campaignID := range campaignIDs {
		for _, id := range settings.AllowDenyIDs {
			allowDenyRows = append(allowDenyRows, database.CampaignAllowDeny{CampaignID: campaignID, AllowDenyID: id})
		}
		seen := map[uuid.UUID]bool{}
		for _, wh := range settings.Webhooks {
			id, err := wh.WebhookID.Get()
			if err != nil || seen[id] {
				continue
			}
			seen[id] = true
			webhookID := id
			webhookRows = append(webhookRows, database.CampaignWebhook{
				CampaignID:         campaignID,
				WebhookID:          &webhookID,
				WebhookIncludeData: wh.GetWebhookIncludeDataOrDefault(),
				WebhookEvents:      wh.GetWebhookEventsOrDefault(),
			})
		}
	}
	values := uuidValues(campaignIDs)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields := map[string]any{
			"deny_page_id":        nil,
			"evasion_page_id":     nil,
			"save_submitted_data": settings.SaveSubmittedData,
			"updated_at":          utils.NowRFC3339UTC(),
		}
		if settings.DenyPageID != nil {
			fields["deny_page_id"] = *settings.DenyPageID
		}
		if settings.EvasionPageID != nil {
			fields["evasion_page_id"] = *settings.EvasionPageID
		}
		if err := tx.Model(&database.Campaign{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: values}).
			Updates(fields).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Where(clause.IN{Column: clause.Column{Name: "campaign_id"}, Values: values}).
			Delete(&database.CampaignAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Where(clause.IN{Column: clause.Column{Name: "campaign_id"}, Values: values}).
			Delete(&database.CampaignWebhook{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(allowDenyRows) > 0 {
			if err := tx.Create(&allowDenyRows).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		if len(webhookRows) > 0 {
			if err := tx.Create(&webhookRows).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		return nil
	})
}

// RemoveWebhookFromPrograms drops a webhook from every program, used when the
// webhook is deleted.
func (r *Program) RemoveWebhookFromPrograms(ctx context.Context, webhookID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "webhook_id"}, Value: webhookID}).
		Delete(&database.ProgramWebhook{})
	return errs.Wrap(res.Error)
}

func programAllowDenyRows(programID *uuid.UUID, ids []*uuid.UUID) []database.ProgramAllowDeny {
	rows := []database.ProgramAllowDeny{}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		rows = append(rows, database.ProgramAllowDeny{ProgramID: programID, AllowDenyID: id})
	}
	return rows
}

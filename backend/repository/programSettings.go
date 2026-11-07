package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProgramCampaignSettings is what a program copies to each of its hidden
// campaigns: the deny and evasion page, allow or deny lists and webhooks.
type ProgramCampaignSettings struct {
	DenyPageID    *uuid.UUID
	EvasionPageID *uuid.UUID
	AllowDenyIDs  []*uuid.UUID
	Webhooks      []*model.CampaignWebhook
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
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramAllowDeny{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Pluck("allow_deny_id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// GetWebhooks returns the webhooks set on a program with their settings.
func (r *Program) GetWebhooks(ctx context.Context, programID *uuid.UUID) ([]*model.CampaignWebhook, error) {
	var rows []database.ProgramWebhook
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
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
	rows := programAllowDenyRows(programID, ids)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
			Delete(&database.ProgramAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		return errs.Wrap(tx.Create(&rows).Error)
	})
}

// ReplaceWebhooks sets the webhooks of a program.
func (r *Program) ReplaceWebhooks(ctx context.Context, programID *uuid.UUID, webhooks []*model.CampaignWebhook) error {
	rows := []database.ProgramWebhook{}
	seen := map[uuid.UUID]bool{}
	for _, wh := range webhooks {
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
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
			Delete(&database.ProgramWebhook{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		return errs.Wrap(tx.Create(&rows).Error)
	})
}

// DeleteSettings removes the allow or deny list and webhook links of a program.
func (r *Program) DeleteSettings(ctx context.Context, programID *uuid.UUID) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		where := clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}
		if err := tx.Where(where).Delete(&database.ProgramAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		return errs.Wrap(tx.Where(where).Delete(&database.ProgramWebhook{}).Error)
	})
}

// GetOpenCampaignIDs returns the hidden campaigns of a program that are not closed.
func (r *Program) GetOpenCampaignIDs(ctx context.Context, programID *uuid.UUID) ([]*uuid.UUID, error) {
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Pluck("id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// ApplyCampaignSettings writes the program settings onto hidden campaigns,
// replacing their pages, allow or deny lists and webhooks in one transaction.
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
			"deny_page_id":    nil,
			"evasion_page_id": nil,
			"updated_at":      time.Now().UTC(),
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

// RemoveAllowDenyFromPrograms drops an allow or deny list from every program,
// used when the list is deleted.
func (r *Program) RemoveAllowDenyFromPrograms(ctx context.Context, allowDenyID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "allow_deny_id"}, Value: allowDenyID}).
		Delete(&database.ProgramAllowDeny{})
	return errs.Wrap(res.Error)
}

// RemoveWebhookFromPrograms drops a webhook from every program, used when the
// webhook is deleted.
func (r *Program) RemoveWebhookFromPrograms(ctx context.Context, webhookID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "webhook_id"}, Value: webhookID}).
		Delete(&database.ProgramWebhook{})
	return errs.Wrap(res.Error)
}

// RemovePageFromPrograms clears a page used as deny or evasion page by any
// program, used when the page is deleted.
func (r *Program) RemovePageFromPrograms(ctx context.Context, pageID *uuid.UUID) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, column := range []string{"deny_page_id", "evasion_page_id"} {
			if err := tx.Model(&database.Program{}).
				Where(clause.Eq{Column: clause.Column{Name: column}, Value: pageID}).
				Update(column, nil).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		return nil
	})
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

// IsNameUnique reports whether no program of the company, or no global program
// when companyID is nil, already has the name
func (r *Program) IsNameUnique(ctx context.Context, name string, companyID *uuid.UUID) (bool, error) {
	return CheckNameIsUnique(ctx, r.DB, database.PROGRAM_TABLE, name, companyID, nil)
}

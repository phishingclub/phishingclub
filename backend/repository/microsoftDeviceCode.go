package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// MicrosoftDeviceCode is a repository for microsoft device code entries
type MicrosoftDeviceCode struct {
	DB *gorm.DB
}

// Insert inserts a new microsoft device code entry
func (r *MicrosoftDeviceCode) Insert(
	ctx context.Context,
	entry *model.MicrosoftDeviceCode,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := map[string]any{
		"id":               id.String(),
		"device_code":      entry.DeviceCode,
		"user_code":        entry.UserCode,
		"verification_uri": entry.VerificationURI,
		"expires_at":       utils.RFC3339UTC(*entry.ExpiresAt),
		"last_polled_at":   nil,
		"resource":         entry.Resource,
		"client_id":        entry.ClientID,
		"tenant_id":        entry.TenantID,
		"scope":            entry.Scope,
		"access_token":     "",
		"refresh_token":    "",
		"id_token":         "",
		"captured":         false,
		"captured_once":    entry.CapturedOnce,
		"proxy_url":        entry.ProxyURL,
	}
	if v, err := entry.CampaignID.Get(); err == nil {
		row["campaign_id"] = v.String()
	}
	if v, err := entry.RecipientID.Get(); err == nil {
		row["recipient_id"] = v.String()
	}
	AddTimestamps(row)
	res := r.DB.WithContext(ctx).Model(&database.MicrosoftDeviceCode{}).Create(row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetByCampaignAndRecipientID returns a microsoft device code entry for the given campaign and recipient
func (r *MicrosoftDeviceCode) GetByCampaignAndRecipientID(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
) (*model.MicrosoftDeviceCode, error) {
	var row database.MicrosoftDeviceCode
	res := r.DB.WithContext(ctx).
		Where("campaign_id = ? AND recipient_id = ?", campaignID.String(), recipientID.String()).
		Order("created_at DESC").
		First(&row)
	if res.Error != nil {
		// return gorm.ErrRecordNotFound unwrapped so callers can use errors.Is
		return nil, res.Error
	}
	return toMicrosoftDeviceCode(&row), nil
}

// GetAllPendingNotExpired returns all microsoft device code entries that are not captured and not expired
func (r *MicrosoftDeviceCode) GetAllPendingNotExpired(ctx context.Context) ([]*model.MicrosoftDeviceCode, error) {
	var rows []database.MicrosoftDeviceCode
	res := r.DB.WithContext(ctx).
		Where("captured = false AND expires_at > ?", utils.NowRFC3339UTC()).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	result := make([]*model.MicrosoftDeviceCode, 0, len(rows))
	for i := range rows {
		result = append(result, toMicrosoftDeviceCode(&rows[i]))
	}
	return result, nil
}

// MarkCaptured marks a microsoft device code entry as captured and stores the tokens
func (r *MicrosoftDeviceCode) MarkCaptured(
	ctx context.Context,
	id *uuid.UUID,
	accessToken string,
	refreshToken string,
	idToken string,
) error {
	row := map[string]any{
		"captured":      true,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"id_token":      idToken,
	}
	AddUpdatedAt(row)
	res := r.DB.WithContext(ctx).Model(&database.MicrosoftDeviceCode{}).
		Where("id = ?", id.String()).
		Updates(row)
	return errs.Wrap(res.Error)
}

// UpdateLastPolledAt updates the last_polled_at timestamp for the given entry
func (r *MicrosoftDeviceCode) UpdateLastPolledAt(ctx context.Context, id *uuid.UUID, t time.Time) error {
	row := map[string]any{
		"last_polled_at": utils.RFC3339UTC(t),
	}
	AddUpdatedAt(row)
	res := r.DB.WithContext(ctx).Model(&database.MicrosoftDeviceCode{}).
		Where("id = ?", id.String()).
		Updates(row)
	return errs.Wrap(res.Error)
}

// DeleteByCampaignAndRecipientID deletes all microsoft device code entries for a campaign and recipient
func (r *MicrosoftDeviceCode) DeleteByCampaignAndRecipientID(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where("campaign_id = ? AND recipient_id = ?", campaignID.String(), recipientID.String()).
		Delete(&database.MicrosoftDeviceCode{})
	return errs.Wrap(res.Error)
}

// DeleteByCampaignID deletes all device code entries for the given campaign
func (r *MicrosoftDeviceCode) DeleteByCampaignID(ctx context.Context, campaignID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).
		Where("campaign_id = ?", campaignID.String()).
		Delete(&database.MicrosoftDeviceCode{})
	return errs.Wrap(res.Error)
}

func toMicrosoftDeviceCode(row *database.MicrosoftDeviceCode) *model.MicrosoftDeviceCode {
	id := nullable.NewNullableWithValue(*row.ID)
	campaignID := nullable.NewNullableWithValue(*row.CampaignID)

	var recipientID nullable.Nullable[uuid.UUID]
	recipientID.SetNull()
	if row.RecipientID != nil {
		recipientID = nullable.NewNullableWithValue(*row.RecipientID)
	}

	return &model.MicrosoftDeviceCode{
		ID:              id,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		DeviceCode:      row.DeviceCode,
		UserCode:        row.UserCode,
		VerificationURI: row.VerificationURI,
		ExpiresAt:       row.ExpiresAt,
		LastPolledAt:    row.LastPolledAt,
		Resource:        row.Resource,
		ClientID:        row.ClientID,
		TenantID:        row.TenantID,
		Scope:           row.Scope,
		AccessToken:     row.AccessToken,
		RefreshToken:    row.RefreshToken,
		IDToken:         row.IDToken,
		Captured:        row.Captured,
		CapturedOnce:    row.CapturedOnce,
		ProxyURL:        row.ProxyURL,
		CampaignID:      campaignID,
		RecipientID:     recipientID,
	}
}

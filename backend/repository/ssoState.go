package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
)

// SSOState is the repository for SSO PKCE/CSRF state tokens.
type SSOState struct {
	DB *gorm.DB
}

// SSOStateRecord is a plain struct used to pass state data between
// the service layer and this repository without importing the database
// package in the service.
type SSOStateRecord struct {
	ID           string
	StateToken   string
	CodeVerifier string
	Nonce        string
	ExpiresAt    *time.Time
	Used         bool
	UsedAt       *time.Time
}

// Insert persists a new SSO state record and returns its ID.
func (r *SSOState) Insert(
	ctx context.Context,
	stateToken string,
	codeVerifier string,
	nonce string,
	expiresAt *time.Time,
) (string, error) {
	id := uuid.New().String()
	now := time.Now()
	record := &database.SSOState{
		ID:           id,
		StateToken:   stateToken,
		CodeVerifier: codeVerifier,
		Nonce:        nonce,
		CreatedAt:    &now,
		ExpiresAt:    expiresAt,
		Used:         false,
	}
	if err := r.DB.WithContext(ctx).Create(record).Error; err != nil {
		return "", errs.Wrap(err)
	}
	return id, nil
}

// GetByStateToken retrieves an unexpired, unused state record by its
// state token value.  Returns gorm.ErrRecordNotFound when no match exists.
func (r *SSOState) GetByStateToken(
	ctx context.Context,
	stateToken string,
) (*SSOStateRecord, error) {
	now := time.Now()
	var record database.SSOState
	err := r.DB.WithContext(ctx).
		Where("state_token = ? AND used = ? AND expires_at > ?", stateToken, false, now).
		First(&record).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return toSSOStateRecord(&record), nil
}

// MarkAsUsed marks the record with the given ID as consumed so it cannot
// be replayed.
func (r *SSOState) MarkAsUsed(
	ctx context.Context,
	id string,
) error {
	now := time.Now()
	err := r.DB.WithContext(ctx).
		Model(&database.SSOState{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"used":    true,
			"used_at": &now,
		}).Error
	if err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// DeleteExpired removes all records whose expiry time has passed.
func (r *SSOState) DeleteExpired(ctx context.Context) error {
	now := time.Now()
	err := r.DB.WithContext(ctx).
		Where("expires_at < ?", now).
		Delete(&database.SSOState{}).Error
	if err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// toSSOStateRecord maps a database row to the plain repository record type.
func toSSOStateRecord(d *database.SSOState) *SSOStateRecord {
	return &SSOStateRecord{
		ID:           d.ID,
		StateToken:   d.StateToken,
		CodeVerifier: d.CodeVerifier,
		Nonce:        d.Nonce,
		ExpiresAt:    d.ExpiresAt,
		Used:         d.Used,
		UsedAt:       d.UsedAt,
	}
}

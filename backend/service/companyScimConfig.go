package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// CompanyScimConfig is the service for SCIM configuration per company
type CompanyScimConfig struct {
	Common
	CompanyScimConfigRepository *repository.CompanyScimConfig
}

// generateToken creates a cryptographically random 32-byte token and returns
// the hex-encoded plain token and its 8-character prefix
func generateScimToken() (plain string, prefix string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("failed to generate random token: %w", err)
	}
	plain = hex.EncodeToString(buf)
	prefix = plain[:8]
	return plain, prefix, nil
}

// hashScimToken bcrypt-hashes the plain token with cost 12
func hashScimToken(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", fmt.Errorf("failed to hash token: %w", err)
	}
	return string(hash), nil
}

// GetByCompanyID returns the SCIM config for the given company
func (s *CompanyScimConfig) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.CompanyScimConfig, error) {
	ae := NewAuditEvent("CompanyScimConfig.GetByCompanyID", session)
	ae.Details["companyID"] = companyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get
	config, err := s.CompanyScimConfigRepository.GetByCompanyID(ctx, companyID)
	if err != nil {
		s.Logger.Errorw("failed to get scim config by company id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return config, nil
}

// Upsert creates or updates the SCIM configuration for the given company.
// when creating, a new token is generated and returned in plain text (shown once).
// when updating, the token is NOT rotated.
func (s *CompanyScimConfig) Upsert(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	enabled bool,
) (*model.CompanyScimConfig, error) {
	ae := NewAuditEvent("CompanyScimConfig.Upsert", session)
	ae.Details["companyID"] = companyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// check if a config already exists for this company
	existing, err := s.CompanyScimConfigRepository.GetByCompanyID(ctx, companyID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("failed to look up existing scim config", "error", err)
		return nil, errs.Wrap(err)
	}
	if existing != nil {
		// update enabled and linked group only; do not regenerate the token
		existing.Enabled = enabled
		existingID, idErr := existing.ID.Get()
		if idErr != nil {
			s.Logger.Errorw("scim config has no id", "error", idErr)
			return nil, errs.Wrap(idErr)
		}
		if err := s.CompanyScimConfigRepository.UpdateByID(ctx, &existingID, existing); err != nil {
			s.Logger.Errorw("failed to update scim config", "error", err)
			return nil, errs.Wrap(err)
		}
		ae.Details["id"] = existingID.String()
		ae.Details["action"] = "update"
		s.AuditLogAuthorized(ae)
		return existing, nil
	}
	// create: generate a fresh token
	plain, prefix, err := generateScimToken()
	if err != nil {
		s.Logger.Errorw("failed to generate scim token", "error", err)
		return nil, errs.Wrap(err)
	}
	tokenHash, err := hashScimToken(plain)
	if err != nil {
		s.Logger.Errorw("failed to hash scim token", "error", err)
		return nil, errs.Wrap(err)
	}
	config := &model.CompanyScimConfig{
		CompanyID:   nullable.NewNullableWithValue(*companyID),
		Enabled:     enabled,
		TokenPrefix: nullable.NewNullableWithValue(prefix),
	}
	id, err := s.CompanyScimConfigRepository.Insert(ctx, config, tokenHash)
	if err != nil {
		s.Logger.Errorw("failed to insert scim config", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	ae.Details["action"] = "insert"
	s.AuditLogAuthorized(ae)
	// load the persisted record and attach the plain token for one-time display
	created, err := s.CompanyScimConfigRepository.GetByID(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to fetch newly inserted scim config", "error", err)
		return nil, errs.Wrap(err)
	}
	// set plain token on model for one-time display; it is never persisted
	created.Token = plain

	return created, nil
}

// RotateToken generates a new token for the existing SCIM config identified by
// companyID. the plain token is returned once so the caller can present it to
// the user; only the hash is persisted.
func (s *CompanyScimConfig) RotateToken(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*model.CompanyScimConfig, error) {
	ae := NewAuditEvent("CompanyScimConfig.RotateToken", session)
	ae.Details["companyID"] = companyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// fetch existing config
	config, err := s.CompanyScimConfigRepository.GetByCompanyID(ctx, companyID)
	if err != nil {
		s.Logger.Errorw("failed to get scim config for token rotation", "error", err)
		return nil, errs.Wrap(err)
	}
	configID, idErr := config.ID.Get()
	if idErr != nil {
		s.Logger.Errorw("scim config has no id", "error", idErr)
		return nil, errs.Wrap(idErr)
	}
	// generate a new token
	plain, prefix, err := generateScimToken()
	if err != nil {
		s.Logger.Errorw("failed to generate new scim token", "error", err)
		return nil, errs.Wrap(err)
	}
	tokenHash, err := hashScimToken(plain)
	if err != nil {
		s.Logger.Errorw("failed to hash new scim token", "error", err)
		return nil, errs.Wrap(err)
	}
	if err := s.CompanyScimConfigRepository.UpdateTokenByID(ctx, &configID, tokenHash, prefix); err != nil {
		s.Logger.Errorw("failed to update scim token", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = configID.String()
	s.AuditLogAuthorized(ae)
	// reload and return with plain token set for one-time display
	updated, err := s.CompanyScimConfigRepository.GetByID(ctx, &configID)
	if err != nil {
		s.Logger.Errorw("failed to fetch scim config after token rotation", "error", err)
		return nil, errs.Wrap(err)
	}
	// set plain token on model for one-time display; it is never persisted
	updated.Token = plain

	return updated, nil
}

// DeleteByCompanyID removes the SCIM configuration for the given company
func (s *CompanyScimConfig) DeleteByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) error {
	ae := NewAuditEvent("CompanyScimConfig.DeleteByCompanyID", session)
	ae.Details["companyID"] = companyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// delete
	if err := s.CompanyScimConfigRepository.DeleteByCompanyID(ctx, companyID); err != nil {
		s.Logger.Errorw("failed to delete scim config", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)

	return nil
}

// VerifyToken checks whether the supplied plain token matches the stored bcrypt
// hash for the given company. no session or permission check is performed
// because this is called by the inbound SCIM handler during bearer-token auth.
func (s *CompanyScimConfig) VerifyToken(
	ctx context.Context,
	companyID *uuid.UUID,
	plainToken string,
) (bool, *model.CompanyScimConfig, error) {
	// fetch config and token hash in a single round-trip
	config, tokenHash, err := s.CompanyScimConfigRepository.GetWithTokenHashByCompanyID(ctx, companyID)
	if err != nil {
		s.Logger.Errorw("failed to get scim config for token verification", "error", err)
		return false, nil, errs.Wrap(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(plainToken)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, config, nil
		}
		s.Logger.Errorw("failed to compare scim token hash", "error", err)
		return false, config, errs.Wrap(err)
	}

	return true, config, nil
}

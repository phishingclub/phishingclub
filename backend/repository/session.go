package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var allowedSessionColumns = assignTableToColumns(database.SESSION_TABLE, []string{
	"created_at",
	"updated_at",
	"ip_address",
})

// SessionOption is a session option
type SessionOption struct {
	*vo.QueryArgs

	WithUser        bool
	WithUserRole    bool
	WithUserCompany bool
}

// Session is a repository for Session
type Session struct {
	DB *gorm.DB
}

// / preload preloads the user ... with the role and company to a user?
func (r *Session) with(option *SessionOption, db *gorm.DB) *gorm.DB {
	if option.WithUser {
		db := db.Preload("User")
		if option.WithUserRole {
			db = db.Preload("User.Role")
		}
		if option.WithUserCompany {
			db = db.Preload("User.Company")
		}
		return db
	}
	return db
}

// Insert creates a new session
func (r *Session) Insert(
	ctx context.Context,
	session *model.Session,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := map[string]interface{}{
		"id":         id.String(),
		"expires_at": utils.RFC3339UTC(*session.ExpiresAt),
		"max_age_at": utils.RFC3339UTC(*session.MaxAgeAt),
		"ip_address": session.IP,
		"user_id":    session.User.ID.MustGet().String(),
	}
	AddTimestamps(row)
	result := r.DB.Model(&database.Session{}).
		Create(row)

	if result.Error != nil {
		return nil, result.Error
	}
	return &id, nil

}

// GetByID gets a session
func (r *Session) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *SessionOption,
) (*model.Session, error) {
	var dbSession database.Session
	// get session by id which is not expired or older than max age
	now := utils.NowRFC3339UTC()
	db := r.with(options, r.DB)
	result := db.First(
		&dbSession,
		fmt.Sprintf(
			"%s = ? AND %s > ? AND %s > ?",
			TableColumnID(database.SESSION_TABLE),
			TableColumn(database.SESSION_TABLE, "expires_at"),
			TableColumn(database.SESSION_TABLE, "max_age_at"),
		),
		id.String(),
		now,
		now,
	)
	if result.Error != nil {
		return nil, result.Error
	}
	return ToSession(&dbSession)
}

// GetAllActiveSessionByUserID gets all sessions by user ID
func (r *Session) GetAllActiveSessionByUserID(
	ctx context.Context,
	userID *uuid.UUID,
	options *SessionOption,
) (*model.Result[model.Session], error) {
	result := model.NewEmptyResult[model.Session]()
	var dbSessions []database.Session
	now := utils.NowRFC3339UTC()
	db := r.with(options, r.DB)
	db, err := useQuery(db, database.SESSION_TABLE, options.QueryArgs, allowedSessionColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.Find(
		&dbSessions,
		fmt.Sprintf(
			"%s = ? AND (expires_at > ? OR %s > ?)",
			TableColumn(database.SESSION_TABLE, "user_id"),
			TableColumn(database.SESSION_TABLE, "max_age_at"),
		),
		userID.String(),
		now,
		now,
	)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}
	hasNextPage, err := useHasNextPage(
		db, database.SESSION_TABLE, options.QueryArgs, allowedSessionColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbSession := range dbSessions {
		session, err := ToSession(&dbSession)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, session)
	}
	return result, nil
}

// UpdateExpiry updates a session
func (r *Session) UpdateExpiry(
	ctx context.Context,
	session *model.Session,
) error {
	row := map[string]any{
		"expires_at": utils.RFC3339UTC(*session.ExpiresAt),
	}
	AddUpdatedAt(row)
	result := r.DB.
		Model(&database.Session{}).
		Where("id = ?", session.ID.String()).
		Updates(row)

	if result.Error != nil {
		return result.Error
	}
	return nil
}

// Expire expires a session
func (r *Session) Expire(
	ctx context.Context,
	sessionID *uuid.UUID,
) error {
	now := utils.NowRFC3339UTC()
	row := map[string]any{
		"expires_at": now,
		"max_age_at": now,
	}
	AddUpdatedAt(row)
	// update both expires_at and max_age_at to now
	result := r.DB.
		Model(&database.Session{}).
		Where("id = ?", sessionID.String()).
		Updates(row)

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func ToSession(row *database.Session) (*model.Session, error) {
	var user *model.User
	if row.User != nil {
		u, err := ToUser(row.User)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		user = u
	}
	return &model.Session{
		ID:        row.ID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		ExpiresAt: row.ExpiresAt,
		MaxAgeAt:  row.MaxAgeAt,
		IP:        row.IPAddress,
		User:      user,
	}, nil
}

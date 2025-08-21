package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var sessionAllowedColumns = assignTableToColumns(database.SESSION_TABLE, []string{
	"created_at",
	"updated_at",
	"ip_address",
})

var userAllowedColumns = assignTableToColumns(database.USER_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"username",
	"email",
})

// UserOption is a user option
type UserOption struct {
	*vo.QueryArgs

	WithRole    bool
	WithCompany bool
}

// User is a repository for User
type User struct {
	DB *gorm.DB
}

// / with preloads the role and company to a user
func (r *User) with(options *UserOption, db *gorm.DB) *gorm.DB {
	if options.WithRole {
		db = db.Preload("Role")
	}
	if options.WithCompany {
		db = db.Preload("Company")
	}
	return db
}

// SetupTOTP adds TOTP to a user
// adds the url and secret, but does not enable TOTP
func (r *User) SetupTOTP(
	ctx context.Context,
	userID *uuid.UUID,
	secret string,
	recoveryCodes string,
	url string,
) error {
	row :=
		map[string]any{
			"totp_enabled":       false,
			"totp_secret":        secret,
			"totp_recovery_code": recoveryCodes,
			"totp_auth_url":      url,
		}
	AddUpdatedAt(row)
	result := r.DB.
		Model(&database.User{}).
		Where("id = ?", userID.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// EnableTOTP enables TOTP for a user
func (r *User) EnableTOTP(
	ctx context.Context,
	userID *uuid.UUID,
) error {
	row := map[string]any{
		"totp_enabled": true,
	}
	AddUpdatedAt(row)
	result := r.DB.
		Model(&database.User{}).
		Where("id = ?", userID.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// RemoveTOTP removes TOTP from a user
func (r *User) RemoveTOTP(
	ctx context.Context,
	userID *uuid.UUID,
) error {
	row := map[string]any{
		"totp_enabled":       false,
		"totp_secret":        "",
		"totp_auth_url":      "",
		"totp_recovery_code": "",
	}
	AddUpdatedAt(row)
	result := r.DB.
		Model(&database.User{}).
		Where("id = ?", userID.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// GetTOTP gets TOTP from a user
func (r *User) GetTOTP(
	ctx context.Context,
	userID *uuid.UUID,
) (string, string, error) {
	dbUser := &database.User{}
	result := r.DB.
		Select("totp_secret", "totp_auth_url").
		Where("id = ?", userID.String()).
		First(&dbUser)

	if result.Error != nil {
		return "", "", result.Error
	}
	return dbUser.TOTPSecret, dbUser.TOTPAuthURL, nil
}

// GetMFARecoveryCode gets the TOTP secret for a user
func (r *User) GetMFARecoveryCode(
	ctx context.Context,
	userID *uuid.UUID,
) (string, error) {
	dbUser := &database.User{}
	result := r.DB.
		Select("totp_recovery_code").
		Where("id = ?", userID.String()).
		First(&dbUser)

	if result.Error != nil {
		return "", result.Error
	}
	return dbUser.TOTPRecoveryCode, nil
}

// IsTOTPEnabled checks if TOTP is enabled for a user
func (r *User) IsTOTPEnabled(
	ctx context.Context,
	userID *uuid.UUID,
) (bool, error) {
	dbUser := &database.User{}
	result := r.DB.
		Select("totp_enabled").
		Where("id = ?", userID.String()).
		First(&dbUser)

	if result.Error != nil {
		return false, result.Error
	}
	return dbUser.TOTPEnabled, nil
}

// Insert creates a new user
func (r *User) Insert(
	ctx context.Context,
	user *model.User,
	passwordHash string,
	ssoID string,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := user.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	row["password_hash"] = passwordHash
	row["sso_id"] = ssoID

	res := r.DB.
		Model(&database.User{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// UpdateByID updates a user by id
func (r *User) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	user *model.User,
) error {
	row := user.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// UpsertAPIKey upserts api key
func (r *User) UpsertAPIKey(
	ctx context.Context,
	id *uuid.UUID,
	key string,
) error {
	row := map[string]any{}
	AddUpdatedAt(row)
	row["api_key"] = key
	res := r.DB.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveAPIKey deletes a api key
func (r *User) RemoveAPIKey(
	ctx context.Context,
	id *uuid.UUID,
) error {
	row := map[string]any{}
	AddUpdatedAt(row)
	row["api_key"] = ""
	res := r.DB.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetAPIKey gets the users api key
func (r *User) GetAPIKey(
	ctx context.Context,
	id *uuid.UUID,
) (string, error) {
	dbUser := &database.User{}
	result := r.DB.
		Select("api_key").
		Where("id = ?", id.String()).
		First(&dbUser)

	if result.Error != nil {
		return "", result.Error
	}
	return dbUser.APIKey, nil
}

// GetAllAPIKeys gets alll the users api keys
// return map[apiKey]userID
func (r *User) GetAllAPIKeys(
	ctx context.Context,
) (map[string]*uuid.UUID, error) {
	apiKeys := map[string]*uuid.UUID{}
	dbUsers := []database.User{}
	result := r.DB.
		Select("id, api_key").
		First(&dbUsers)

	if result.Error != nil {
		return apiKeys, result.Error
	}
	for _, dbUser := range dbUsers {
		apiKeys[dbUser.APIKey] = dbUser.ID
	}
	return apiKeys, nil
}

// DeleteByID deletes a user by id
func (r *User) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	// anonymize user
	// 	anon := uuid.New()
	newName := fmt.Sprintf(
		"deleted-%s",
		uuid.New().String(),
	)
	res := r.DB.
		Table(database.USER_TABLE).
		Where("id = ?", id.String()).
		Updates(map[string]any{
			"name":               newName,
			"username":           newName,
			"email":              fmt.Sprintf("%s@deleted.deleteduser", newName),
			"password_hash":      "",
			"totp_enabled":       false,
			"totp_secret":        nil,
			"totp_auth_url":      nil,
			"totp_recovery_code": nil,
			"sso_id":             "",
		})

	if res.Error != nil {
		return res.Error
	}

	res = r.DB.
		Where("id = ?", id.String()).
		Delete(&database.User{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetAll gets all users
func (r *User) GetAll(
	ctx context.Context,
	options *UserOption,
) (*model.Result[model.User], error) {
	result := model.NewEmptyResult[model.User]()
	dbUsers := []database.User{}

	db, err := useQuery(r.DB, database.USER_TABLE, options.QueryArgs, userAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := r.with(options, db).
		Find(&dbUsers)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(
		db, database.USER_TABLE, options.QueryArgs, userAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbUsers := range dbUsers {
		usr, err := ToUser(&dbUsers)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, usr)
	}
	return result, nil
}

// GetByID gets a user by id, includding the role and company
func (r *User) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *UserOption,
) (*model.User, error) {
	if id == nil {
		return nil, errs.Wrap(errors.New("ID is nil"))
	}
	dbUser := &database.User{}
	result := r.with(options, r.DB).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.USER_TABLE),
			),
			id.String(),
		).
		First(&dbUser)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToUser(dbUser)
}

// GetByUsername gets a user by username
func (r *User) GetByUsername(
	ctx context.Context,
	username *vo.Username,
	options *UserOption,
) (*model.User, error) {
	dbUser := &database.User{}
	result := r.with(options, r.DB).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.USER_TABLE, "username"),
			),
			username.String(),
		).
		First(&dbUser)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToUser(dbUser)
}

// GetByEmail gets a user by email
func (r *User) GetByEmail(
	ctx context.Context,
	email *vo.Email,
	options *UserOption,
) (*model.User, error) {
	dbUser := &database.User{}
	result := r.with(options, r.DB).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.USER_TABLE, "email"),
			),
			email.String(),
		).
		First(&dbUser)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToUser(dbUser)
}

func (r *User) GetPasswordHashByUsername(
	ctx context.Context,
	username *vo.Username,
) (string, error) {
	dbUser := &database.User{}
	result := r.DB.
		Select("password_hash").
		Where("username = ?", username.String()).
		First(&dbUser)

	if result.Error != nil {
		return "", result.Error
	}
	return dbUser.PasswordHash, nil
}

// GetBySessionID gets a user by session id
// this does not validate the session
func (r *User) GetBySessionID(
	ctx context.Context,
	sessionID *uuid.UUID,
	options *UserOption,
) (*model.User, error) {
	dbUser := &database.User{}
	db, err := useQuery(r.DB, database.SESSION_TABLE, options.QueryArgs, allowedSessionColumns...)

	if err != nil {
		return nil, errs.Wrap(err)
	}
	result := r.with(options, db).
		Joins("JOIN sessions ON sessions.user_id = users.id").
		Where("sessions.id = ?", sessionID.String()).
		First(&dbUser)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToUser(dbUser)

}

// updateUsernameByID updates the username by id
func (r *User) updateUsernameByID(
	tx *gorm.DB,
	id *uuid.UUID,
	username *vo.Username,
) error {
	row := map[string]any{
		"username": username.String(),
	}
	AddUpdatedAt(row)
	result := tx.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// UpdateUserToSSO removes the password hash and sets a sso id
func (r *User) UpdateUserToSSO(
	ctx context.Context,
	id *uuid.UUID,
	ssoID string,
) error {
	result := r.DB.
		Table(database.USER_TABLE).
		Where("id = ?", id.String()).
		Updates(map[string]interface{}{
			"password_hash": "",
			"sso_id":        ssoID,
		})

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}

	return nil
}

// UpdateUserToNoSSO removes the SSO id
// f
func (r *User) UpdateUserToNoSSO(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := r.DB.
		Table(database.USER_TABLE).
		Where("id = ?", id.String()).
		Updates(map[string]interface{}{
			"sso_id": "",
		})

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}

	return nil
}

// UpdateUsernameByID updates the username by id
func (r *User) UpdateUsernameByID(
	ctx context.Context,
	id *uuid.UUID,
	username *vo.Username,
) error {
	return r.updateUsernameByID(r.DB, id, username)
}

// UpdateUsernameByIDWithTransaction updates the username by id
func (r *User) UpdateUsernameByIDWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	id *uuid.UUID,
	username *vo.Username,
) error {
	return r.updateUsernameByID(tx, id, username)
}

// updateFullNameByID updates the full name by id
func (r *User) updateFullNameByID(
	tx *gorm.DB,
	id *uuid.UUID,
	name *vo.UserFullname,
) error {
	row := map[string]any{
		"name": name.String(),
	}
	AddUpdatedAt(row)
	result := tx.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// UpdateFullNameByID updates the full name by id
func (r *User) UpdateFullNameByID(
	ctx context.Context,
	id *uuid.UUID,
	name *vo.UserFullname,
) error {
	return r.updateFullNameByID(r.DB, id, name)
}

// UpdateFullNameByIDWithTransaction updates the full name by id
func (r *User) UpdateFullNameByIDWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	id *uuid.UUID,
	name *vo.UserFullname,
) error {
	return r.updateFullNameByID(tx, id, name)
}

// updateEmailByID updates the email by id
func (r *User) updateEmailByID(
	tx *gorm.DB,
	id *uuid.UUID,
	email *vo.Email,
) error {
	row := map[string]any{
		"email": email.String(),
	}
	AddUpdatedAt(row)
	result := tx.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// UpdateEmailByID updates the email by id
func (r *User) UpdateEmailByID(
	ctx context.Context,
	id *uuid.UUID,
	email *vo.Email,
) error {
	return r.updateEmailByID(r.DB, id, email)
}

// UpdateEmailByIDWithTransaction updates the email by id
func (r *User) UpdateEmailByIDWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	id *uuid.UUID,
	email *vo.Email,
) error {
	return r.updateEmailByID(tx, id, email)
}

// updatePasswordHashByID updates the password hash by id
func (r *User) updatePasswordHashByID(
	tx *gorm.DB,
	id *uuid.UUID,
	passwordHash string,
) error {
	row := map[string]interface{}{
		"password_hash": passwordHash,
	}
	AddUpdatedAt(row)
	result := tx.
		Model(&database.User{}).
		Where("id = ?", id.String()).
		Updates(row)

	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// UpdatePasswordHashByID updates the password hash by id
func (r *User) UpdatePasswordHashByID(
	ctx context.Context,
	id *uuid.UUID,
	passwordHash string,
) error {
	return r.updatePasswordHashByID(r.DB, id, passwordHash)
}

// UpdatePasswordHashByIDWithTransaction updates the password hash by id
func (r *User) UpdatePasswordHashByIDWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	id *uuid.UUID,
	passwordHash string,
) error {
	return r.updatePasswordHashByID(tx, id, passwordHash)
}

// updatePasswordHashByID updates the password hash by id
func (r *User) updatePasswordHashByUsername(
	tx *gorm.DB,
	username *vo.Username,
	passwordHash string,
) error {
	row := map[string]interface{}{
		"password_hash":          passwordHash,
		"require_password_renew": false,
	}
	AddUpdatedAt(row)
	result := tx.
		Model(&database.User{}).
		Where("username = ?", username.String()).
		Updates(row)
	if result.Error != nil {
		return errs.Wrap(result.Error)
	}
	return nil
}

// UpdatePasswordHashByUsername updates the password hash by id
func (r *User) UpdatePasswordHashByUsername(
	ctx context.Context,
	username *vo.Username,
	passwordHash string,
) error {
	return r.updatePasswordHashByUsername(r.DB, username, passwordHash)
}

// UpdatePasswordHashByUsernameWithTransaction updates the password hash by id
func (r *User) UpdatePasswordHashByUsernameWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	username *vo.Username,
	passwordHash string,
) error {
	return r.updatePasswordHashByUsername(tx, username, passwordHash)
}

func ToUser(row *database.User) (*model.User, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	roleID := nullable.NewNullableWithValue(*row.RoleID)
	userFullname := nullable.NewNullableWithValue(*vo.NewUserFullnameMust(row.Name))
	username := nullable.NewNullableWithValue(*vo.NewUsernameMust(row.Username))
	email := nullable.NewNullableWithValue(*vo.NewEmailMust(row.Email))
	ssoID := nullable.NewNullableWithValue(row.SSOID)
	var role *model.Role
	if row.Role != nil {
		role = ToRole(row.Role)
	}
	var company *model.Company
	if row.Company != nil {
		company = ToCompany(row.Company)
	}

	return &model.User{
		ID:                   id,
		Name:                 userFullname,
		Username:             username,
		Email:                email,
		RoleID:               roleID,
		Role:                 role,
		RequirePasswordRenew: nullable.NewNullableWithValue(row.RequirePasswordRenew),
		CompanyID:            companyID,
		Company:              company,
		SSOID:                ssoID,
	}, nil
}

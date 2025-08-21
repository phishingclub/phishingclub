package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strings"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/password"
	"github.com/phishingclub/phishingclub/random"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// TOTPValues is TOTP related values
type TOTPValues struct {
	Secret       string
	URL          string
	RecoveryCode string
}

// User is a service for User
type User struct {
	Common
	UserRepository    *repository.User
	RoleRepository    *repository.Role
	CompanyRepository *repository.Company
	PasswordVerifier  *password.Argon2Verifier
	PasswordHasher    *password.Argon2Hasher
}

// Create creates a new user
func (u *User) Create(
	ctx context.Context,
	session *model.Session,
	newUser *model.UserUpsertRequest,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("User.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// check if the username is already taken
	_, err = u.UserRepository.GetByUsername(
		ctx,
		&newUser.Username,
		&repository.UserOption{},
	)
	// if there is not record not found error, then thw username is already user
	if err == nil {
		u.Logger.Debugw("username is already taken", "username", newUser.Username.String())
		return nil, validate.WrapErrorWithField(errors.New("not unique"), "username")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		u.Logger.Errorw("failed to create user - failed to get user by username", "error", err)
		return nil, errs.Wrap(err)
	}
	// check if the email is already taken
	_, err = u.UserRepository.GetByEmail(
		ctx,
		&newUser.Email,
		&repository.UserOption{},
	)
	if err == nil {
		u.Logger.Debugw("email is already taken", "email", newUser.Email.String())

		return nil, validate.WrapErrorWithField(errors.New("not unique"), "email")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		u.Logger.Errorw("failed to create user - failed to get user by email", "error", err)
		return nil, errs.Wrap(err)
	}
	adminRole, err := u.RoleRepository.GetByName(
		ctx,
		data.RoleSuperAdministrator,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// create
	user := model.User{
		Username: nullable.NewNullableWithValue(newUser.Username),
		Email:    nullable.NewNullableWithValue(newUser.Email),
		Name:     nullable.NewNullableWithValue(newUser.Fullname),
		RoleID:   nullable.NewNullableWithValue(adminRole.ID),
	}
	passwdHash, err := u.PasswordHasher.Hash(newUser.Password.String())
	if err != nil {
		u.Logger.Errorw("failed to create user - failed to hash password", "error", err)
		return nil, errs.Wrap(err)
	}
	// validate
	if err := user.Validate(); err != nil {
		u.Logger.Debugw("failed to create user - failed to validate user", "error", err)
		return nil, errs.Wrap(err)
	}
	// save the user
	id, err := u.UserRepository.Insert(
		ctx,
		&user,
		passwdHash,
		"",
	)
	if err != nil {
		u.Logger.Errorw("failed to create user - failed to save user", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	u.AuditLogAuthorized(ae)

	return id, nil
}

// CreateFromSSO create a users from SSO login flow
// if the user already exists it returns the ID
func (u *User) CreateFromSSO(
	ctx context.Context,
	name string,
	email string,
	externalID string,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("User.SSOCreate", nil) // TODO could be a system session
	// check if user already exists by email
	emailVO, err := vo.NewEmail(email)
	if err != nil {
		u.Logger.Debugw("failed to setup SSO user", "error", err)
		return nil, errs.Wrap(err)
	}
	existingUser, err := u.UserRepository.GetByEmail(
		ctx,
		emailVO,
		&repository.UserOption{},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		u.Logger.Debugw("failed to setup SSO user: DB error", "error", err)
		return nil, errs.Wrap(err)
	}
	if existingUser != nil {
		// update the user to SSO by removing the password hash
		// if they dont have a SSO id
		ssoID, err := existingUser.SSOID.Get()
		if err != nil {
			u.Logger.Errorf("failed to update user to SSO", "error", err)
		}
		if len(ssoID) == 0 {
			uid := existingUser.ID.MustGet()
			err := u.UserRepository.UpdateUserToSSO(ctx, &uid, externalID)
			if err != nil {
				u.Logger.Errorf("failed to update user to SSO", "error", err)
				return nil, errs.Wrap(err)
			}
		}
		// User exists, return their ID
		id := existingUser.ID.MustGet()
		return &id, nil
	}
	// create username from email (part before @)
	username := strings.Split(email, "@")[0]
	// trim the username for non alpha numeric
	// trim username for non alpha numeric characters
	username = strings.Map(
		func(r rune) rune {
			if r >= 'a' && r <= 'z' ||
				r >= 'A' && r <= 'Z' ||
				r >= '0' && r <= '9' {
				return r
			}
			return -1
		},
		username,
	)
	usernameVO, err := vo.NewUsername(username)
	if err != nil {
		u.Logger.Debugw("failed to setup SSO user: username error", "error", err)
		return nil, errs.Wrap(err)
	}
	// check if username exists, append a random string
	count := 1
	baseUsername := username
	for {
		_, err := u.UserRepository.GetByUsername(
			ctx,
			usernameVO,
			&repository.UserOption{},
		)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		ri, err := random.RandomIntN(4)
		if err != nil {
			u.Logger.Debugw("failed to setup SSO user: rand gen error", "error", err)
			return nil, errs.Wrap(err)
		}
		usernameVO, err = vo.NewUsername(fmt.Sprintf("%s%d", baseUsername, ri))
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if count > 3 {
			err := errors.New("too many attempts at creating username")
			u.Logger.Debugw("failed to setup SSO user: username error", "error", err)
			return nil, errs.Wrap(err)
		}
		count++
	}
	nameVO, err := vo.NewUserFullname(name)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// generate a random password
	/*
		passwd, err := vo.NewReasonableLengthPasswordGenerated()
		if err != nil {
			u.Logger.Debugw("failed to setup SSO user: password generation", "error", err)
			return nil, errs.Wrap(err)
		}
		hash, err := u.PasswordHasher.Hash(passwd.String())
	*/
	// get role
	adminRole, err := u.RoleRepository.GetByName(
		ctx,
		data.RoleSuperAdministrator,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// create new user
	user := model.User{
		Username: nullable.NewNullableWithValue(*usernameVO),
		Email:    nullable.NewNullableWithValue(*emailVO),
		Name:     nullable.NewNullableWithValue(*nameVO),
		// Set default role - you might want to configure this
		RoleID: nullable.NewNullableWithValue(adminRole.ID),
	}
	// insert user
	id, err := u.UserRepository.Insert(
		ctx,
		&user,
		"", //empty hash for MFA users
		externalID,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	u.AuditLogAuthorized(ae)

	return id, nil
}

// GetMaskedAPIKey gets a masked API user key
func (u *User) GetMaskedAPIKey(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
) (string, error) {
	ae := NewAuditEvent("User.GetMaskedAPIKey", session)
	ae.Details["userId"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	// get api key
	apiKey, err := u.UserRepository.GetAPIKey(ctx, userID)
	if err != nil {
		u.Logger.Errorw("failed to get api key", "error", err)
		return "", errs.Wrap(err)
	}
	masked := ""
	if len(apiKey) > 4 {
		masked = apiKey[0:4] + strings.Repeat("*", 28)
	}
	// no audit on read

	return masked, nil
}

// GetAllAPIKeys gets all api keys as SHA256
// THIS METHOD DOES NOT HAVE AUTH, USE WITH DISCRETION
func (s *User) GetAllAPIKeysSHA256(
	ctx context.Context,
) ([]*model.APIUser, error) {
	apiUsers := []*model.APIUser{}
	// get api key
	apiKeyAndIDMap, err := s.UserRepository.GetAllAPIKeys(ctx)
	for apiKey, userID := range apiKeyAndIDMap {
		hash := sha256.Sum256([]byte(apiKey))
		apiUsers = append(
			apiUsers,
			&model.APIUser{
				ID:         userID,
				APIKeyHash: hash,
			},
		)
	}
	if err != nil {
		s.Logger.Errorw("failed to get all api keys", "error", err)
		return apiUsers, errs.Wrap(err)
	}
	return apiUsers, nil
}

// UpsertAPIKey creates/updates a user API key
func (s *User) UpsertAPIKey(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
) (string, error) {
	ae := NewAuditEvent("User.UpsertAPIKey", session)
	ae.Details["userId"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	key, err := random.GenerateRandomURLBase64Encoded(64)
	if err != nil {
		s.Logger.Errorw("failed to create api key - bad crypto", "error", err)
		return "", errs.Wrap(err)
	}
	// upsert api key
	err = s.UserRepository.UpsertAPIKey(
		ctx,
		userID,
		key,
	)
	if err != nil {
		s.Logger.Errorw("failed set api key", "error", err)
		return "", errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)

	return key, nil
}

// RemoveAPIKey removes a users api key
func (u *User) RemoveAPIKey(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
) error {
	ae := NewAuditEvent("User.RemoveAPIKey", session)
	ae.Details["userId"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	err = u.UserRepository.RemoveAPIKey(ctx, userID)
	if err != nil {
		u.Logger.Errorw("failed to remove api key", "error", err)
		return err
	}
	u.AuditLogAuthorized(ae)

	return nil
}

// UpdateByID updates a user by ID
// values to update are email, username and fullname
func (u *User) Update(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
	incoming *model.User,
) error {
	ae := NewAuditEvent("User.Update", session)
	ae.Details["id"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get user to be updated
	current, err := u.UserRepository.GetByID(ctx, userID, &repository.UserOption{})
	if err != nil {
		u.Logger.Errorw("failed to update user - failed to get user by id", "error", err)
		return err
	}
	// check if the username is already taken
	if username, err := incoming.Username.Get(); err == nil {
		_, err = u.UserRepository.GetByUsername(
			ctx,
			&username,
			&repository.UserOption{},
		)
		if err == nil && current.Username.MustGet().String() != username.String() {
			u.Logger.Debugw("username is already taken", "username", username.String())
			return validate.WrapErrorWithField(errors.New("not unique"), "username")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			u.Logger.Errorw("failed to update user - failed to get user by username", "error", err)
			return err
		}
	}
	// check if the is already taken
	if email, err := incoming.Email.Get(); err == nil {
		// check if the email is already taken
		_, err = u.UserRepository.GetByEmail(
			ctx,
			&email,
			&repository.UserOption{},
		)
		if err == nil && current.Email.MustGet().String() != incoming.Email.MustGet().String() {
			u.Logger.Debugw("email is already taken", "email", email.String())
			return validate.WrapErrorWithField(errors.New("not unique"), "email")
		}

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			u.Logger.Errorw("failed to update user - failed to get user by email", "error", err)
			return err
		}
	}
	if v, err := incoming.Email.Get(); err == nil {
		current.Email.Set(v)
	}
	if v, err := incoming.Username.Get(); err == nil {
		current.Username.Set(v)
	}
	if v, err := incoming.Name.Get(); err == nil {
		current.Name.Set(v)
	}
	// validate
	if err := current.Validate(); err != nil {
		u.Logger.Debugw("failed to update user - failed to validate user", "error", err)
		return err
	}
	// update the user
	err = u.UserRepository.UpdateByID(
		ctx,
		userID,
		current,
	)
	if err != nil {
		u.Logger.Errorw("failed to update user - failed to update user", "error", err)
		return err
	}
	u.AuditLogAuthorized(ae)

	return nil
}

// GetAll gets all users
func (u *User) GetAll(
	ctx context.Context,
	session *model.Session,
	options *repository.UserOption,
) (*model.Result[model.User], error) {
	result := model.NewEmptyResult[model.User]()
	ae := NewAuditEvent("User.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = u.UserRepository.GetAll(ctx, options)
	if err != nil {
		u.Logger.Errorw("failed to get all users - failed to get all users", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetByID gets a user by ID
func (s *User) GetByID(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
) (*model.User, error) {
	ae := NewAuditEvent("User.GetByID", session)
	ae.Details["id"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	user, err := s.UserRepository.GetByID(
		ctx,
		userID,
		&repository.UserOption{
			WithRole:    true,
			WithCompany: true,
		},
	)
	if err != nil {
		s.Logger.Errorw("failed to get user by id - failed to get user by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return user, nil
}

// GetByIDWithoutAuth gets a user by ID without requiring auth
func (s *User) GetByIDWithoutAuth(
	ctx context.Context,
	userID *uuid.UUID,
) (*model.User, error) {
	user, err := s.UserRepository.GetByID(
		ctx,
		userID,
		&repository.UserOption{
			WithRole:    true,
			WithCompany: true,
		},
	)
	if err != nil {
		s.Logger.Errorw("failed to get user by id - failed to get user by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return user, nil
}

// Delete deletes a user
func (u *User) Delete(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
) error {
	ae := NewAuditEvent("User.Delete", session)
	ae.Details["id"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if session.User.ID.MustGet().String() == userID.String() {
		u.Logger.Debugw("Attempted to delete own user", "userID", userID.String())
		return errs.NewValidationError(
			errors.New("Can not delete own user"),
		)
	}
	// delete the user
	err = u.UserRepository.DeleteByID(
		ctx,
		userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to delete user - failed to delete user", "error", err)
		return err
	}
	u.AuditLogAuthorized(ae)

	return nil
}

// SetupTOTP sets up TOTP for a user
// returns secret, url and error
func (u *User) SetupTOTP(
	ctx context.Context,
	session *model.Session,
	password *vo.ReasonableLengthPassword,
) (*TOTPValues, error) {
	ae := NewAuditEvent("User.SetupTOTP", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// check if the user is loaded in the session
	user := session.User
	if user == nil {
		u.Logger.Error("user is not loaded in session")
		return nil, errors.New("user is not loaded in session")
	}
	// check password
	username := user.Username.MustGet()
	hasValidPassword, err := u.CheckPassword(
		ctx,
		&username,
		password,
	)
	if err != nil || !hasValidPassword {
		return nil, errs.ErrAuthenticationFailed
	}
	// generate OTP
	email := user.Email.MustGet()
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Phishing Club",
		AccountName: email.String(),
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		u.Logger.Errorw("failed to setup TOTP - failed to generate key", "error", err)
		return nil, errs.Wrap(err)
	}
	rc, err := random.GenerateRandomURLBase64Encoded(24)
	if err != nil {
		u.Logger.Errorw("failed to setup TOTP - failed to generate recovery code", "error", err)
		return nil, errs.Wrap(err)
	}
	// update user
	userID := user.ID.MustGet()
	err = u.UserRepository.SetupTOTP(
		ctx,
		&userID,
		key.Secret(),
		rc,
		key.URL(),
	)
	if err != nil {
		u.Logger.Errorw("failed to setup TOTP - failed to update user", "error", err)
		return nil, errs.Wrap(err)
	}
	u.AuditLogAuthorized(ae)

	// audit log
	return &TOTPValues{
		Secret:       key.Secret(),
		URL:          key.URL(),
		RecoveryCode: rc,
	}, nil
}

// SetupCheckTOTP verifies a TOTP setup
func (u *User) SetupCheckTOTP(
	ctx context.Context,
	session *model.Session,
	token *vo.String64,
) error {
	ae := NewAuditEvent("User.SetupCheckTOTP", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get user from session
	user := session.User
	if user == nil {
		u.Logger.Error("user is not loaded in session")
		return errors.New("user is not loaded in session")
	}
	// check if the token is valid
	// get the secret
	userID := user.ID.MustGet()
	secret, _, err := u.UserRepository.GetTOTP(
		ctx,
		&userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to verify TOTP - failed to get TOTP", "error", err)
		return err
	}
	// verify the token
	u.Logger.Debug("verifying TOTP")
	valid := totp.Validate(token.String(), secret)
	if !valid {
		u.Logger.Debug("failed to verify TOTP - invalid token")
		return errs.ErrUserWrongTOTP
	}
	u.Logger.Debugw("Enabling MFA TOTP for user", "userID", userID)
	// enable TOTP
	err = u.UserRepository.EnableTOTP(
		ctx,
		&userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to verify TOTP - failed to enable TOTP", "error", err)
		return err
	}
	u.AuditLogAuthorized(ae)

	return nil
}

// IsTOTPEnabled checks if TOTP is enabled
func (u *User) IsTOTPEnabled(
	ctx context.Context,
	session *model.Session,
) (bool, error) {
	ae := NewAuditEvent("User.IsTOTPEnabled", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return false, errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return false, errs.ErrAuthorizationFailed
	}
	// get user from session
	user := session.User
	if user == nil {
		u.Logger.Error("user is not loaded in session")
		return false, errors.New("user is not loaded in session")
	}
	// check if TOTP is enabled
	userID := user.ID.MustGet()
	enabled, err := u.UserRepository.IsTOTPEnabled(
		ctx,
		&userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to check if TOTP is enabled", "error", err)
		return false, errs.Wrap(err)
	}
	// no audit on read

	return enabled, nil
}

// IsTOTPEnabledByUserID checks if TOTP is enabled by user ID
// this method had no auth check, use with consideration
func (u *User) IsTOTPEnabledByUserID(
	ctx context.Context,
	userID *uuid.UUID,
) (bool, error) {
	// check if TOTP is enabled
	enabled, err := u.UserRepository.IsTOTPEnabled(
		ctx,
		userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to check if TOTP is enabled", "error", err)
		return false, errs.Wrap(err)
	}
	return enabled, nil
}

// DisableTOTP disables TOTP
// without checking if the user privilige, use with consideration
func (u *User) DisableTOTP(
	ctx context.Context,
	userID *uuid.UUID,
) error {
	err := u.UserRepository.RemoveTOTP(
		ctx,
		userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to disable TOTP", "error", err)
		return err
	}
	// TODO audit log successful TOTP disable
	return nil
}

// CheckTOTP verifies a TOTP token
func (u *User) CheckTOTP(
	ctx context.Context,
	userID *uuid.UUID,
	token *vo.String64,
) error {
	// get the secret
	secret, _, err := u.UserRepository.GetTOTP(
		ctx,
		userID,
	)
	if err != nil {
		u.Logger.Errorw("failed to verify TOTP - failed to get TOTP", "error", err)
		return err
	}
	// verify the token
	valid := totp.Validate(token.String(), secret)
	if !valid {
		u.Logger.Debug("failed to verify TOTP - invalid token")
		return errs.ErrUserWrongTOTP
	}
	return nil
}

// AuthenticateUsernameWithPassword tests a username and password is correct
func (u *User) AuthenticateUsernameWithPassword(
	ctx context.Context,
	username string,
	passwd string,
	ip string,
) (*model.User, error) {
	ae := NewAuditEvent("User.AuthenticateUsernameWithPassword", nil)
	ae.IP = ip
	ae.Details["username"] = username
	// check the entities are valid before doing anything
	usernameEntity, err := vo.NewUsername(username)
	if err != nil {
		u.Logger.Debugw("failed to authenticate - invalid username", "error", err)
		return nil, errs.Wrap(err)
	}
	passwordEntity, err := vo.NewReasonableLengthPassword(passwd)
	if err != nil {
		u.Logger.Debugw("failed to authenticate - invalid password", "error", err)
		return nil, errs.Wrap(err)
	}
	// retrieve only the password hash to minimize the timing attack window compared to
	// pulling the user with all relations
	passwordHash, err := u.UserRepository.GetPasswordHashByUsername(
		ctx,
		usernameEntity,
	)
	errIsRecordNotFound := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !errIsRecordNotFound {
		u.Logger.Errorw("failed to authenticate - failed to get password hash", "error", err)
		return nil, errs.Wrap(err)
	}
	// if user not found - we compare with a fake fakePass hash to mitigate risk of timing attacks
	// or if the user is a SSO user
	if errIsRecordNotFound || len(passwordHash) == 0 {
		_, err = u.PasswordVerifier.Verify(passwd, password.DummyHash)
		if err != nil {
			u.Logger.Debugw("failed to verify dummy hash", "error", err)
		}
		return nil, gorm.ErrRecordNotFound
	}
	// veriy the hash in a constant time manner
	verified, err := u.PasswordVerifier.Verify(passwordEntity.String(), passwordHash)
	if err != nil {
		u.Logger.Errorw("failed to verify password hash", "error", err)
		return nil, errs.Wrap(err)
	}
	// if the password is not verifed, log it and return the error
	if !verified {
		u.AuditLogNotAuthorized(ae)
		return nil, errs.ErrUserWrongPasword
	}
	// on successful login, retrieve the user with relations and send it back
	user, err := u.UserRepository.GetByUsername(
		ctx,
		usernameEntity,
		&repository.UserOption{
			WithRole:    true,
			WithCompany: true,
		},
	)
	if err != nil {
		u.Logger.Errorw("failed to get user by username after verifying login", "error", err)
		return nil, errs.Wrap(err)
	}
	u.AuditLogAuthorized(ae)

	return user, nil
}

// CheckPassword checks if a password is correct
func (u *User) CheckPassword(
	ctx context.Context,
	username *vo.Username,
	password *vo.ReasonableLengthPassword,
) (bool, error) {
	passwordHash, err := u.UserRepository.GetPasswordHashByUsername(
		ctx,
		username,
	)
	if err != nil {
		u.Logger.Errorw("failed to check password - failed to get password hash", "error", err)
		return false, errs.Wrap(err)
	}
	verified, err := u.PasswordVerifier.Verify(password.String(), passwordHash)
	if err != nil {
		u.Logger.Errorw("failed to check password - failed to verify hash", "error", err)
		return false, errs.Wrap(err)
	}
	return verified, nil
}

// ChangePassword changes a user's password
func (u *User) ChangePassword(
	ctx context.Context,
	session *model.Session,
	currentPassword *vo.ReasonableLengthPassword,
	newPassword *vo.ReasonableLengthPassword,
) error {
	ae := NewAuditEvent("User.ChangePassword", session)
	// check if the current password is correct
	user := session.User
	if user == nil {
		u.Logger.Error("user is not loaded in session")
		return errors.New("user is not loaded in session")
	}
	username := user.Username.MustGet()
	ae.Details["id"] = user.ID.MustGet().String()
	ae.Details["username"] = username.String()
	passwordHash, err := u.UserRepository.GetPasswordHashByUsername(
		ctx,
		&username,
	)
	if err != nil {
		u.Logger.Errorw("failed to change password - failed to get password hash", "error", err)
		return err
	}
	verified, err := u.PasswordVerifier.Verify(currentPassword.String(), passwordHash)
	if err != nil {
		u.Logger.Errorw("failed to change password - failed to verify hash", "error", err)
		return err
	}
	if !verified {
		u.AuditLogNotAuthorized(ae)
		return errs.ErrUserWrongPasword
	}
	// change the password
	passwordHash, err = u.PasswordHasher.Hash(newPassword.String())
	if err != nil {
		u.Logger.Errorw("failed to change password - failed to hash new password", "error", err)
		return err
	}
	err = u.UserRepository.UpdatePasswordHashByUsername(
		ctx,
		&username,
		passwordHash,
	)
	if err != nil {
		u.Logger.Errorw("failed to change password - failed to update password hash", "error", err)
		return err
	}
	u.AuditLogAuthorized(ae)

	return nil
}

// ChangeFullname changes a users fullname
func (u *User) ChangeFullname(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
	newFullname *vo.UserFullname,
) (*vo.UserFullname, error) {
	ae := NewAuditEvent("User.ChangeFullname", session)
	ae.Details["id"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// change full name
	_, err = u.UserRepository.GetByID(ctx, userID, &repository.UserOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		u.Logger.Debugw("failed to change fullname - user not found", "error", err)
		return nil, errs.Wrap(err)
	}
	err = u.UserRepository.UpdateFullNameByID(
		ctx,
		userID,
		newFullname,
	)
	if err != nil {
		u.Logger.Errorw("failed to change fullname - failed to update fullname", "error", err)
		return nil, errs.Wrap(err)
	}
	u.AuditLogAuthorized(ae)

	return newFullname, nil
}

// ChangeEmailAsAdministrator changes a user's email
// changes a users email without validating their email
func (u *User) ChangeEmailAsAdministrator(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
	newEmail *vo.Email,
) (*vo.Email, error) {
	ae := NewAuditEvent("User.ChangeEmailAsAdministrator", session)
	ae.Details["id"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	current, err := u.UserRepository.GetByID(ctx, userID, &repository.UserOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		u.Logger.Debug("failed to change email - user not found")
		return nil, errs.Wrap(err)
	}
	if err != nil {
		u.Logger.Debugw("failed to change email - failed to get user by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// update
	current.Email.Set(*newEmail)
	err = u.UserRepository.UpdateByID(
		ctx,
		userID,
		current,
	)
	if err != nil {
		u.Logger.Errorw("failed to change email - failed to update email", "error", err)
		return nil, errs.Wrap(err)
	}
	u.AuditLogAuthorized(ae)

	return newEmail, nil
}

// ChangeUsername changes a user's username
func (u *User) ChangeUsername(
	ctx context.Context,
	session *model.Session,
	userID *uuid.UUID,
	newUsername *vo.Username,
) error {
	ae := NewAuditEvent("User.ChangeUsername", session)
	ae.Details["id"] = userID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		u.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		u.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	current, err := u.UserRepository.GetByID(ctx, userID, &repository.UserOption{})
	if err != nil {
		u.Logger.Debugw("failed to change username - failed to get user by id", "error", err)
		return err
	}
	current.Username.Set(*newUsername)
	err = u.UserRepository.UpdateByID(
		ctx,
		userID,
		current,
	)
	if err != nil {
		u.Logger.Errorw("failed to change username - failed to update username", "error", err)
		return err
	}
	u.AuditLogAuthorized(ae)

	return nil
}

// CheckMFARecoveryCode checks if a recovery code is valid
// returns true if the recovery code is valid
func (u *User) CheckMFARecoveryCode(
	ctx context.Context,
	userID *uuid.UUID,
	recoveryCode *vo.String64,
) (bool, error) {
	dbRecoveryCodeHash, err := u.UserRepository.GetMFARecoveryCode(
		ctx,
		userID,
	)
	if subtle.ConstantTimeCompare([]byte(recoveryCode.String()), []byte(dbRecoveryCodeHash)) != 1 {
		u.Logger.Info("invalid recovery code")
		return false, errs.ErrUserWrongRecoveryCode
	}
	if err != nil {
		u.Logger.Errorw("failed to get recovery code", "error", err)
		return false, errs.Wrap(err)
	}
	return true, nil
}

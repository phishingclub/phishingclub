package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/password"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// InstallSetup sets up the intial administrator account
// this is called by the installation process and is not part of the normal application flow
// it must not use services that require authentication or be used by any other part of the application
type InstallSetup struct {
	Common
	UserRepository    *repository.User
	RoleRepository    *repository.Role
	CompanyRepository *repository.Company
	PasswordHasher    *password.Argon2Hasher
}

// SetupAccounts sets up the accounts needed for the system to function
func (s *InstallSetup) SetupAccounts(
	ctx context.Context,
) (*model.User, *vo.ReasonableLengthPassword, error) {
	user, password, err := s.setupInitialAdministratorAccount(ctx)
	if err != nil {
		s.Logger.Errorw("failed to setup the initial administrator account", "error", err)
		return nil, nil, errs.Wrap(err)
	}
	return user, password, nil
}

// setupInitialAdministratorAccount sets up the initial administrator account
func (s *InstallSetup) setupInitialAdministratorAccount(
	ctx context.Context,
) (*model.User, *vo.ReasonableLengthPassword, error) {
	username := vo.NewUsernameMust(data.DefaultSacrificalAccountUsername)
	nullableUsername := nullable.NewNullableWithValue(*username)
	// get the admin user if it already exists
	// this could happend if the installation was started, but not completed
	adminUser, err := s.UserRepository.GetByUsername(
		ctx,
		username,
		&repository.UserOption{
			WithRole:    true,
			WithCompany: true,
		},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, errs.Wrap(err)
	}
	password, err := vo.NewReasonableLengthPasswordGenerated()
	if err != nil {
		return nil, nil, errs.Wrap(err)
	}
	// if the admin account does not exist, create it
	if adminUser == nil {
		adminRole, err := s.RoleRepository.GetByName(
			ctx,
			data.RoleSuperAdministrator,
		)
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
		email := nullable.NewNullableWithValue(*vo.NewEmailMust(data.DefaultSacrificalAccountEmail))
		fullname := nullable.NewNullableWithValue(*vo.NewUserFullnameMust(data.DefaultSacrificalAccountName))
		tmpAdminID := nullable.NewNullableWithValue(uuid.New())
		tmpAdmin := &model.User{
			ID:       tmpAdminID,
			Name:     fullname,
			Username: nullableUsername,
			Email:    email,
			RoleID:   nullable.NewNullableWithValue(adminRole.ID),
		}
		hash, err := s.PasswordHasher.Hash(password.String())
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}

		newUserID, err := s.UserRepository.Insert(
			ctx,
			tmpAdmin,
			hash,
			"",
		)
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
		adminUser, err = s.UserRepository.GetByID(
			ctx,
			newUserID,
			&repository.UserOption{
				WithRole:    true,
				WithCompany: false,
			},
		)
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
	} else {
		username, err := vo.NewUsername(data.DefaultSacrificalAccountUsername)
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
		// if the admin account exists, update the password
		hash, err := s.PasswordHasher.Hash(password.String())
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
		err = s.UserRepository.UpdatePasswordHashByUsername(
			ctx,
			username,
			hash,
		)
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
		return adminUser, password, nil
	}
	return adminUser, password, nil
}

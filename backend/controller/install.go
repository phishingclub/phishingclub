package controller

import (
	"fmt"

	"github.com/go-errors/errors"
	"github.com/google/uuid"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/cli"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/password"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

// SetupAdminRequest is the request for the install action
type SetupAdminRequest struct {
	Username     string `json:"username" binding:"required"`
	UserFullname string `json:"userFullname" binding:"required"`
	NewPassword  string `json:"newPassword" binding:"required"`
}

// InitialSetup is a controller used by the CLI in the
// initial setup process - it is not an API controller
type InitialSetup struct {
	Common
	CLIOutputter     cli.Outputter
	OptionRepository *repository.Option
	InstallService   *service.InstallSetup
	OptionService    *service.Option
}

// IsInstalled checks if the application is installed
// not as a
func (is *InitialSetup) IsInstalled(ctx context.Context) (bool, error) {
	isInstalledOption, err := is.OptionRepository.GetByKey(
		ctx,
		data.OptionKeyIsInstalled,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not get '%s' option: %w", data.OptionKeyIsInstalled, err)
	}
	return isInstalledOption.Value.String() == data.OptionValueIsInstalled, nil
}

// HandleInitialSetup handles the initial setup of the application
// this includes inserting the isInstalled option to not installed
// and making or updating the sacrificial admin account
func (is *InitialSetup) HandleInitialSetup(ctx context.Context) error {
	// setup option for is installed
	isInstalledOption, err := is.OptionRepository.GetByKey(
		ctx,
		data.OptionKeyIsInstalled,
	)
	// if the option does not exist, create it
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: could not get '%s' option", err, data.OptionKeyIsInstalled)
		}
		key := vo.NewString64Must(data.OptionKeyIsInstalled)
		value := vo.NewOptionalString1MBMust(data.OptionValueIsNotInstalled)
		isInstalledOptionWithoutID := model.Option{
			Key:   *key,
			Value: *value,
		}
		_, err = is.OptionRepository.Insert(
			ctx,
			&isInstalledOptionWithoutID,
		)
		if err != nil {
			return fmt.Errorf("%w: could not insert entity for option '%s'", err, data.OptionKeyIsInstalled)
		}
		isInstalledOption, err = is.OptionRepository.GetByKey(
			ctx,
			isInstalledOptionWithoutID.Key.String(),
		)
		if err != nil {
			return fmt.Errorf("%w: could not get created '%s' option", err, data.OptionKeyIsInstalled)
		}
	}
	// if no instance ID exists, add it
	instanceIDOption, err := is.OptionRepository.GetByKey(
		ctx,
		data.OptionKeyInstanceID,
	)
	// if the instance id option does not exist, create it
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: could not get '%s' option", err, data.OptionKeyInstanceID)
		}
		key := vo.NewString64Must(data.OptionKeyInstanceID)
		instanceID := uuid.New()
		value := vo.NewOptionalString1MBMust(instanceID.String())
		instanceIDOption = &model.Option{
			Key:   *key,
			Value: *value,
		}
		_, err = is.OptionRepository.Insert(
			ctx,
			instanceIDOption,
		)
		if err != nil {
			return fmt.Errorf("could not insert instance ID: %w", err)
		}
	}

	// if installation is already complete, return error
	if isInstalledOption.Value.String() == data.OptionValueIsInstalled {
		return errs.ErrAlreadyInstalled
	}
	// setup accounts
	admin, password, err := is.InstallService.SetupAccounts(ctx)
	if err != nil {
		return fmt.Errorf("could not setup initial admin account: %w", err)
	}
	is.CLIOutputter.PrintInitialAdminAccount(
		admin.Username.MustGet().String(),
		password.String(),
	)

	return nil
}

// Install is the Install controller used by the API
type Install struct {
	Common
	UserRepository    *repository.User
	CompanyRepository *repository.Company
	OptionRepository  *repository.Option
	DB                *gorm.DB
	PasswordHasher    password.Argon2Hasher
}

// Install completes the installation by setting the initial administrators and options
func (in *Install) Install(g *gin.Context) {
	tx := in.DB.Begin()
	var committed bool
	defer func() {
		if r := recover(); r != nil {
			if !committed {
				tx.Rollback()
			}
		}
	}()
	ok := in.install(g, tx)
	if !ok {
		if err := tx.Rollback().Error; err != nil {
			in.Logger.Errorw("failed to install - could not rollback transaction",
				"error", err,
			)
		}
		return
	}
	result := tx.Commit()
	if result.Error != nil {
		in.Logger.Errorw("failed to install - could not commit transaction",
			"error", result.Error,
		)
		in.Response.ServerError(g)
		return
	}
	committed = true
	// the admin user changed username and password
	// however as the install process is a special case, we wont
	// require re-authentication
	in.Response.OK(g, gin.H{})
}

// Install completes the installation by setting the initial administrators
// username, password, email, name and company name
func (in *Install) install(g *gin.Context, tx *gorm.DB) bool {
	// handle session
	_, user, ok := in.handleSession(g)
	if !ok {
		return false
	}
	role := user.Role
	if role == nil {
		in.Logger.Error("failed to install - session contain no role")
		in.Response.ServerError(g)
		return false
	}
	if !role.IsSuperAdministrator() {
		in.Logger.Info("failed to install - not super admin")
		// TODO add audit log
		in.Response.Forbidden(g)
		return false
	}
	// defer rollback or commit tx
	var request SetupAdminRequest
	if err := g.ShouldBindJSON(&request); err != nil {
		in.Logger.Debugw("failed to parse request",
			"error", err,
		)
		in.Response.BadRequest(g)
		return false
	}
	ctx := g.Request.Context()
	// check if already installed
	isInstalled, err := in.OptionRepository.GetByKey(ctx, data.OptionKeyIsInstalled)
	if err != nil {
		in.Logger.Errorw("failed to install - could not get option",
			"optionKey", data.OptionKeyIsInstalled,
			"error", err,
		)
		in.Response.ServerError(g)
		return false
	}
	if isInstalled.Value.String() == data.OptionValueIsInstalled {
		in.Logger.Info("failed to install - already installed")
		in.Response.ServerErrorMessage(
			g,
			"Installation is already complete",
		)
		return false
	}
	// update the username
	newUsername, err := vo.NewUsername(request.Username)
	if err != nil {
		in.Logger.Infow("failed to install - invalid username",
			"username", request.Username,
			"error", err,
		)
		in.Response.ValidationFailed(g, "Username", err)
		return false
	}
	if newUsername.String() == user.Username.MustGet().String() {
		in.Logger.Infow("failed to install - new username is the same as the current",
			"username", newUsername.String(),
			"error", err,
		)
		in.Response.BadRequestMessage(
			g,
			"Username may not be the same as the current",
		)
		return false
	}
	userID := user.ID.MustGet()
	err = in.UserRepository.UpdateUsernameByIDWithTransaction(
		ctx,
		tx,
		&userID,
		newUsername,
	)
	if err != nil {
		in.Logger.Infow("failed to install - could not update username",
			"username", newUsername.String(),
			"error", err,
		)
		in.Response.ServerError(g)
		return false
	}
	// update the password
	newPassword, err := vo.NewReasonableLengthPassword(request.NewPassword)
	if err != nil {
		in.Logger.Infow("failed to install - invalid password",
			"error", err,
		)
		in.Response.ValidationFailed(g, "Password", err)
		return false
	}
	hash, err := in.PasswordHasher.Hash(newPassword.String())
	if err != nil {
		in.Logger.Errorw("failed to install - could not hash password",
			"error", err,
		)
		in.Response.ServerError(g)
		return false
	}
	err = in.UserRepository.UpdatePasswordHashByIDWithTransaction(
		ctx,
		tx,
		&userID,
		hash,
	)
	if err != nil {
		in.Logger.Errorw("failed to install - could not update password",
			"error", err,
		)
		in.Response.ServerError(g)
		return false
	}
	// update the name
	newName, err := vo.NewUserFullname(request.UserFullname)
	if err != nil {
		in.Logger.Infow("failed to install - invalid name",
			"error", err,
		)
		in.Response.ValidationFailed(g, "Name", err)
		return false
	}
	err = in.UserRepository.UpdateFullNameByIDWithTransaction(
		ctx,
		tx,
		&userID,
		newName,
	)
	if err != nil {
		in.Logger.Infow("failed to install - could not update name",
			"error", err,
		)
		in.Response.ServerError(g)
		return false
	}
	// update installed option to installed
	option := model.Option{
		Key:   *vo.NewString64Must(data.OptionKeyIsInstalled),
		Value: *vo.NewOptionalString1MBMust(data.OptionValueIsInstalled),
	}
	err = in.OptionRepository.UpdateByKeyWithTransaction(
		ctx,
		tx,
		&option,
	)
	if err != nil {
		in.Logger.Errorw("failed to install - could not create install option",
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "failed to create install option")
		return false

	}
	return true
}

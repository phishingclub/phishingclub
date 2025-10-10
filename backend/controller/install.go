package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
	ImportService     *service.Import
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

// InstallTemplates downloads and imports example templates from GitHub
func (in *Install) InstallTemplates(g *gin.Context) {
	// handle session
	session, user, ok := in.handleSession(g)
	if !ok {
		return
	}
	role := user.Role
	if role == nil {
		in.Logger.Error("failed to install templates - session contain no role")
		in.Response.ServerError(g)
		return
	}
	if !role.IsSuperAdministrator() {
		in.Logger.Info("failed to install templates - not super admin")
		in.Response.Forbidden(g)
		return
	}

	ctx := g.Request.Context()

	// check if already installed
	isInstalled, err := in.OptionRepository.GetByKey(ctx, data.OptionKeyIsInstalled)
	if err != nil {
		in.Logger.Errorw("failed to install templates - could not get option",
			"optionKey", data.OptionKeyIsInstalled,
			"error", err,
		)
		in.Response.ServerError(g)
		return
	}
	if isInstalled.Value.String() != data.OptionValueIsInstalled {
		in.Logger.Info("failed to install templates - installation not complete")
		in.Response.BadRequestMessage(g, "Installation must be completed first")
		return
	}

	// create http client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// get latest release info from GitHub API
	releaseURL := "https://api.github.com/repos/phishingclub/templates/releases/latest"
	resp, err := client.Get(releaseURL)
	if err != nil {
		in.Logger.Errorw("failed to get latest release info",
			"url", releaseURL,
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "Failed to get latest templates release info")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		in.Logger.Errorw("failed to get release info - bad status",
			"url", releaseURL,
			"status", resp.StatusCode,
		)
		in.Response.ServerErrorMessage(g, fmt.Sprintf("Failed to get release info: HTTP %d", resp.StatusCode))
		return
	}

	// parse release response
	var release struct {
		Assets []struct {
			BrowserDownloadURL string `json:"browser_download_url"`
			Name               string `json:"name"`
		} `json:"assets"`
	}

	releaseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		in.Logger.Errorw("failed to read release response",
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "Failed to read release info")
		return
	}

	if err := json.Unmarshal(releaseBody, &release); err != nil {
		in.Logger.Errorw("failed to parse release response",
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "Failed to parse release info")
		return
	}

	if len(release.Assets) == 0 {
		in.Logger.Error("no assets found in latest release")
		in.Response.ServerErrorMessage(g, "No template assets found in latest release")
		return
	}

	// use the first asset (should be the templates zip)
	templatesURL := release.Assets[0].BrowserDownloadURL
	in.Logger.Infow("downloading templates from latest release",
		"url", templatesURL,
		"asset", release.Assets[0].Name,
	)

	// download the templates
	resp2, err := client.Get(templatesURL)
	if err != nil {
		in.Logger.Errorw("failed to download templates",
			"url", templatesURL,
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "Failed to download templates from GitHub")
		return
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		in.Logger.Errorw("failed to download templates - bad status",
			"url", templatesURL,
			"status", resp2.StatusCode,
		)
		in.Response.ServerErrorMessage(g, fmt.Sprintf("Failed to download templates: HTTP %d", resp2.StatusCode))
		return
	}

	// read the response body
	body, err := io.ReadAll(resp2.Body)
	if err != nil {
		in.Logger.Errorw("failed to read templates response",
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "Failed to read templates download")
		return
	}

	// import the templates from raw bytes (for global use, not company-specific)
	summary, err := in.ImportService.ImportFromBytes(g, session, body, false, nil)
	if err != nil {
		in.Logger.Errorw("failed to import templates",
			"error", err,
		)
		in.Response.ServerErrorMessage(g, "Failed to import templates")
		return
	}

	in.Logger.Infow("successfully installed templates",
		"assetsCreated", summary.AssetsCreated,
		"pagesCreated", summary.PagesCreated,
		"emailsCreated", summary.EmailsCreated,
	)

	in.Response.OK(g, summary)
}

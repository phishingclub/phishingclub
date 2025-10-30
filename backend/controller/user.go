package controller

import (
	"net/http"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var SessionColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.SESSION_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.SESSION_TABLE, "updated_at"),
	"ip_address": repository.TableColumn(database.SESSION_TABLE, "ip_address"),
}

var UserColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.USER_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.USER_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.USER_TABLE, "name"),
	"username":   repository.TableColumn(database.USER_TABLE, "username"),
	"email":      repository.TableColumn(database.USER_TABLE, "email"),
}

// UserLoginRequest is a request for login with username and password
type UserLoginRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	TOTP            string `json:"totp"`
	MFARecoveryCode string `json:"recoveryCode"`
}

// UserSetupTOTPRequest is a request for setting up TOTP
type UserSetupTOTPRequest struct {
	Password string `json:"password"`
}

// UserSetupDisableTOTPRequest is a request for disabling TOTP
type UserDisableTOTPRequest struct {
	Token string `json:"token"`
}

// UserVerifyTOTPRequest is a request for verifying TOTP
type UserVerifyTOTPRequest struct {
	TOTP string `json:"token"`
}

// UserLoginWithMFARecoveryCodeRequest is a request for login with MFA recovery code
type UserLoginWithMFARecoveryCodeRequest struct {
	RecoveryCode string `json:"recoveryCode"`
	Username     string `json:"username"`
	Password     string `json:"password"`
}

// User is the change email controller
type User struct {
	Common
	UserService *service.User
}

// Create creates a new user
func (c *User) Create(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.UserUpsertRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// create user
	newUserID, err := c.UserService.Create(
		g,
		session,
		&req,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"id": newUserID.String(),
		},
	)
}

// GetMaskedAPIKey gets logged-in users masked API key
func (c *User) GetMaskedAPIKey(g *gin.Context) {
	session, user, ok := c.handleSession(g)
	if !ok {
		return
	}
	if user == nil {
		c.handleErrors(g, errors.New("no user in session"))
	}
	// get
	cid := user.ID.MustGet()
	apiKey, err := c.UserService.GetMaskedAPIKey(
		g,
		session,
		&cid,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"apiKey": apiKey,
		},
	)
}

// UpsertAPIKey create/update API key
func (c *User) UpsertAPIKey(g *gin.Context) {
	session, user, ok := c.handleSession(g)
	if !ok {
		return
	}
	if user == nil {
		c.handleErrors(g, errors.New("no user in session"))
	}
	// create user
	uid := user.ID.MustGet()
	apiKey, err := c.UserService.UpsertAPIKey(
		g,
		session,
		&uid,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"apiKey": apiKey,
		},
	)
}

// RemoveAPIKey removes a api key
func (c *User) RemoveAPIKey(g *gin.Context) {
	session, user, ok := c.handleSession(g)
	if !ok {
		return
	}
	if user == nil {
		c.handleErrors(g, errors.New("no user in session"))
	}
	// create user
	uid := user.ID.MustGet()
	err := c.UserService.RemoveAPIKey(
		g,
		session,
		&uid,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{},
	)
}

// UpdateByID updates a user by ID
func (c *User) UpdateByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.User
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// update user
	err := c.UserService.Update(
		g,
		session,
		id,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Delete deletes a user
func (c *User) Delete(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete user
	err := c.UserService.Delete(g, session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// GetAll gets all users using pagination
func (c *User) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(UserColumnsMap)
	// get user
	users, err := c.UserService.GetAll(g, session, &repository.UserOption{
		QueryArgs:   queryArgs,
		WithRole:    true,
		WithCompany: true,
	})
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, users)
}

// GetByID gets a user by ID
func (c *User) GetByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get user
	user, err := c.UserService.GetByID(g, session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, user)
}

// ChangeEmailOnLoggedInUser changes email on logged in user
// this is an administrator action
func (c *User) ChangeEmailOnLoggedInUser(g *gin.Context) {
	session, sessionUser, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse and validate request
	var request model.UserChangeEmailRequest
	if ok := c.handleParseRequest(g, &request); !ok {
		return
	}
	// change email
	userID := sessionUser.ID.MustGet()
	changedEmail, err := c.UserService.ChangeEmailAsAdministrator(
		g,
		session,
		&userID,
		&request.Email,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{"email": changedEmail.String()},
	)
}

// ChangeFullnameOnLoggedInUser is the handler for change fullname
func (c *User) ChangeFullnameOnLoggedInUser(g *gin.Context) {
	session, sessionUser, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.UserChangeFullnameRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// change fullname
	userID := sessionUser.ID.MustGet()
	_, err := c.UserService.ChangeFullname(
		g,
		session,
		&userID,
		&req.NewFullname,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// ChangePasswordOnLoggedInUser changes the password on the logged in user
func (c *User) ChangePasswordOnLoggedInUser(g *gin.Context) {
	session, sessionUser, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.UserChangePasswordRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// change password
	err := c.UserService.ChangePassword(
		g,
		session,
		&req.CurrentPassword,
		&req.NewPassword,
	)
	// handle response
	if errors.Is(err, errs.ErrUserWrongPasword) {
		c.Response.BadRequestMessage(g, "Invalid current password")
		return
	}
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// invalidate all currently running sessions
	userID := sessionUser.ID.MustGet()
	err = c.SessionService.ExpireAllByUserID(g, session, &userID)
	// partial error, the password is changed but the sessions are not invalidated
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		"password changed - all sessions have been invalidated",
	)
}

// ChangeUsernameOnLoggedInUser changes the username
func (c *User) ChangeUsernameOnLoggedInUser(g *gin.Context) {
	session, sessionUser, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.UserChangeUsernameOnLoggedInRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	userID := sessionUser.ID.MustGet()
	// change username
	err := c.UserService.ChangeUsername(
		g.Request.Context(),
		session,
		&userID,
		&req.NewUsername,
	)
	// handle error
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// ExpireSessionByID expires a session by ID
// a administrator can expire any session
// a user can expire their own sessions
func (c *User) ExpireSessionByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	if !isAuthorized {
		c.Response.Forbidden(g)
		return
	}
	err = c.SessionService.Expire(g, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		"session expired",
	)
}

// GetSessionsByUserID gets all sessions by user ID
func (c *User) GetSessionsOnLoggedInUser(g *gin.Context) {
	session, sessionUser, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(SessionColumnsMap)
	userID := sessionUser.ID.MustGet()
	sessions, err := c.SessionService.GetSessionsByUserID(
		g,
		session,
		&userID,
		&repository.SessionOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	data := []map[string]interface{}{}
	for _, sess := range sessions.Rows {
		idStr := sess.ID.String()
		data = append(data, map[string]interface{}{
			"id":        idStr,
			"current":   idStr == session.ID.String(),
			"ip":        sess.IP,
			"createdAt": sess.CreatedAt,
			"updatedAt": sess.UpdatedAt,
		})
	}
	c.Response.OK(
		g,
		gin.H{
			"sessions":    data,
			"hasNextPage": sessions.HasNextPage,
		},
	)
}

// Login logs in a user
func (c *User) Login(g *gin.Context) {
	// parse req
	var req UserLoginRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	user, err := c.UserService.AuthenticateUsernameWithPassword(
		g,
		req.Username,
		req.Password,
		g.ClientIP(),
	)
	if errors.Is(err, errs.ErrUserWrongPasword) {
		c.Response.BadRequestMessage(g, "Invalid password")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Response.BadRequestMessage(g, "Invalid credentials")
		return
	}
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// if the user has MFA enabled then we check the MFA flow
	// if the user has MFA enabled, we must check if there is a
	// valid MFA or a valid recovery code
	userID := user.ID.MustGet()
	MFATokenSupplied := len(req.TOTP) > 0
	MFARecoveryCodeSupplied := len(req.MFARecoveryCode) > 0
	mfaEnabled, err := c.UserService.IsTOTPEnabledByUserID(
		g,
		&userID,
	)
	if errors.Is(err, errs.ErrUserWrongTOTP) {
		c.Response.BadRequestMessage(g, "Invalid TOTP")
		return
	}
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	if mfaEnabled {
		// if tokens or recovery codes are supplied
		// return mfa is required
		if !MFATokenSupplied && !MFARecoveryCodeSupplied {
			c.Response.OK(
				g,
				gin.H{
					"mfa": true,
				},
			)
			return
		}
		// if the client has given both a TOTP and a recovery code
		// we return a bad request
		if MFATokenSupplied && MFARecoveryCodeSupplied {
			c.Response.BadRequestMessage(g, "Cannot supply both MFA token and MFA recovery code")
			return
		}
		// verify the TOTP MFA token
		userID := user.ID.MustGet()
		if MFATokenSupplied && !MFARecoveryCodeSupplied {
			// if MFA is enabled, verify the TOTP
			totpToken, err := vo.NewString64(req.TOTP)
			if err != nil {
				c.Logger.Debugw("failed to create TOTP",
					"error", err,
				)
				c.Response.ValidationFailed(g, "TOTP", err)
				return
			}
			err = c.UserService.CheckTOTP(
				g,
				&userID,
				totpToken,
			)
			if err != nil {
				if errors.Is(err, errs.ErrUserWrongTOTP) {
					c.Response.BadRequestMessage(g, "Invalid TOTP")
					return
				}
				if ok := c.handleErrors(g, err); !ok {
					return
				}
			}
		}
		// if the user has MFA enabled and the client has supplied a recovery code
		// we verify the recovery code
		if !MFATokenSupplied && MFARecoveryCodeSupplied {
			recoveryCode, err := vo.NewString64(req.MFARecoveryCode)
			if err != nil {
				c.Logger.Debugw("failed to create recovery code",
					"error", err,
				)
				c.Response.ValidationFailed(g, "RecoveryCode", err)
				return
			}
			verifiedMFA, err := c.UserService.CheckMFARecoveryCode(
				g,
				&userID,
				recoveryCode,
			)
			if err != nil {
				if errors.Is(err, errs.ErrUserWrongRecoveryCode) {
					c.Response.BadRequestMessage(g, "Invalid recovery code")
					return
				}
				if ok := c.handleErrors(g, err); !ok {
					return
				}
			}
			if !verifiedMFA {
				c.Response.BadRequestMessage(g, "Invalid recovery code")
				return
			}
			// as the recovery code is valid, we can now disable MFA
			err = c.UserService.DisableTOTP(g, &userID)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
		}
	}
	// create a new session
	session, err := c.SessionService.Create(
		g,
		user,
		g.ClientIP(),
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// Set the session in the cookie
	cookie := &http.Cookie{
		Name:     data.SessionCookieKey,
		Value:    session.ID.String(),
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Secure:   true,
		Expires:  *session.MaxAgeAt,
	}
	http.SetCookie(g.Writer, cookie)
	c.Response.OK(g, session)
}

// expireCookieAndStatusOK expires the cookie and returns a 200 OK
func (c *User) expireCookieAndStatusOK(g *gin.Context) {
	g.SetCookie(
		data.SessionCookieKey,
		"",
		-1,
		"/",
		"",
		false,
		true,
	)
	c.logoutOK(g)
}

// logoutOK returns a 200 OK
func (c *User) logoutOK(g *gin.Context) {
	c.Response.OK(
		g,
		gin.H{"message": "logged out"},
	)
}

// Logout logs out the user
// only invalidates the session if the session cookie is
// in the request, this should reduce the risk of CSRF logout
func (c *User) Logout(g *gin.Context) {
	sessionCookie, err := g.Cookie(data.SessionCookieKey)
	if err != nil {
		c.logoutOK(g)
		return
	}
	sessionID, err := uuid.Parse(sessionCookie)
	if err != nil {
		c.logoutOK(g)
		return
	}
	ctx := g.Request.Context()
	err = c.SessionService.Expire(ctx, &sessionID)
	if err != nil {
		c.expireCookieAndStatusOK(g)
		return
	}
	c.expireCookieAndStatusOK(g)
}

// SessionPing pings the session
func (c *User) SessionPing(g *gin.Context) {
	// handle session
	session, sessionUser, ok := c.handleSession(g)
	if !ok {
		return
	}
	c.Logger.Debugw("pinged session for user",
		"userID", sessionUser.ID.MustGet().String(),
	)
	sessionRole := sessionUser.Role
	if sessionRole == nil {
		c.Logger.Error("failed to load role from session user")
		c.Response.ServerError(g)
		return
	}
	sessionCompany := sessionUser.Company
	companyName := ""
	if sessionCompany != nil {
		companyName = sessionCompany.Name.MustGet().String()
	}
	c.Response.OK(
		g,
		gin.H{
			"userID":   sessionUser.ID,
			"username": sessionUser.Username.MustGet().String(),
			"name":     sessionUser.Name.MustGet().String(),
			"role":     sessionRole.Name,
			"company":  companyName,
			"ip":       session.IP,
		},
	)
}

// InvalidateAllSessionByUserID is the nuclear session button for a user
func (c *User) InvalidateAllSessionByUserID(g *gin.Context) {
	session, user, ok := c.handleSession(g)
	if !ok {
		return
	}
	var userID *uuid.UUID
	// parse req
	var req model.InvalidateAllSessionRequest
	err := g.ShouldBindJSON(&req)
	if err != nil {
		if user == nil || !user.ID.IsSpecified() {
			c.Response.BadRequest(g)
			return
		}
		uid := user.ID.MustGet()
		userID = &uid
	} else {
		if req.UserID == nil {
			c.Response.BadRequest(g)
			return
		}
		userID = req.UserID
	}
	// invalidate
	err = c.SessionService.ExpireAllByUserID(g, session, userID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// SetupTOTP generates a new TOTP MFA secrets
func (c *User) SetupTOTP(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var request UserSetupTOTPRequest
	if ok := c.handleParseRequest(g, &request); !ok {
		return
	}
	passwd, err := vo.NewReasonableLengthPassword(request.Password)
	if err != nil {
		c.Logger.Debugw("failed to create password",
			"error", err,
		)
		c.Response.ValidationFailed(g, "Password", err)
		return
	}
	// get and save TOTP for user
	totpValues, err := c.UserService.SetupTOTP(
		g.Request.Context(),
		session,
		passwd,
	)
	// handle response
	if errors.Is(err, errs.ErrAuthenticationFailed) {
		c.Response.BadRequestMessage(g, "Incorrect password")
		return
	}
	if ok := handleServerError(g, c.Response, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"base32":       totpValues.Secret,
			"url":          totpValues.URL,
			"recoveryCode": totpValues.RecoveryCode,
		},
	)
}

// SetupVerifyTOTP verifies a TOTP
func (c *User) SetupVerifyTOTP(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req UserVerifyTOTPRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	totp, err := vo.NewString64(req.TOTP)
	if err != nil {
		c.Logger.Debugw("failed to create TOTP",
			"error", err,
		)
		c.Response.ValidationFailed(g, "TOTP", err)
		return
	}
	// verify TOTP
	err = c.UserService.SetupCheckTOTP(
		g.Request.Context(),
		session,
		totp,
	)
	if errors.Is(err, errs.ErrUserWrongTOTP) {
		c.Response.BadRequestMessage(g, "Invalid token")
		return
	}

	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		"TOTP verified",
	)
}

// IsTOTPEnabled checks if TOTP is enabled
func (c *User) IsTOTPEnabled(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// check if TOTP is enabled
	isEnabled, err := c.UserService.IsTOTPEnabled(
		g.Request.Context(),
		session,
	)
	// handle response
	if ok := handleServerError(g, c.Response, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{"enabled": isEnabled},
	)
}

// DisableTOTP disables TOTP
func (c *User) DisableTOTP(g *gin.Context) {
	_, user, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var request UserDisableTOTPRequest
	if ok := c.handleParseRequest(g, &request); !ok {
		return
	}
	token, err := vo.NewString64(request.Token)
	if err != nil {
		c.Logger.Debugw("failed to create token",
			"error", err,
		)
		c.Response.ValidationFailed(g, "Token", err)
		return
	}
	// check TOTP
	userID := user.ID.MustGet()
	err = c.UserService.CheckTOTP(
		g.Request.Context(),
		&userID,
		token,
	)
	if err != nil {
		if errors.Is(err, errs.ErrUserWrongTOTP) {
			c.Response.BadRequestMessage(g, "Invalid token")
			return
		}
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	// disable TOTP
	err = c.UserService.DisableTOTP(
		g.Request.Context(),
		&userID,
	)
	// handle response
	if err != nil {
		if errors.Is(err, errs.ErrUserWrongTOTP) {
			c.Response.BadRequestMessage(g, "Invalid token")
			return
		}
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	c.Response.OK(
		g,
		"TOTP disabled",
	)
}

// VerifyTOTP verifies a TOTP
func (c *User) VerifyTOTP(g *gin.Context) {
	_, user, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req UserVerifyTOTPRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	totp, err := vo.NewString64(req.TOTP)
	if err != nil {
		c.Logger.Debugw("failed to create TOTP",
			"error", err,
		)
		c.Response.ValidationFailed(g, "TOTP", err)
		return
	}
	// verify TOTP
	userID := user.ID.MustGet()
	err = c.UserService.CheckTOTP(
		g.Request.Context(),
		&userID,
		totp,
	)
	if errors.Is(err, errs.ErrUserWrongTOTP) {
		c.Response.BadRequestMessage(g, "Invalid token")
		return
	}
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		"TOTP verified",
	)
}

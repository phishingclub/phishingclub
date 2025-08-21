package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/gin-gonic/gin"
	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/sso"
	"github.com/phishingclub/phishingclub/vo"
)

type SSO struct {
	Common
	OptionsService *Option
	UserService    *User
	SessionService *Session
	MSALClient     *confidential.Client
}

type MsGraphUserInfo struct {
	DisplayName       string `json:"displayName"`       // Full name
	Email             string `json:"mail"`              // Primary email
	UserPrincipalName string `json:"userPrincipalName"` // Often email or login
	GivenName         string `json:"givenName"`         // First name
	Surname           string `json:"surname"`           // Last name
	ID                string `json:"id"`                // Unique Azure AD ID
}

// Get is the auth protected method for getting SSO details
func (s *SSO) Get(
	ctx context.Context,
	session *model.Session,
) (*model.SSOOption, error) {
	ae := NewAuditEvent("SSO.Get", session)
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
	return s.GetSSOOptionWithoutAuth(ctx)
}

// Upsert upserts SSO config it also replaces the in memory SSO configuration
func (s *SSO) Upsert(
	ctx context.Context,
	session *model.Session,
	ssoOpt *model.SSOOption,
) error {
	ae := NewAuditEvent("SSO.Upsert", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	ssoOpt.Enabled = len(ssoOpt.ClientID.String()) > 0 &&
		len(ssoOpt.TenantID.String()) > 0 &&
		len(ssoOpt.ClientSecret.String()) > 0

	// if the config is incomplete, we clear it
	if !ssoOpt.Enabled {
		ssoOpt.ClientID = *vo.NewEmptyOptionalString64()
		ssoOpt.TenantID = *vo.NewEmptyOptionalString64()
		ssoOpt.ClientSecret = *vo.NewEmptyOptionalString1024()
		ssoOpt.RedirectURL = *vo.NewEmptyOptionalString1024()
	}
	opt, err := ssoOpt.ToOption()
	if err != nil {
		return errs.Wrap(err)
	}
	err = s.OptionsService.SetOptionByKey(ctx, session, opt)
	if err != nil {
		s.Logger.Errorw("failed to upsert sso option", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	// replace the in memory msal client
	if ssoOpt.Enabled {
		s.MSALClient, err = sso.NewEntreIDClient(ssoOpt)
		if err != nil {
			return errs.Wrap(err)
		}
	} else {
		s.MSALClient = nil
	}

	return nil
}

func (s *SSO) GetSSOOptionWithoutAuth(ctx context.Context) (*model.SSOOption, error) {
	opt, err := s.OptionsService.GetOptionWithoutAuth(ctx, data.OptionKeyAdminSSOLogin)
	if err != nil {
		s.Logger.Errorw("failed to get sso option",
			"key", data.OptionKeyAdminSSOLogin,
			"error", err)
		return nil, errs.Wrap(err)
	}
	ssoOpt, err := model.NewSSOOptionFromJSON([]byte(opt.Value.String()))
	if err != nil {
		s.Logger.Errorw("failed to unmarshall sso option", "error", err)
		return nil, errs.Wrap(err)
	}
	return ssoOpt, nil
}

func (s *SSO) EntreIDLogin(ctx context.Context) (string, error) {
	// check if sso is enabled
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return "", err
	}
	if !ssoOpt.Enabled {
		s.Logger.Debugf("SSO login URL visited but it is disabed")
		return "", errs.Wrap(errs.ErrSSODisabled)
	}
	// the MSALCLient is set on application start up
	// and when a upsert is done, replacing the old client with new details
	if s.MSALClient == nil {
		return "", errs.Wrap(errors.New("no MSAL client"))
	}
	authURL, err := s.MSALClient.AuthCodeURL(
		ctx,
		ssoOpt.ClientID.String(),
		ssoOpt.RedirectURL.String(),
		[]string{"https://graph.microsoft.com/User.Read"},
	)
	if err != nil {
		return "", errs.Wrap(err)
	}
	return authURL, nil
}

// EntreIDCallBack checks if the callback is OK then requests user details from the graph API
func (s *SSO) HandlEntraIDCallback(
	g *gin.Context,
	code string,
) (*model.Session, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(g)
	if err != nil {
		return nil, err
	}
	if !ssoOpt.Enabled {
		return nil, errs.Wrap(errs.ErrSSODisabled)
	}
	if s.MSALClient == nil {
		return nil, errors.New("no msal client in memory")
	}
	result, err := s.MSALClient.AcquireTokenByAuthCode(
		context.Background(),
		code,
		ssoOpt.RedirectURL.String(),
		[]string{"User.Read"},
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	userInfo, err := s.getMsGraphMe(g, result.AccessToken)
	if err != nil {
		s.Logger.Debugw("failed to get /me graph info", "error", err)
		return nil, err
	}
	// validate required fields
	if userInfo.Email == "" && userInfo.UserPrincipalName == "" {
		err := errors.New("no email provided from SSO")
		s.Logger.Debugw("no email or userPrincipalName from SSO", "error", err)
		return nil, errs.Wrap(err)
	}
	// determine email (prefer mail over UPN)
	email := userInfo.Email
	if email == "" {
		email = userInfo.UserPrincipalName
	}
	// determine name
	name := userInfo.DisplayName
	if name == "" {
		name = strings.TrimSpace(fmt.Sprintf("%s %s", userInfo.GivenName, userInfo.Surname))
	}
	if name == "" {
		// Fallback to email prefix if no name available
		name = strings.Split(email, "@")[0]
	}
	userID, err := s.UserService.CreateFromSSO(g, name, email, userInfo.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if userID == nil {
		return nil, errs.Wrap(errors.New("user ID is unexpectedly nil"))
	}
	// get the user and create a session
	user, err := s.UserService.GetByIDWithoutAuth(g, userID)
	if err != nil {
		s.Logger.Debugf("failed to get SSO user", "error", err)
		return nil, errs.Wrap(err)
	}
	session, err := s.SessionService.Create(g, user, g.ClientIP())
	if err != nil {
		s.Logger.Debugf("failed to create session from SSO", "error", err)
		return nil, errs.Wrap(err)
	}
	return session, nil
}

func (s *SSO) getMsGraphMe(ctx context.Context, accessToken string) (*MsGraphUserInfo, error) {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://graph.microsoft.com/v1.0/me", nil)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := client.Do(req)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errs.Wrap(fmt.Errorf("graph API returned status %d", resp.StatusCode))
	}

	// Read and log raw response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	s.Logger.Debugw("Raw Microsoft Graph response", "body", string(body))

	var userInfo MsGraphUserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, errs.Wrap(err)
	}

	s.Logger.Debugw("Parsed user info",
		"id", userInfo.ID,
		"email", userInfo.Email,
		"displayName", userInfo.DisplayName,
		"userPrincipalName", userInfo.UserPrincipalName,
	)

	return &userInfo, nil
}

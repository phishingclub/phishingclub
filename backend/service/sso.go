package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	OIDCClient     *sso.OIDCClient
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
	switch ssoOpt.Provider() {
	case data.SSOProviderOIDC:
		// PKCE is always used, so a public client without a secret is valid
		ssoOpt.Enabled = len(ssoOpt.IssuerURL.String()) > 0 &&
			len(ssoOpt.ClientID.String()) > 0
	default:
		ssoOpt.Enabled = len(ssoOpt.ClientID.String()) > 0 &&
			len(ssoOpt.TenantID.String()) > 0 &&
			len(ssoOpt.ClientSecret.String()) > 0
	}

	// if the config is incomplete, we clear it
	if !ssoOpt.Enabled {
		ssoOpt.ClientID = *vo.NewEmptyOptionalString64()
		ssoOpt.TenantID = *vo.NewEmptyOptionalString64()
		ssoOpt.ClientSecret = *vo.NewEmptyOptionalString1024()
		ssoOpt.RedirectURL = *vo.NewEmptyOptionalString1024()
		ssoOpt.IssuerURL = *vo.NewEmptyOptionalString1024()
		ssoOpt.Scopes = *vo.NewEmptyOptionalString1024()
		ssoOpt.ACRValues = *vo.NewEmptyOptionalString1024()
		// never leave exclusive login on without a working SSO, it would lock
		// everyone out of the local login as well
		ssoOpt.ExclusiveLogin = false
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
	// replace the in memory clients for both providers
	s.MSALClient = nil
	s.OIDCClient = nil
	if ssoOpt.Enabled {
		switch ssoOpt.Provider() {
		case data.SSOProviderOIDC:
			s.OIDCClient, err = sso.NewOIDCClient(ctx, ssoOpt)
			if err != nil {
				return errs.Wrap(err)
			}
		default:
			s.MSALClient, err = sso.NewEntreIDClient(ssoOpt)
			if err != nil {
				return errs.Wrap(err)
			}
		}
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
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
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

// SSOLoginStatus is returned to the login page so it can render the correct
// provider button and hide local login when exclusive SSO is on.
type SSOLoginStatus struct {
	Enabled        bool   `json:"enabled"`
	ProviderType   string `json:"providerType"`
	ExclusiveLogin bool   `json:"exclusiveLogin"`
}

// LoginStatus reports whether SSO is active, which provider is configured and
// whether local login is exclusive.
func (s *SSO) LoginStatus(ctx context.Context) (*SSOLoginStatus, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	enabled := s.MSALClient != nil || s.OIDCClient != nil
	return &SSOLoginStatus{
		Enabled:        enabled,
		ProviderType:   ssoOpt.Provider(),
		ExclusiveLogin: enabled && ssoOpt.ExclusiveLogin,
	}, nil
}

// IsExclusiveLoginEnabled reports whether local username and password login is
// disabled. It is only true when SSO is actually active in memory, so a broken
// or unconfigured SSO never locks out local login.
func (s *SSO) IsExclusiveLoginEnabled(ctx context.Context) (bool, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return false, errs.Wrap(err)
	}
	enabled := s.MSALClient != nil || s.OIDCClient != nil
	return enabled && ssoOpt.ExclusiveLogin, nil
}

// IsLocalLoginBlocked reports whether a username and password login must be
// refused because exclusive SSO is enabled. The breakglass argument comes from
// the server config and keeps local login available for recovery. A blocked
// attempt is audited.
func (s *SSO) IsLocalLoginBlocked(ctx context.Context, breakglass bool, ip string) (bool, error) {
	exclusive, err := s.IsExclusiveLoginEnabled(ctx)
	if err != nil {
		return false, errs.Wrap(err)
	}
	if !exclusive || breakglass {
		return false, nil
	}
	ae := NewAuditEvent("User.LocalLoginDenied", nil)
	ae.Details["ip"] = ip
	s.AuditLogNotAuthorized(ae)
	return true, nil
}

// OIDCAuthCodeURL builds the OIDC authorization request URL. The caller supplies
// the state, nonce and PKCE verifier so it can store them for the callback.
func (s *SSO) OIDCAuthCodeURL(
	ctx context.Context,
	state string,
	nonce string,
	verifier string,
) (string, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return "", err
	}
	if !ssoOpt.Enabled || ssoOpt.Provider() != data.SSOProviderOIDC {
		return "", errs.Wrap(errs.ErrSSODisabled)
	}
	if s.OIDCClient == nil {
		return "", errs.Wrap(errors.New("no OIDC client"))
	}
	return s.OIDCClient.AuthCodeURL(state, nonce, verifier), nil
}

// HandleOIDCCallback verifies the OIDC callback, resolves the pre provisioned
// user by their verified email and creates a session.
func (s *SSO) HandleOIDCCallback(
	g *gin.Context,
	code string,
	nonce string,
	verifier string,
) (*model.Session, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(g)
	if err != nil {
		return nil, err
	}
	if !ssoOpt.Enabled || ssoOpt.Provider() != data.SSOProviderOIDC {
		return nil, errs.Wrap(errs.ErrSSODisabled)
	}
	if s.OIDCClient == nil {
		return nil, errors.New("no OIDC client in memory")
	}
	userInfo, err := s.OIDCClient.Exchange(g, code, nonce, verifier)
	if err != nil {
		s.Logger.Debugw("OIDC code exchange failed", "error", err)
		return nil, err
	}
	name := userInfo.Name
	if name == "" {
		name = strings.Split(userInfo.Email, "@")[0]
	}
	userID, err := s.UserService.CreateFromSSO(g, name, userInfo.Email, userInfo.Subject)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if userID == nil {
		return nil, errs.Wrap(errors.New("user ID is unexpectedly nil"))
	}
	user, err := s.UserService.GetByIDWithoutAuth(g, userID)
	if err != nil {
		s.Logger.Debugw("failed to get SSO user", "error", err)
		return nil, errs.Wrap(err)
	}
	session, err := s.SessionService.Create(g, user, g.ClientIP())
	if err != nil {
		s.Logger.Debugw("failed to create session from SSO", "error", err)
		return nil, errs.Wrap(err)
	}
	return session, nil
}

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
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/sso"
	"github.com/phishingclub/phishingclub/vo"
)

// SSO is the service responsible for all single-sign-on operations.
// It supports two authentication paths:
//
//  1. Legacy Microsoft Entra ID (MSAL) — activated when the stored
//     SSOOption has a non-empty TenantID.
//  2. Generic OIDC — activated when the stored SSOOption has a non-empty
//     IssuerURL and an empty TenantID.
type SSO struct {
	Common
	OptionsService *Option
	UserService    *User
	SessionService *Session
	SSOStateRepo   *repository.SSOState
	MSALClient     *confidential.Client
	OIDCClient     *sso.OIDCClient
}

// MsGraphUserInfo is the subset of Microsoft Graph /me fields we care about.
type MsGraphUserInfo struct {
	DisplayName       string `json:"displayName"`
	Email             string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	GivenName         string `json:"givenName"`
	Surname           string `json:"surname"`
	ID                string `json:"id"`
}

// --- read helpers ---

// Get is the auth-protected method for fetching SSO details.
func (s *SSO) Get(
	ctx context.Context,
	session *model.Session,
) (*model.SSOOption, error) {
	ae := NewAuditEvent("SSO.Get", session)
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

// GetSSOOptionWithoutAuth reads the SSOOption from the options store without
// requiring a session.  Used on startup and in public login-flow handlers.
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

// --- write helper ---

// Upsert persists the SSO configuration and hot-swaps the in-memory clients.
func (s *SSO) Upsert(
	ctx context.Context,
	session *model.Session,
	ssoOpt *model.SSOOption,
) error {
	ae := NewAuditEvent("SSO.Upsert", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// determine whether the supplied config is complete enough to enable SSO
	hasLegacyFields := ssoOpt.ClientID.String() != "" &&
		ssoOpt.TenantID.String() != "" &&
		ssoOpt.ClientSecret.String() != ""
	hasOIDCFields := ssoOpt.ClientID.String() != "" &&
		ssoOpt.IssuerURL.String() != "" &&
		ssoOpt.ClientSecret.String() != "" &&
		ssoOpt.RedirectURL.String() != ""

	ssoOpt.Enabled = hasLegacyFields || hasOIDCFields

	// clear everything when the config is incomplete
	if !ssoOpt.Enabled {
		ssoOpt.ClientID = *vo.NewEmptyOptionalString64()
		ssoOpt.TenantID = *vo.NewEmptyOptionalString64()
		ssoOpt.ClientSecret = *vo.NewEmptyOptionalString1024()
		ssoOpt.RedirectURL = *vo.NewEmptyOptionalString1024()
		ssoOpt.IssuerURL = *vo.NewEmptyOptionalString1024()
		ssoOpt.RequiredRoleClaim = *vo.NewEmptyOptionalString255()
		ssoOpt.RequiredRoleValue = *vo.NewEmptyOptionalString255()
		ssoOpt.ACRValues = *vo.NewEmptyOptionalString255()
		ssoOpt.SSOOnly = false
	}

	opt, err := ssoOpt.ToOption()
	if err != nil {
		return errs.Wrap(err)
	}
	if err = s.OptionsService.SetOptionByKey(ctx, session, opt); err != nil {
		s.Logger.Errorw("failed to upsert sso option", "error", err)
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)

	// hot-swap in-memory clients
	s.MSALClient = nil
	s.OIDCClient = nil
	if ssoOpt.Enabled {
		if ssoOpt.IsLegacyEntraID() {
			s.MSALClient, err = sso.NewEntreIDClient(ssoOpt)
			if err != nil {
				return errs.Wrap(err)
			}
		} else if ssoOpt.IsOIDC() {
			s.OIDCClient, err = sso.NewOIDCClient(
				ctx,
				ssoOpt.IssuerURL.String(),
				ssoOpt.ClientID.String(),
				ssoOpt.ClientSecret.String(),
				ssoOpt.RedirectURL.String(),
				[]string{"openid", "profile", "email"},
			)
			if err != nil {
				return errs.Wrap(err)
			}
		}
	}
	return nil
}

// --- status helpers ---

// IsEnabled returns true when either the legacy MSAL client or the generic
// OIDC client is initialised.
func (s *SSO) IsEnabled() bool {
	return s.MSALClient != nil || s.OIDCClient != nil
}

// IsLegacyEnabled returns true when only the MSAL client is active.
func (s *SSO) IsLegacyEnabled() bool {
	return s.MSALClient != nil
}

// IsOIDCEnabled returns true when the generic OIDC client is active.
func (s *SSO) IsOIDCEnabled() bool {
	return s.OIDCClient != nil
}

// IsSSOOnly reports whether local-password login has been disabled.
func (s *SSO) IsSSOOnly(ctx context.Context) (bool, error) {
	opt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return false, err
	}
	return opt.SSOOnly && opt.Enabled, nil
}

// --- legacy Entra ID ---

// EntreIDLogin returns the Microsoft authorization URL for the legacy MSAL
// flow.  Returns errs.ErrSSODisabled when the MSAL client is not configured.
func (s *SSO) EntreIDLogin(ctx context.Context) (string, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return "", err
	}
	if !ssoOpt.Enabled {
		s.Logger.Debugf("SSO login URL visited but it is disabled")
		return "", errs.Wrap(errs.ErrSSODisabled)
	}
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

// HandlEntraIDCallback completes the legacy Entra ID OAuth2 code exchange,
// fetches the user from MS Graph and creates (or reuses) a local account.
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
	if userInfo.Email == "" && userInfo.UserPrincipalName == "" {
		err := errors.New("no email provided from SSO")
		s.Logger.Debugw("no email or userPrincipalName from SSO", "error", err)
		return nil, errs.Wrap(err)
	}
	email := userInfo.Email
	if email == "" {
		email = userInfo.UserPrincipalName
	}
	name := userInfo.DisplayName
	if name == "" {
		name = strings.TrimSpace(fmt.Sprintf("%s %s", userInfo.GivenName, userInfo.Surname))
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	userID, err := s.UserService.CreateFromSSO(g, name, email, userInfo.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if userID == nil {
		return nil, errs.Wrap(errors.New("user ID is unexpectedly nil"))
	}
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
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me", nil)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	s.Logger.Debugw("raw Microsoft Graph response", "body", string(body))

	var userInfo MsGraphUserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, errs.Wrap(err)
	}
	s.Logger.Debugw("parsed user info",
		"id", userInfo.ID,
		"email", userInfo.Email,
		"displayName", userInfo.DisplayName,
		"userPrincipalName", userInfo.UserPrincipalName,
	)
	return &userInfo, nil
}

// --- generic OIDC ---

// OIDCLoginURL generates the authorization URL for the generic OIDC flow.
// It creates a PKCE pair, a state token and a nonce, persists them in
// sso_states and returns the authorization URL.
func (s *SSO) OIDCLoginURL(ctx context.Context) (string, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return "", err
	}
	if !ssoOpt.Enabled || !ssoOpt.IsOIDC() {
		return "", errs.Wrap(errs.ErrSSODisabled)
	}
	if s.OIDCClient == nil {
		return "", errs.Wrap(errors.New("OIDC client not initialised"))
	}

	// generate PKCE pair
	codeVerifier, codeChallenge, err := sso.GeneratePKCEPair()
	if err != nil {
		return "", errs.Wrap(err)
	}

	// generate CSRF state token and nonce
	state, err := sso.GenerateStateToken()
	if err != nil {
		return "", errs.Wrap(err)
	}
	nonce, err := sso.GenerateStateToken()
	if err != nil {
		return "", errs.Wrap(err)
	}

	// persist state so the callback can verify and retrieve the verifier
	expiry := time.Now().Add(sso.SSOStateExpiry)
	if _, err = s.SSOStateRepo.Insert(ctx, state, codeVerifier, nonce, &expiry); err != nil {
		return "", errs.Wrap(err)
	}

	authURL := s.OIDCClient.AuthCodeURL(state, nonce, codeChallenge, ssoOpt.ACRValues.String())
	return authURL, nil
}

// HandleOIDCCallback completes the generic OIDC authorisation code flow with
// PKCE, enforces role and ACR requirements, then creates or reuses a local
// user account and returns a new session.
func (s *SSO) HandleOIDCCallback(
	ctx context.Context,
	code string,
	stateToken string,
	clientIP string,
) (*model.Session, error) {
	ssoOpt, err := s.GetSSOOptionWithoutAuth(ctx)
	if err != nil {
		return nil, err
	}
	if !ssoOpt.Enabled || !ssoOpt.IsOIDC() {
		return nil, errs.Wrap(errs.ErrSSODisabled)
	}
	if s.OIDCClient == nil {
		return nil, errs.Wrap(errors.New("OIDC client not initialised"))
	}

	// look up and validate the state record
	stateRecord, err := s.SSOStateRepo.GetByStateToken(ctx, stateToken)
	if err != nil {
		s.Logger.Warnw("oidc callback: state token not found or expired", "error", err)
		return nil, errs.Wrap(errors.New("invalid or expired state token"))
	}

	// mark it consumed immediately to prevent replay
	if err = s.SSOStateRepo.MarkAsUsed(ctx, stateRecord.ID); err != nil {
		return nil, errs.Wrap(err)
	}

	// exchange code → tokens (includes id_token signature + nonce verification)
	result, err := s.OIDCClient.ExchangeCode(ctx, code, stateRecord.CodeVerifier, stateRecord.Nonce)
	if err != nil {
		s.Logger.Warnw("oidc token exchange failed", "error", err)
		return nil, errs.Wrap(err)
	}

	// --- ACR verification ---
	if ssoOpt.HasACR() {
		rawClaims := result.RawClaims
		acrClaim, _ := rawClaims["acr"].(string)
		requiredACR := strings.TrimSpace(ssoOpt.ACRValues.String())
		// the ACR values field may contain multiple space-separated values;
		// we require that the returned acr matches at least one of them
		matched := false
		for _, v := range strings.Fields(requiredACR) {
			if acrClaim == v {
				matched = true
				break
			}
		}
		if !matched {
			s.Logger.Warnw("oidc acr mismatch",
				"required", requiredACR,
				"got", acrClaim,
			)
			return nil, errs.Wrap(errors.New("ACR requirement not satisfied"))
		}
	}

	// --- role gating ---
	if ssoOpt.HasRoleGating() {
		if !sso.CheckRoleClaim(
			result.RawClaims,
			ssoOpt.RequiredRoleClaim.String(),
			ssoOpt.RequiredRoleValue.String(),
		) {
			s.Logger.Warnw("oidc login denied: required role not present",
				"claim", ssoOpt.RequiredRoleClaim.String(),
				"required", ssoOpt.RequiredRoleValue.String(),
			)
			return nil, errs.Wrap(errs.ErrAuthorizationFailed)
		}
	}

	// --- fetch user identity ---
	sub, email, name, err := s.OIDCClient.UserInfoEmail(ctx, result.AccessToken)
	if err != nil {
		// fall back to id_token claims on userinfo failure
		s.Logger.Warnw("oidc userinfo failed, falling back to id_token claims", "error", err)
		sub = result.IDToken.Subject
		if v, ok := result.RawClaims["email"].(string); ok {
			email = v
		}
		if v, ok := result.RawClaims["name"].(string); ok {
			name = v
		}
	}

	if email == "" {
		// last resort: use the subject as a pseudo-email
		s.Logger.Warnw("oidc provider did not return an email address, using sub", "sub", sub)
		email = sub
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	userID, err := s.UserService.CreateFromSSO(ctx, name, email, sub)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if userID == nil {
		return nil, errs.Wrap(errors.New("user ID is unexpectedly nil after CreateFromSSO"))
	}

	user, err := s.UserService.GetByIDWithoutAuth(ctx, userID)
	if err != nil {
		s.Logger.Errorw("failed to get OIDC user after create", "error", err)
		return nil, errs.Wrap(err)
	}

	session, err := s.SessionService.Create(ctx, user, clientIP)
	if err != nil {
		s.Logger.Errorw("failed to create session from OIDC login", "error", err)
		return nil, errs.Wrap(err)
	}
	return session, nil
}

// CleanupExpiredSSOStates removes expired PKCE/state records from the store.
// It is safe to call at any time; errors are logged but not propagated.
func (s *SSO) CleanupExpiredSSOStates(ctx context.Context) {
	if err := s.SSOStateRepo.DeleteExpired(ctx); err != nil {
		s.Logger.Warnw("failed to clean up expired sso states", "error", err)
	}
}

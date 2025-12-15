package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/random"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

// OAuthProvider service handles oauth provider operations
type OAuthProvider struct {
	Common
	OAuthProviderRepository *repository.OAuthProvider
	OAuthStateRepository    *repository.OAuthState

	// refreshGroup ensures only one token refresh happens per provider at a time
	// even if multiple goroutines request simultaneous token refreshes
	refreshGroup singleflight.Group
}

// TokenResponse represents the response from oauth token endpoints
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// Create creates a new oauth provider
func (o *OAuthProvider) Create(
	ctx context.Context,
	session *model.Session,
	provider *model.OAuthProvider,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("OAuthProvider.Create", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	// validate
	if err := provider.Validate(); err != nil {
		o.Logger.Errorw("failed to validate oauth provider", "error", err)
		return nil, errs.Wrap(err)
	}

	var companyID *uuid.UUID
	if cid, err := provider.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	// check uniqueness
	name := provider.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		o.OAuthProviderRepository.DB,
		"oauth_providers",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		o.Logger.Errorw("failed to check oauth provider uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		o.Logger.Debugw("oauth provider name is already used", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}

	// set initial authorization state
	provider.IsAuthorized = nullable.NewNullableWithValue(false)

	// save
	id, err := o.OAuthProviderRepository.Insert(ctx, provider)
	if err != nil {
		o.Logger.Errorw("failed to insert oauth provider", "error", err)
		return nil, errs.Wrap(err)
	}

	ae.Details["id"] = id.String()
	o.AuditLogAuthorized(ae)

	return id, nil
}

// GetAll gets all oauth providers with pagination
func (o *OAuthProvider) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	option repository.OAuthProviderOption,
) (*model.Result[model.OAuthProvider], error) {
	ae := NewAuditEvent("OAuthProvider.GetAll", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	// get all oauth providers
	result, err := o.OAuthProviderRepository.GetAll(ctx, companyID, &option)
	if err != nil {
		o.Logger.Errorw("failed to get all oauth providers", "error", err)
		return nil, errs.Wrap(err)
	}

	// clear sensitive fields before returning
	for i := range result.Rows {
		result.Rows[i].ClientSecret = nullable.NewNullNullable[vo.OptionalString255]()
	}

	return result, nil
}

// GetByID gets an oauth provider by id
func (o *OAuthProvider) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.OAuthProvider, error) {
	ae := NewAuditEvent("OAuthProvider.GetByID", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	provider, err := o.OAuthProviderRepository.GetByID(ctx, *id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(err)
		}
		o.Logger.Errorw("failed to get oauth provider by id", "error", err)
		return nil, errs.Wrap(err)
	}

	// clear sensitive fields
	provider.ClientSecret = nullable.NewNullNullable[vo.OptionalString255]()

	return provider, nil
}

// UpdateByID updates an oauth provider by id
func (o *OAuthProvider) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	provider *model.OAuthProvider,
) error {
	ae := NewAuditEvent("OAuthProvider.UpdateByID", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// validate
	if err := provider.Validate(); err != nil {
		o.Logger.Errorw("failed to validate oauth provider", "error", err)
		return errs.Wrap(err)
	}

	// get existing provider
	existing, err := o.OAuthProviderRepository.GetByID(ctx, *id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(err)
		}
		return errs.Wrap(err)
	}

	// for imported providers, only allow name updates
	if existing.IsImported.MustGet() {
		// clear all fields except name and id
		provider.AuthURL = nullable.NewNullNullable[vo.String512]()
		provider.TokenURL = nullable.NewNullNullable[vo.String512]()
		provider.Scopes = nullable.NewNullNullable[vo.String2048]()
		provider.ClientID = nullable.NewNullNullable[vo.String255]()
		provider.ClientSecret = nullable.NewNullNullable[vo.OptionalString255]()
		provider.AccessToken = nullable.NewNullNullable[vo.OptionalString1MB]()
		provider.RefreshToken = nullable.NewNullNullable[vo.OptionalString1MB]()
		provider.IsAuthorized = nullable.NewNullNullable[bool]()
		provider.IsImported = nullable.NewNullNullable[bool]()
	}

	var companyID *uuid.UUID
	if cid, err := existing.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	// check uniqueness
	name := provider.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		o.OAuthProviderRepository.DB,
		"oauth_providers",
		name.String(),
		companyID,
		id,
	)
	if err != nil {
		o.Logger.Errorw("failed to check oauth provider uniqueness", "error", err)
		return errs.Wrap(err)
	}
	if !isOK {
		o.Logger.Debugw("oauth provider name is already used", "name", name.String())
		return validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}

	// if client secret is being updated with a non-empty value, invalidate authorization
	if provider.ClientSecret.IsSpecified() && !provider.ClientSecret.IsNull() {
		if secret, err := provider.ClientSecret.Get(); err == nil && secret.String() != "" {
			provider.IsAuthorized = nullable.NewNullableWithValue(false)
		}
	}

	// update
	if err := o.OAuthProviderRepository.UpdateByID(ctx, *id, provider); err != nil {
		o.Logger.Errorw("failed to update oauth provider", "error", err)
		return errs.Wrap(err)
	}

	ae.Details["id"] = id.String()
	o.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes an oauth provider by id
func (o *OAuthProvider) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("OAuthProvider.DeleteByID", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// check if provider exists
	_, err = o.OAuthProviderRepository.GetByID(ctx, *id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(err)
		}
		return errs.Wrap(err)
	}

	// delete
	if err := o.OAuthProviderRepository.DeleteByID(ctx, *id); err != nil {
		o.Logger.Errorw("failed to delete oauth provider", "error", err)
		return errs.Wrap(err)
	}

	ae.Details["id"] = id.String()
	o.AuditLogAuthorized(ae)

	return nil
}

// RemoveAuthorization removes authorization tokens from an oauth provider
func (o *OAuthProvider) RemoveAuthorization(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("OAuthProvider.RemoveAuthorization", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// check if provider exists
	provider, err := o.OAuthProviderRepository.GetByID(ctx, *id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(err)
		}
		return errs.Wrap(err)
	}

	// remove authorization
	if err := o.OAuthProviderRepository.RemoveAuthorization(ctx, *id); err != nil {
		o.Logger.Errorw("failed to remove authorization from oauth provider", "error", err)
		return errs.Wrap(err)
	}

	name, _ := provider.Name.Get()
	ae.Details["id"] = id.String()
	ae.Details["name"] = name.String()
	o.AuditLogAuthorized(ae)

	return nil
}

// GetAuthorizationURL creates the oauth authorization url for the user to visit
func (o *OAuthProvider) GetAuthorizationURL(
	ctx context.Context,
	session *model.Session,
	providerID *uuid.UUID,
	redirectURI string,
) (string, error) {
	ae := NewAuditEvent("OAuthProvider.GetAuthorizationURL", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}

	// get provider
	provider, err := o.OAuthProviderRepository.GetByID(ctx, *providerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errs.Wrap(err)
		}
		return "", errs.Wrap(err)
	}

	// prevent authorization on imported providers
	if provider.IsImported.MustGet() {
		return "", errors.New("cannot authorize imported providers - they use pre-authorized tokens")
	}

	// generate cryptographically random state token (32 bytes base64-encoded)
	stateToken, err := random.GenerateRandomURLBase64Encoded(32)
	if err != nil {
		o.Logger.Errorw("failed to generate state token", "error", err)
		return "", errs.Wrap(err)
	}

	// store state token (expires in 10 minutes)
	expiresAt := time.Now().Add(10 * time.Minute)

	// create state token vo
	stateTokenVO, err := vo.NewString255(stateToken)
	if err != nil {
		o.Logger.Errorw("failed to create state token vo", "error", err)
		return "", errs.Wrap(err)
	}

	oauthState := &model.OAuthState{
		StateToken:      nullable.NewNullableWithValue(*stateTokenVO),
		OAuthProviderID: nullable.NewNullableWithValue(*providerID),
		ExpiresAt:       &expiresAt,
	}

	_, err = o.OAuthStateRepository.Insert(ctx, oauthState)
	if err != nil {
		o.Logger.Errorw("failed to store oauth state token", "error", err)
		return "", errs.Wrap(err)
	}

	// build authorization url
	authURL := provider.AuthURL.MustGet()
	clientID := provider.ClientID.MustGet()
	scopes := provider.Scopes.MustGet()

	params := url.Values{
		"client_id":     {clientID.String()},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {scopes.String()},
		"state":         {stateToken},
		"access_type":   {"offline"}, // request refresh token
		"prompt":        {"consent"}, // force consent to get refresh token
	}

	authorizationURL := authURL.String() + "?" + params.Encode()

	o.AuditLogAuthorized(ae)

	return authorizationURL, nil
}

// ExchangeCodeForTokens exchanges authorization code for access and refresh tokens
// session can be nil when called from public callback endpoint
// security is enforced through state token validation (one-time-use, expires)
func (o *OAuthProvider) ExchangeCodeForTokens(
	ctx context.Context,
	session *model.Session,
	stateToken string,
	code string,
	redirectURI string,
) error {
	// retrieve state token from database
	oauthState, err := o.OAuthStateRepository.GetByStateToken(ctx, stateToken)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			o.Logger.Warnw("invalid or expired state token", "stateToken", stateToken)
			return errors.New("invalid or expired state token")
		}
		o.Logger.Errorw("failed to retrieve state token", "error", err)
		return errs.Wrap(err)
	}

	// validate state token hasn't been used (prevent replay attacks)
	if oauthState.Used {
		o.Logger.Warnw("state token already used", "stateToken", stateToken)
		return errors.New("state token already used")
	}

	// validate state token hasn't expired
	if oauthState.ExpiresAt != nil && time.Now().After(*oauthState.ExpiresAt) {
		o.Logger.Warnw("state token expired", "stateToken", stateToken, "expiresAt", oauthState.ExpiresAt)
		return errors.New("state token expired")
	}

	// get provider from state
	providerID := oauthState.OAuthProviderID.MustGet()
	provider, err := o.OAuthProviderRepository.GetByID(ctx, providerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(err)
		}
		return errs.Wrap(err)
	}

	// mark state token as used
	stateID := oauthState.ID.MustGet()
	if err := o.OAuthStateRepository.MarkAsUsed(ctx, stateID); err != nil {
		o.Logger.Errorw("failed to mark state token as used", "error", err)
		// continue anyway - token exchange is more important
	}

	// get client secret
	clientSecret := provider.ClientSecret.MustGet().String()

	// exchange code for tokens
	tokenURL := provider.TokenURL.MustGet()
	clientID := provider.ClientID.MustGet()

	data := url.Values{
		"code":          {code},
		"client_id":     {clientID.String()},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	tokens, err := o.requestTokens(tokenURL.String(), data)
	if err != nil {
		o.Logger.Errorw("failed to exchange code for tokens", "error", err)
		return errs.Wrap(err)
	}

	// store tokens
	expiresAt := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	if err := o.OAuthProviderRepository.UpdateTokens(
		ctx,
		providerID,
		tokens.AccessToken,
		tokens.RefreshToken,
		expiresAt,
	); err != nil {
		o.Logger.Errorw("failed to update tokens", "error", err)
		return errs.Wrap(err)
	}

	// log successful token exchange
	o.Logger.Infow("oauth token exchange successful",
		"providerID", providerID.String(),
	)

	return nil
}

// GetValidAccessToken returns a valid access token, refreshing if needed
// this is the key method used by other services
// uses singleflight to deduplicate concurrent refresh requests for the same provider
func (o *OAuthProvider) GetValidAccessToken(
	ctx context.Context,
	providerID uuid.UUID,
) (string, error) {
	// use singleflight to ensure only one refresh per provider at a time
	// key is the provider id - all concurrent calls with same provider will share the same work
	val, err, shared := o.refreshGroup.Do(providerID.String(), func() (interface{}, error) {
		return o.getValidAccessTokenInternal(ctx, providerID)
	})

	if shared {
		o.Logger.Debugw("oauth token request shared with concurrent call",
			"providerID", providerID.String(),
		)
	}

	if err != nil {
		return "", err
	}

	return val.(string), nil
}

// getValidAccessTokenInternal is the actual implementation that fetches/refreshes tokens
// this is wrapped by GetValidAccessToken with singleflight for concurrency safety
func (o *OAuthProvider) getValidAccessTokenInternal(
	ctx context.Context,
	providerID uuid.UUID,
) (string, error) {
	// get provider
	provider, err := o.OAuthProviderRepository.GetByID(ctx, providerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errs.Wrap(err)
		}
		return "", errs.Wrap(err)
	}

	// check if authorized
	if provider.IsAuthorized.MustGet() == false {
		return "", errors.New("oauth provider not authorized - user must complete authorization flow")
	}

	// validate that required tokens exist even if marked as authorized
	accessToken, err := provider.AccessToken.Get()
	if err != nil {
		return "", errors.New("oauth provider marked as authorized but access token is missing - authorization may be incomplete")
	}

	refreshToken, err := provider.RefreshToken.Get()
	if err != nil {
		return "", errors.New("oauth provider marked as authorized but refresh token is missing - authorization may be incomplete")
	}

	// check if token needs refresh (5 minute buffer)
	if provider.TokenExpiresAt != nil && time.Now().Add(5*time.Minute).Before(*provider.TokenExpiresAt) {
		// token still valid, return as-is
		return accessToken.String(), nil
	}

	// token expired or about to expire, refresh it
	o.Logger.Infow("refreshing oauth token", "providerID", providerID.String())

	// get client secret (stored as plain text)
	clientSecret := provider.ClientSecret.MustGet().String()

	// refresh tokens
	tokenURL := provider.TokenURL.MustGet()
	clientID := provider.ClientID.MustGet()

	data := url.Values{
		"client_id":     {clientID.String()},
		"refresh_token": {refreshToken.String()},
		"grant_type":    {"refresh_token"},
	}

	// only include client_secret if it's not a placeholder (imported tokens use "n/a")
	// public clients don't need/have client secrets
	if clientSecret != "" && clientSecret != "n/a" {
		data.Set("client_secret", clientSecret)
	}

	newTokens, err := o.requestTokens(tokenURL.String(), data)
	if err != nil {
		o.Logger.Errorw("failed to refresh tokens", "error", err)
		return "", errs.Wrap(err)
	}

	// some providers return new refresh token, some don't
	newRefreshToken := newTokens.RefreshToken
	if newRefreshToken == "" {
		// keep the old refresh token (already validated above)
		newRefreshToken = refreshToken.String()
	}

	// update stored
	expiresAt := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
	if err := o.OAuthProviderRepository.UpdateTokens(
		ctx,
		providerID,
		newTokens.AccessToken,
		newRefreshToken,
		expiresAt,
	); err != nil {
		o.Logger.Errorw("failed to update refreshed tokens", "error", err)
		return "", errs.Wrap(err)
	}

	return newTokens.AccessToken, nil
}

// requestTokens makes a request to the token endpoint
func (o *OAuthProvider) requestTokens(tokenURL string, data url.Values) (*TokenResponse, error) {
	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokens TokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, err
	}

	return &tokens, nil
}

// ImportAuthorizedTokens imports pre-authorized oauth tokens
func (o *OAuthProvider) ImportAuthorizedTokens(
	ctx context.Context,
	session *model.Session,
	tokens []model.ImportAuthorizedToken,
) ([]uuid.UUID, error) {
	ae := NewAuditEvent("OAuthProvider.ImportAuthorizedTokens", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	// validate input
	if len(tokens) == 0 {
		return nil, errs.NewCustomError(errors.New("no tokens provided"))
	}

	var ids []uuid.UUID

	for _, token := range tokens {
		// validate token
		if err := token.Validate(); err != nil {
			return nil, err
		}

		// set default token url if not provided
		token.SetDefaultTokenURL()

		// generate name if empty
		if token.Name == "" {
			randomName, err := random.GenerateRandomURLBase64Encoded(16)
			if err != nil {
				o.Logger.Errorw("failed to generate random name for imported token", "error", err)
				return nil, errs.Wrap(err)
			}
			token.Name = fmt.Sprintf("imported-%s", randomName)
		}

		// refresh token to get fresh access token and metadata
		// note: don't send client_secret for imported tokens (public clients don't have/need it)
		tokenURL := token.TokenURL
		clientID := token.ClientID
		data := url.Values{
			"client_id":     {clientID},
			"refresh_token": {token.RefreshToken},
			"grant_type":    {"refresh_token"},
		}

		o.Logger.Debugw("refreshing token during import", "name", token.Name)
		newTokens, err := o.requestTokens(tokenURL, data)
		if err != nil {
			o.Logger.Errorw("failed to refresh token during import", "error", err, "name", token.Name)
			return nil, errs.NewCustomError(fmt.Errorf("failed to refresh token for '%s': %w", token.Name, err))
		}

		// use refreshed access token
		accessToken := newTokens.AccessToken

		// some providers return new refresh token, some don't
		refreshToken := newTokens.RefreshToken
		if refreshToken == "" {
			// keep the original refresh token
			refreshToken = token.RefreshToken
		}

		// calculate expiry from refresh response
		expiresAt := time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)

		// use scope from refresh response if available, otherwise use provided scope
		// if both are empty, use placeholder to satisfy validation
		scope := newTokens.Scope
		if scope == "" {
			scope = token.Scope
		}
		if scope == "" {
			scope = "offline_access" // placeholder scope if none provided
		}

		// create provider with imported flag
		provider := &model.OAuthProvider{
			Name:            nullable.NewNullableWithValue(*vo.NewString127Must(token.Name)),
			AuthURL:         nullable.NewNullableWithValue(*vo.NewString512Must("n/a")), // placeholder for imported
			TokenURL:        nullable.NewNullableWithValue(*vo.NewString512Must(token.TokenURL)),
			Scopes:          nullable.NewNullableWithValue(*vo.NewString2048Must(scope)),
			ClientID:        nullable.NewNullableWithValue(*vo.NewString255Must(token.ClientID)),
			ClientSecret:    nullable.NewNullableWithValue(*vo.NewOptionalString255Must("n/a")), // placeholder for imported
			AccessToken:     nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(accessToken)),
			RefreshToken:    nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(refreshToken)),
			TokenExpiresAt:  &expiresAt,
			AuthorizedEmail: nullable.NewNullableWithValue(*vo.NewOptionalString255Must(token.User)),
			AuthorizedAt:    ptrTime(time.Now()),
			IsAuthorized:    nullable.NewNullableWithValue(true),
			IsImported:      nullable.NewNullableWithValue(true),
			CompanyID:       nullable.NewNullNullable[uuid.UUID](),
		}

		// check uniqueness
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			o.OAuthProviderRepository.DB,
			"oauth_providers",
			token.Name,
			nil,
			nil,
		)
		if err != nil {
			o.Logger.Errorw("failed to check oauth provider uniqueness", "error", err)
			return nil, errs.Wrap(err)
		}
		if !isOK {
			o.Logger.Debugw("oauth provider name is already used", "name", token.Name)
			return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}

		// save
		id, err := o.OAuthProviderRepository.Insert(ctx, provider)
		if err != nil {
			o.Logger.Errorw("failed to insert imported oauth provider", "error", err)
			return nil, errs.Wrap(err)
		}

		ids = append(ids, *id)
	}

	ae.Details["count"] = len(ids)
	o.AuditLogAuthorized(ae)

	return ids, nil
}

// ExportAuthorizedTokens exports oauth tokens in the import format
func (o *OAuthProvider) ExportAuthorizedTokens(
	ctx context.Context,
	session *model.Session,
	providerID uuid.UUID,
) (*model.ImportAuthorizedToken, error) {
	ae := NewAuditEvent("OAuthProvider.ExportAuthorizedTokens", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	// get provider
	provider, err := o.OAuthProviderRepository.GetByID(ctx, providerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(err)
		}
		o.Logger.Errorw("failed to get oauth provider", "error", err)
		return nil, errs.Wrap(err)
	}

	// check if provider is authorized
	if !provider.IsAuthorized.MustGet() {
		return nil, errors.New("provider is not authorized")
	}

	// extract tokens
	accessToken := ""
	if at, err := provider.AccessToken.Get(); err == nil {
		accessToken = at.String()
	}

	refreshToken := ""
	if rt, err := provider.RefreshToken.Get(); err == nil {
		refreshToken = rt.String()
	}

	authorizedEmail := ""
	if ae, err := provider.AuthorizedEmail.Get(); err == nil {
		authorizedEmail = ae.String()
	}

	var expiresAt int64
	if provider.TokenExpiresAt != nil {
		expiresAt = provider.TokenExpiresAt.UnixMilli()
	}

	var createdAt int64
	if provider.CreatedAt != nil {
		createdAt = provider.CreatedAt.UnixMilli()
	}

	exported := &model.ImportAuthorizedToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     provider.ClientID.MustGet().String(),
		ExpiresAt:    expiresAt,
		Name:         provider.Name.MustGet().String(),
		User:         authorizedEmail,
		Scope:        provider.Scopes.MustGet().String(),
		TokenURL:     provider.TokenURL.MustGet().String(),
		CreatedAt:    createdAt,
	}

	ae.Details["id"] = providerID.String()
	o.AuditLogAuthorized(ae)

	return exported, nil
}

// ptrTime returns a pointer to a time.Time
func ptrTime(t time.Time) *time.Time {
	return &t
}

/* @TODO the logic is here, but i dont think we really need to implement it
// CleanupExpiredStates removes expired oauth state tokens from database
// should be called periodically (e.g., daily)
func (o *OAuthProvider) CleanupExpiredStates(ctx context.Context) error {
	err := o.OAuthStateRepository.DeleteExpired(ctx)
	if err != nil {
		o.Logger.Errorw("failed to cleanup expired oauth states", "error", err)
		return errs.Wrap(err)
	}
	return nil
}
**/

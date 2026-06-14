package sso

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-errors/errors"
	"golang.org/x/oauth2"

	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
)

// oidcNetworkTimeout bounds the outbound calls to the identity provider
// (discovery, token exchange and JWKS) so a slow or unreachable provider cannot
// hang a request goroutine.
const oidcNetworkTimeout = 15 * time.Second

// OIDCClient is a configured generic OpenID Connect relying party. It holds the
// discovered provider, the oauth2 config and the ID token verifier so logins and
// callbacks can be served without re running discovery each request.
type OIDCClient struct {
	Provider  *oidc.Provider
	OAuth2    *oauth2.Config
	Verifier  *oidc.IDTokenVerifier
	ACRValues string
}

// OIDCUserInfo holds the identity claims taken from a verified ID token.
type OIDCUserInfo struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// NewOIDCClient discovers the provider and builds the relying party from the SSO
// configuration. Discovery performs a network request to the issuer.
func NewOIDCClient(ctx context.Context, sso *model.SSOOption) (*OIDCClient, error) {
	if !sso.Enabled {
		return nil, errs.Wrap(errs.ErrSSODisabled)
	}
	issuer := strings.TrimSpace(sso.IssuerURL.String())
	if issuer == "" {
		return nil, errs.Wrap(errors.New("missing OIDC issuer URL"))
	}
	clientID := sso.ClientID.String()
	ctx, cancel := context.WithTimeout(ctx, oidcNetworkTimeout)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, errs.Wrap(errors.Errorf("failed to discover OIDC provider: %s", err))
	}
	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: sso.ClientSecret.String(),
		Endpoint:     provider.Endpoint(),
		RedirectURL:  sso.RedirectURL.String(),
		Scopes:       strings.Fields(sso.ScopesOrDefault()),
	}
	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})
	return &OIDCClient{
		Provider:  provider,
		OAuth2:    oauth2Config,
		Verifier:  verifier,
		ACRValues: strings.TrimSpace(sso.ACRValues.String()),
	}, nil
}

// AuthCodeURL builds the authorization request URL. The nonce binds the ID token
// to this login, PKCE protects the code exchange and acr_values requests a given
// authentication context such as multi factor authentication.
func (c *OIDCClient) AuthCodeURL(state string, nonce string, verifier string) string {
	opts := []oauth2.AuthCodeOption{
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	}
	if c.ACRValues != "" {
		opts = append(opts, oauth2.SetAuthURLParam("acr_values", c.ACRValues))
	}
	return c.OAuth2.AuthCodeURL(state, opts...)
}

// Exchange swaps the authorization code for tokens, verifies the ID token and
// returns the identity claims. It enforces the nonce, that the email is verified
// and, when configured, that the returned authentication context matches.
func (c *OIDCClient) Exchange(
	ctx context.Context,
	code string,
	nonce string,
	verifier string,
) (*OIDCUserInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcNetworkTimeout)
	defer cancel()
	token, err := c.OAuth2.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errs.Wrap(errors.New("no id_token in token response"))
	}
	idToken, err := c.Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if idToken.Nonce != nonce {
		return nil, errs.Wrap(errors.New("OIDC nonce mismatch"))
	}
	var claims struct {
		Subject       string   `json:"sub"`
		Email         string   `json:"email"`
		EmailVerified flexBool `json:"email_verified"`
		Name          string   `json:"name"`
		ACR           string   `json:"acr"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, errs.Wrap(err)
	}
	if claims.Email == "" {
		return nil, errs.Wrap(errors.New("no email claim from OIDC provider"))
	}
	// a generic provider can hand out self set, unverified emails so the email
	// is only trusted for account matching when the provider has verified it
	if !bool(claims.EmailVerified) {
		return nil, errs.Wrap(errors.New("email is not verified by the OIDC provider"))
	}
	if c.ACRValues != "" && !acrSatisfied(claims.ACR, c.ACRValues) {
		return nil, errs.Wrap(errors.New("authentication context requirement not met"))
	}
	return &OIDCUserInfo{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: bool(claims.EmailVerified),
		Name:          claims.Name,
	}, nil
}

// NewPKCEVerifier returns a fresh high entropy PKCE code verifier (RFC 7636).
func NewPKCEVerifier() string {
	return oauth2.GenerateVerifier()
}

// acrSatisfied reports whether the acr returned by the provider is one of the
// space separated values that were requested.
func acrSatisfied(returned string, requested string) bool {
	returned = strings.TrimSpace(returned)
	if returned == "" {
		return false
	}
	for _, want := range strings.Fields(requested) {
		if returned == want {
			return true
		}
	}
	return false
}

// flexBool accepts the email_verified claim as either a JSON boolean or a
// quoted string, since providers differ, and fails closed on anything else.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch strings.Trim(string(data), `"`) {
	case "true":
		*b = true
	case "false", "", "null":
		*b = false
	default:
		return fmt.Errorf("invalid boolean value for claim: %s", string(data))
	}
	return nil
}

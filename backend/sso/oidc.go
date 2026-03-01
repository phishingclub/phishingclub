package sso

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCClient wraps the go-oidc provider and the oauth2 config so that
// the service layer has a single, stable type to work with.
type OIDCClient struct {
	provider *gooidc.Provider
	oauth2   oauth2.Config
	verifier *gooidc.IDTokenVerifier
}

// NewOIDCClient discovers the provider metadata from issuerURL and constructs
// an OIDCClient ready for use.  The discovery document is fetched during
// construction so any network or configuration error is surfaced early.
func NewOIDCClient(
	ctx context.Context,
	issuerURL string,
	clientID string,
	clientSecret string,
	redirectURL string,
	scopes []string,
) (*OIDCClient, error) {
	if issuerURL == "" {
		return nil, fmt.Errorf("issuerURL must not be empty")
	}
	if clientID == "" {
		return nil, fmt.Errorf("clientID must not be empty")
	}
	if redirectURL == "" {
		return nil, fmt.Errorf("redirectURL must not be empty")
	}

	provider, err := gooidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc provider discovery failed for %q: %w", issuerURL, err)
	}

	// always include openid; callers may add profile and email
	merged := mergeScopes([]string{gooidc.ScopeOpenID}, scopes)

	cfg := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       merged,
	}

	verifier := provider.Verifier(&gooidc.Config{
		ClientID: clientID,
	})

	return &OIDCClient{
		provider: provider,
		oauth2:   cfg,
		verifier: verifier,
	}, nil
}

// AuthCodeURL returns the authorization endpoint URL with the supplied state,
// nonce, PKCE challenge and optional ACR values.
func (c *OIDCClient) AuthCodeURL(
	state string,
	nonce string,
	codeChallenge string,
	acrValues string,
) string {
	opts := []oauth2.AuthCodeOption{
		gooidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	if acrValues != "" {
		opts = append(opts, oauth2.SetAuthURLParam("acr_values", acrValues))
	}
	return c.oauth2.AuthCodeURL(state, opts...)
}

// OIDCTokenResult is returned from ExchangeCode.
type OIDCTokenResult struct {
	// AccessToken is the raw access token from the provider.
	AccessToken string
	// IDToken is the verified, parsed id_token.
	IDToken *gooidc.IDToken
	// RawClaims are all claims extracted from the id_token.
	RawClaims map[string]any
}

// ExchangeCode exchanges the authorisation code for tokens, verifies the
// id_token signature and nonce, and returns the result.
func (c *OIDCClient) ExchangeCode(
	ctx context.Context,
	code string,
	codeVerifier string,
	expectedNonce string,
) (*OIDCTokenResult, error) {
	token, err := c.oauth2.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("provider did not return an id_token")
	}

	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id_token verification failed: %w", err)
	}

	if idToken.Nonce != expectedNonce {
		return nil, fmt.Errorf("id_token nonce mismatch: got %q, want %q", idToken.Nonce, expectedNonce)
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract id_token claims: %w", err)
	}

	return &OIDCTokenResult{
		AccessToken: token.AccessToken,
		IDToken:     idToken,
		RawClaims:   claims,
	}, nil
}

// UserInfoEmail fetches the userinfo endpoint and returns the sub, email and
// name claims as plain strings.  The access token from ExchangeCode is used.
func (c *OIDCClient) UserInfoEmail(
	ctx context.Context,
	accessToken string,
) (sub, email, name string, err error) {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
	info, infoErr := c.provider.UserInfo(ctx, src)
	if infoErr != nil {
		return "", "", "", fmt.Errorf("userinfo request failed: %w", infoErr)
	}

	sub = info.Subject

	var extra struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if claimErr := info.Claims(&extra); claimErr != nil {
		// non-fatal: we still have the subject
		_ = claimErr
	}
	email = extra.Email
	name = extra.Name
	return sub, email, name, nil
}

// CheckRoleClaim inspects rawClaims for the claim identified by claimPath and
// checks whether requiredValue is present.
//
// claimPath supports a single dot-separated path into nested objects, e.g.
// "realm_access.roles" will look up claims["realm_access"]["roles"].  The
// leaf value may be a string, a []interface{} (JSON array of strings), or a
// JSON-encoded array embedded inside another claim value.
//
// Returns true when the required value is found, false otherwise.
func CheckRoleClaim(
	rawClaims map[string]any,
	claimPath string,
	requiredValue string,
) bool {
	if claimPath == "" || requiredValue == "" {
		return true // gating disabled
	}

	parts := strings.SplitN(claimPath, ".", 2)
	top, ok := rawClaims[parts[0]]
	if !ok {
		return false
	}

	// descend one level for dotted paths, e.g. "realm_access.roles"
	var leaf any = top
	if len(parts) == 2 {
		nested, isMap := top.(map[string]any)
		if !isMap {
			return false
		}
		leaf, ok = nested[parts[1]]
		if !ok {
			return false
		}
	}

	return containsValue(leaf, requiredValue)
}

// containsValue returns true when v equals requiredValue or v is a slice
// that contains requiredValue.
func containsValue(v any, requiredValue string) bool {
	switch typed := v.(type) {
	case string:
		return typed == requiredValue
	case []any:
		for _, item := range typed {
			if s, ok := item.(string); ok && s == requiredValue {
				return true
			}
		}
	case []string:
		for _, s := range typed {
			if s == requiredValue {
				return true
			}
		}
	default:
		// attempt JSON-encoded array stored as a plain string
		if raw, ok := v.(string); ok {
			var arr []string
			if err := json.Unmarshal([]byte(raw), &arr); err == nil {
				for _, s := range arr {
					if s == requiredValue {
						return true
					}
				}
			}
		}
	}
	return false
}

// GeneratePKCEPair generates a cryptographically random code_verifier and
// derives the S256 code_challenge from it.
//
// The verifier is a 64-byte URL-safe base64-encoded random value (no padding).
// The challenge is the base64url encoding (no padding) of the SHA-256 digest
// of the verifier as required by RFC 7636 §4.2.
func GeneratePKCEPair() (verifier string, challenge string, err error) {
	b, err := generateRandomBytes(64)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE verifier bytes: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// GenerateStateToken generates a cryptographically random URL-safe state token
// suitable for use as the OAuth2 'state' and OIDC 'nonce' parameters.
func GenerateStateToken() (string, error) {
	b, err := generateRandomBytes(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate state token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SSOStateExpiry is how long an SSO state token remains valid.
const SSOStateExpiry = 10 * time.Minute

// mergeScopes returns a deduplicated slice with base prepended then extras
// appended, preserving order.
func mergeScopes(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, s := range base {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range extra {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

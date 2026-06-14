package controller

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/sso"
)

const (
	ssoStateCookieKey    = "sso_state"
	ssoNonceCookieKey    = "sso_nonce"
	ssoVerifierCookieKey = "sso_pkce_verifier"
)

// SSO the single sign on controller
type SSO struct {
	Common
	*service.SSO
}

// Upsert upserts a SSO configuration
func (s *SSO) Upsert(g *gin.Context) {
	session, _, ok := s.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var request model.SSOOption
	if ok := s.handleParseRequest(g, &request); !ok {
		return
	}
	// handle upsert
	err := s.SSO.Upsert(
		g.Request.Context(),
		session,
		&request,
	)
	// handle responses
	if ok := s.handleErrors(g, err); !ok {
		return
	}
	s.Response.OK(g, gin.H{})
}

func (s *SSO) IsEnabled(g *gin.Context) {
	status, err := s.SSO.LoginStatus(g.Request.Context())
	if err != nil {
		// fail closed for the login page, report SSO as unavailable
		s.Response.OK(g, &service.SSOLoginStatus{})
		return
	}
	s.Response.OK(g, status)
}

// setSSOCookie sets a short lived, http only, secure cookie used to carry the
// SSO state, nonce and PKCE verifier across the redirect to the provider.
func (s *SSO) setSSOCookie(g *gin.Context, name string, value string) {
	http.SetCookie(g.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(5 * time.Minute / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSSOCookie removes a cookie previously set by setSSOCookie.
func (s *SSO) clearSSOCookie(g *gin.Context, name string) {
	http.SetCookie(g.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// randomHex returns n random bytes hex encoded.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// OIDCLogin starts the generic OIDC authorization code flow.
func (s *SSO) OIDCLogin(g *gin.Context) {
	state, err := randomHex(32)
	if err != nil {
		s.Response.ServerError(g)
		return
	}
	nonce, err := randomHex(32)
	if err != nil {
		s.Response.ServerError(g)
		return
	}
	verifier := sso.NewPKCEVerifier()

	authURL, err := s.SSO.OIDCAuthCodeURL(g.Request.Context(), state, nonce, verifier)
	if err != nil {
		s.Response.BadRequest(g)
		return
	}
	s.setSSOCookie(g, ssoStateCookieKey, state)
	s.setSSOCookie(g, ssoNonceCookieKey, nonce)
	s.setSSOCookie(g, ssoVerifierCookieKey, verifier)
	g.Redirect(http.StatusTemporaryRedirect, authURL)
}

// OIDCCallback completes the generic OIDC authorization code flow.
func (s *SSO) OIDCCallback(g *gin.Context) {
	stateCookie, errState := g.Request.Cookie(ssoStateCookieKey)
	nonceCookie, errNonce := g.Request.Cookie(ssoNonceCookieKey)
	verifierCookie, errVerifier := g.Request.Cookie(ssoVerifierCookieKey)
	// always clear the temporary cookies
	s.clearSSOCookie(g, ssoStateCookieKey)
	s.clearSSOCookie(g, ssoNonceCookieKey)
	s.clearSSOCookie(g, ssoVerifierCookieKey)

	stateParam := g.Query("state")
	if errState != nil || errNonce != nil || errVerifier != nil ||
		stateCookie.Value == "" || stateParam == "" ||
		subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(stateParam)) != 1 {
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}

	code := g.Query("code")
	session, err := s.SSO.HandleOIDCCallback(g, code, nonceCookie.Value, verifierCookie.Value)
	if err != nil {
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}
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
	g.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}

func (s *SSO) EntreIDLogin(g *gin.Context) {
	authURL, err := s.SSO.EntreIDLogin(g)
	if errors.Is(err, errs.ErrSSODisabled) {
		s.Response.BadRequest(g)
		return
	}
	if ok := s.handleErrors(g, err); !ok {
		s.Response.BadRequest(g)
		return
	}

	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		s.Response.ServerError(g)
		return
	}
	state := hex.EncodeToString(stateBytes)

	http.SetCookie(g.Writer, &http.Cookie{
		Name:     ssoStateCookieKey,
		Value:    state,
		Path:     "/",
		MaxAge:   int(5 * time.Minute / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	g.Redirect(http.StatusTemporaryRedirect, authURL+"&state="+state)
}

func (s *SSO) EntreIDCallBack(g *gin.Context) {
	stateCookie, err := g.Request.Cookie(ssoStateCookieKey)
	http.SetCookie(g.Writer, &http.Cookie{
		Name:     ssoStateCookieKey,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	stateParam := g.Query("state")
	if err != nil || stateCookie.Value == "" || stateParam == "" ||
		subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(stateParam)) != 1 {
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}

	code := g.Query("code")
	session, err := s.SSO.HandlEntraIDCallback(g, code)
	if err != nil {
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}
	if ok := s.handleErrors(g, err); !ok {
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
	g.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}

package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
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

// IsEnabled reports whether any SSO provider (legacy Entra ID or generic OIDC)
// is currently active.
func (s *SSO) IsEnabled(g *gin.Context) {
	s.Response.OK(g, s.SSO.IsEnabled())
}

// IsLegacyEnabled reports whether the legacy Microsoft Entra ID path is active.
func (s *SSO) IsLegacyEnabled(g *gin.Context) {
	s.Response.OK(g, s.SSO.IsLegacyEnabled())
}

// IsOIDCEnabled reports whether the generic OIDC path is active.
func (s *SSO) IsOIDCEnabled(g *gin.Context) {
	s.Response.OK(g, s.SSO.IsOIDCEnabled())
}

// EntreIDLogin initiates the legacy Microsoft Entra ID login flow by
// redirecting the browser to the Microsoft authorization endpoint.
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
	g.Redirect(http.StatusTemporaryRedirect, authURL)
}

// EntreIDCallBack handles the authorization code callback from Microsoft for
// the legacy Entra ID flow.
func (s *SSO) EntreIDCallBack(g *gin.Context) {
	code := g.Query("code")
	session, err := s.SSO.HandlEntraIDCallback(g, code)
	if err != nil {
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}
	if ok := s.handleErrors(g, err); !ok {
		return
	}
	setSessionCookie(g, session)
	g.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}

// OIDCLogin initiates the generic OIDC login flow by generating a PKCE pair,
// persisting the state record and redirecting the browser to the provider's
// authorization endpoint.
func (s *SSO) OIDCLogin(g *gin.Context) {
	authURL, err := s.SSO.OIDCLoginURL(g.Request.Context())
	if errors.Is(err, errs.ErrSSODisabled) {
		s.Response.BadRequest(g)
		return
	}
	if ok := s.handleErrors(g, err); !ok {
		s.Response.BadRequest(g)
		return
	}
	g.Redirect(http.StatusTemporaryRedirect, authURL)
}

// OIDCCallback handles the authorization code callback from a generic OIDC
// provider.  It validates the state token (CSRF), completes the PKCE token
// exchange, verifies the nonce, enforces role and ACR requirements, then
// creates or reuses a local account and sets a session cookie.
func (s *SSO) OIDCCallback(g *gin.Context) {
	code := g.Query("code")
	state := g.Query("state")
	errorParam := g.Query("error")

	// surface provider-level errors without leaking detail to the user
	if errorParam != "" {
		errDesc := g.Query("error_description")
		s.Logger.Warnw("oidc provider returned error on callback",
			"error", errorParam,
			"description", errDesc,
		)
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}

	if code == "" || state == "" {
		s.Logger.Warnw("oidc callback missing required parameters")
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}

	session, err := s.SSO.HandleOIDCCallback(
		g.Request.Context(),
		code,
		state,
		g.ClientIP(),
	)
	if err != nil {
		s.Logger.Warnw("oidc callback failed", "error", err)
		g.Redirect(http.StatusTemporaryRedirect, "/login?ssoAuthError=1")
		return
	}

	setSessionCookie(g, session)
	g.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}

// setSessionCookie writes the session ID into a secure, HttpOnly, SameSite=Strict
// cookie on the response.
func setSessionCookie(g *gin.Context, session *model.Session) {
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
}

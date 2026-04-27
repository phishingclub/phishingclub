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
)

const ssoStateCookieKey = "sso_state"

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
	// if no sso client is setup, then it is not enabled
	if s.SSO.MSALClient == nil {
		s.Response.OK(g, false)
		return
	}
	s.Response.OK(g, true)
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

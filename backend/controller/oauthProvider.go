package controller

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// OAuthProviderColumnsMap is a map between the frontend and the backend
var OAuthProviderColumnsMap = map[string]string{
	"created_at":    repository.TableColumn(database.OAUTH_PROVIDER_TABLE, "created_at"),
	"updated_at":    repository.TableColumn(database.OAUTH_PROVIDER_TABLE, "updated_at"),
	"name":          repository.TableColumn(database.OAUTH_PROVIDER_TABLE, "name"),
	"is_authorized": repository.TableColumn(database.OAUTH_PROVIDER_TABLE, "is_authorized"),
}

// OAuthProvider is a controller
type OAuthProvider struct {
	Common
	OAuthProviderService *service.OAuthProvider
	Config               *config.Config
}

// Create creates a new oauth provider
func (c *OAuthProvider) Create(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.OAuthProvider
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save oauth provider
	id, err := c.OAuthProviderService.Create(g, session, &req)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetAll gets oauth providers
func (c *OAuthProvider) GetAll(g *gin.Context) {
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
	queryArgs.RemapOrderBy(OAuthProviderColumnsMap)
	companyID := companyIDFromRequestQuery(g)
	// get
	providers, err := c.OAuthProviderService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		repository.OAuthProviderOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, providers)
}

// GetByID gets an oauth provider by id
func (c *OAuthProvider) GetByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get
	provider, err := c.OAuthProviderService.GetByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, provider)
}

// UpdateByID updates an oauth provider by id
func (c *OAuthProvider) UpdateByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// parse request
	var req model.OAuthProvider
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// update
	err := c.OAuthProviderService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"message": "updated"})
}

// DeleteByID deletes an oauth provider by id
func (c *OAuthProvider) DeleteByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete
	err := c.OAuthProviderService.DeleteByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"message": "deleted"})
}

// RemoveAuthorization removes authorization tokens from an oauth provider
func (c *OAuthProvider) RemoveAuthorization(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// remove authorization
	err := c.OAuthProviderService.RemoveAuthorization(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"message": "authorization removed"})
}

// GetAuthorizationURL generates the oauth authorization url for user to visit
func (c *OAuthProvider) GetAuthorizationURL(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// construct redirect uri from config (secure - not user-controllable)
	var host string
	if build.Flags.Production {
		host = c.Config.TLSHost()
	} else {
		host = "localhost"
	}

	adminPort := c.Config.AdminNetAddressPort()

	var redirectURI string
	if adminPort == 443 || adminPort == 0 {
		// standard https port or ephemeral port, no need to include in url
		redirectURI = fmt.Sprintf("https://%s/api/v1/oauth-callback", host)
	} else {
		// non-standard port, include it
		redirectURI = fmt.Sprintf("https://%s:%d/api/v1/oauth-callback", host, adminPort)
	}

	// get authorization url
	authURL, err := c.OAuthProviderService.GetAuthorizationURL(
		g.Request.Context(),
		session,
		id,
		redirectURI,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"authorizationURL": authURL})
}

// HandleCallback handles the oauth callback from the provider
// note: this endpoint is PUBLIC (no session required) because oauth providers call it from cross-site context
func (c *OAuthProvider) HandleCallback(g *gin.Context) {
	code := g.Query("code")
	state := g.Query("state")
	errorParam := g.Query("error")

	// handle oauth errors from provider (don't expose error details to user)
	if errorParam != "" {
		errorDesc := g.Query("error_description")
		c.Logger.Warnw("oauth provider returned error", "error", errorParam, "description", errorDesc)
		providerID := c.OAuthProviderService.FailAuthorization(g.Request.Context(), state)
		c.renderCallbackPage(g, false, "provider_error", providerID)
		return
	}

	// validate parameters
	if code == "" || state == "" {
		c.Logger.Warnw("oauth callback missing required parameters")
		providerID := c.OAuthProviderService.FailAuthorization(g.Request.Context(), state)
		c.renderCallbackPage(g, false, "invalid_request", providerID)
		return
	}

	// construct redirect uri from config (must match the one used in authorization)
	var host string
	if build.Flags.Production {
		host = c.Config.TLSHost()
	} else {
		host = "localhost"
	}

	adminPort := c.Config.AdminNetAddressPort()

	var redirectURI string
	if adminPort == 443 || adminPort == 0 {
		// standard https port or ephemeral port, no need to include in url
		redirectURI = fmt.Sprintf("https://%s/api/v1/oauth-callback", host)
	} else {
		// non-standard port, include it
		redirectURI = fmt.Sprintf("https://%s:%d/api/v1/oauth-callback", host, adminPort)
	}

	// The provider redirects the popup here as a cross site navigation, so the
	// SameSite Strict admin session cookie is not sent and the state can not be
	// bound to a session. The state token is the only check: random, single
	// use and expiring after ten minutes.
	providerID, err := c.OAuthProviderService.ExchangeCodeForTokens(
		g.Request.Context(),
		nil,
		state,
		code,
		redirectURI,
	)
	if err != nil {
		// log detailed error internally, show generic error to user
		c.Logger.Warnw("failed to exchange code for tokens", "reason", err)
		c.renderCallbackPage(g, false, "token_exchange_failed", providerID)
		return
	}

	// success - render success page
	c.renderCallbackPage(g, true, "", providerID)
}

// GetAuthorizationStatus returns the result of an authorization flow, so the
// admin page learns the outcome when the callback runs on another origin.
func (c *OAuthProvider) GetAuthorizationStatus(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	state := g.Query("state")
	if state == "" || len(state) > 255 {
		c.Response.BadRequestMessage(g, "missing state")
		return
	}
	status, err := c.OAuthProviderService.GetAuthorizationStatus(
		g.Request.Context(),
		session,
		id,
		state,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"status": status})
}

// oauthCallbackChannel is the BroadcastChannel name the callback page posts the
// result on. The admin oauth provider page listens on the same name.
const oauthCallbackChannel = "phishingclub-oauth-callback"

// renderCallbackPage renders a plain text page for oauth callback result
// this page is shown in the popup window, notifies the admin page, and instructs user to close it
// providerID names the flow in the channel message, uuid.Nil when unknown.
func (c *OAuthProvider) renderCallbackPage(g *gin.Context, success bool, errorCode string, providerID uuid.UUID) {
	var text string
	var status string

	// define allowed error codes and their user-friendly messages
	allowedErrors := map[string]string{
		"provider_error":        "The OAuth provider returned an error",
		"invalid_request":       "Invalid authorization request",
		"token_exchange_failed": "Failed to exchange authorization code for tokens",
	}

	if success {
		text = "OAuth authorization successful!\n\nYou can close this window now."
		status = "success"
	} else {
		// validate error code - only use allowed values
		userMessage, ok := allowedErrors[errorCode]
		if !ok {
			// if error code is not in allowed list, use generic message
			c.Logger.Warnw("invalid error code passed to renderCallbackPage", "errorCode", errorCode)
			userMessage = "An unexpected error occurred"
			errorCode = "unknown_error"
		}
		text = fmt.Sprintf("OAuth authorization failed.\n\n%s\n\nYou can close this window now.", userMessage)
		status = "error"
	}

	// The popup is opened with noopener, so it has no handle to the admin
	// window. The result is sent on a BroadcastChannel, which only reaches
	// pages on this same origin. The admin page also polls the flow status, so
	// a browser where the channel does not reach the admin page still updates.
	// The provider id is a uuid string, so it is safe inside the script.
	providerIDText := ""
	if providerID != uuid.Nil {
		providerIDText = providerID.String()
	}
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>OAuth Callback</title>
</head>
<body>
	<pre>%s</pre>
	<script>
		try {
			var channel = new BroadcastChannel('%s');
			channel.postMessage({ type: 'oauth-callback', status: '%s', providerID: '%s' });
			channel.close();
		} catch (e) {}
		setTimeout(function() { window.close(); }, 5000);
	</script>
</body>
</html>`, text, oauthCallbackChannel, status, providerIDText)

	g.Header("Content-Type", "text/html; charset=utf-8")
	g.String(http.StatusOK, html)
}

// ImportAuthorizedTokens imports pre-authorized oauth tokens
func (c *OAuthProvider) ImportAuthorizedTokens(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req []model.ImportAuthorizedToken
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// import tokens
	ids, err := c.OAuthProviderService.ImportAuthorizedTokens(
		g.Request.Context(),
		session,
		req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	// convert ids to strings
	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = id.String()
	}

	c.Response.OK(g, gin.H{
		"ids":   idStrings,
		"count": len(ids),
	})
}

// ExportAuthorizedTokens exports oauth tokens in the import format
func (c *OAuthProvider) ExportAuthorizedTokens(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// export tokens
	exported, err := c.OAuthProviderService.ExportAuthorizedTokens(
		g.Request.Context(),
		session,
		*id,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, exported)
}

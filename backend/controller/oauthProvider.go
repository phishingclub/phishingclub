package controller

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
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
			Limit:  &queryArgs.Limit,
			Offset: &queryArgs.Offset,
			Search: &queryArgs.Search,
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
		c.renderCallbackPage(g, false, "provider_error")
		return
	}

	// validate parameters
	if code == "" || state == "" {
		c.Logger.Warnw("oauth callback missing required parameters")
		c.renderCallbackPage(g, false, "invalid_request")
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

	// exchange code for tokens
	// session is nil because callback is public (cross-site context doesn't send cookies)
	// validation happens through state token lookup (bound to initiating session)
	if err := c.OAuthProviderService.ExchangeCodeForTokens(
		g.Request.Context(),
		nil, // no session - callback is public
		state,
		code,
		redirectURI,
	); err != nil {
		// log detailed error internally, show generic error to user
		c.Logger.Warnw("failed to exchange code for tokens", "reason", err)
		c.renderCallbackPage(g, false, "token_exchange_failed")
		return
	}

	// success - render success page
	c.renderCallbackPage(g, true, "")
}

// renderCallbackPage renders a plain text page for oauth callback result
// this page is shown in the popup window, notifies the parent, and instructs user to close it
func (c *OAuthProvider) renderCallbackPage(g *gin.Context, success bool, errorCode string) {
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

	// determine target origin for postMessage
	// in dev, use wildcard for localhost to handle vite proxy (frontend on :8003, backend on :8002)
	// in production, use specific origin for security
	var targetOrigin string
	if build.Flags.Production {
		targetOrigin = "window.location.origin"
	} else {
		// in dev, check if we're on localhost and use wildcard
		targetOrigin = "(window.location.hostname === 'localhost' ? '*' : window.location.origin)"
	}

	// html with script to notify parent window and plain text display
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>OAuth Callback</title>
</head>
<body>
	<pre>%s</pre>
	<script>
		console.log('OAuth callback page loaded with status: %s');
		if (window.opener) {
			console.log('Sending message to parent window');
			try {
				var targetOrigin = %s;
				console.log('Target origin:', targetOrigin);
				window.opener.postMessage({
					type: 'oauth-callback',
					status: '%s'
				}, targetOrigin);
				console.log('Message sent successfully');
			} catch (e) {
				console.error('Failed to send message:', e);
			}
		} else {
			console.log('No window.opener found');
		}

		// auto-close popup after 5 seconds
		setTimeout(function() {
			console.log('Auto-closing popup window');
			window.close();
		}, 5000);
	</script>
</body>
</html>`, text, status, targetOrigin, status)

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

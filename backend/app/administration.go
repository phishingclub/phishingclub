package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/acme"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/frontend"
	"github.com/phishingclub/phishingclub/server"
	"go.uber.org/zap"
)

const (
	// health
	ROUTE_V1_HEALTH = "/api/v1/healthz"
	ROUTE_V1_LIVE   = "/api/v1/livez"
	ROUTE_V1_READY  = "/api/v1/readyz"
	// application
	ROUTE_V1_FEATURE                 = "/api/v1/features"
	ROUTE_V1_UPDATE_AVAILABLE        = "/api/v1/update/available"
	ROUTE_V1_UPDATE_AVAILABLE_CACHED = "/api/v1/update/available/cached"
	ROUTE_V1_UPDATE                  = "/api/v1/update"
	// backup
	ROUTE_V1_BACKUP_CREATE   = "/api/v1/backup/create"
	ROUTE_V1_BACKUP_LIST     = "/api/v1/backup/list"
	ROUTE_V1_BACKUP_DOWNLOAD = "/api/v1/backup/download/:filename"
	// user
	ROUTE_V1_USER        = "/api/v1/user"
	ROUTE_V1_USER_ID     = "/api/v1/user/:id"
	ROUTE_V1_USER_LOGIN  = "/api/v1/user/login"
	ROUTE_V1_USER_LOGOUT = "/api/v1/user/logout"
	// #nosec
	ROUTE_V1_USER_PASSWORD            = "/api/v1/user/password"
	ROUTE_V1_USER_USERNAME            = "/api/v1/user/username"
	ROUTE_V1_USER_FULLNAME            = "/api/v1/user/fullname"
	ROUTE_V1_USER_EMAIL               = "/api/v1/user/email"
	ROUTE_V1_USER_SESSIONS            = "/api/v1/user/sessions"
	ROUTE_V1_USER_SESSIONS_INVALIDATE = "/api/v1/user/sessions/invalidate"
	ROUTE_V1_USER_API                 = "/api/v1/user/api"
	// sso
	ROUTE_V1_SSO_ENTRA_ID          = "/api/v1/sso/entra-id"
	ROUTE_V1_SSO_ENTRA_ID_ENABLED  = "/api/v1/sso/entra-id/enabled"
	ROUTE_V1_SSO_ENTRA_ID_LOGIN    = "/api/v1/sso/entra-id/login"
	ROUTE_V1_SSO_ENTRA_ID_CALLBACK = "/api/v1/sso/entra-id/auth"
	// mfa
	ROUTE_V1_USER_MFA_TOTP_SETUP        = "/api/v1/user/mfa/totp/setup"
	ROUTE_V1_USER_MFA_TOTP_SETUP_VERIFY = "/api/v1/user/mfa/totp/setup/verify"
	ROUTE_V1_USER_MFA_TOTP_VERIFY       = "/api/v1/user/mfa/totp/verify"
	ROUTE_V1_USER_MFA_TOTP              = "/api/v1/user/mfa/totp"
	ROUTE_V1_QR_FROM_TOTP               = "/api/v1/qr/totp"
	ROUTE_V1_QR_URL_TO_HTML             = "/api/v1/qr/html"
	// session
	ROUTE_V1_SESSION_ID   = "/api/v1/session/:id"
	ROUTE_V1_SESSION_PING = "/api/v1/session/ping"
	// company
	ROUTE_V1_COMPANY                  = "/api/v1/company"
	ROUTE_V1_COMPANY_ID               = "/api/v1/company/:id"
	ROUTE_V1_COMPANY_ID_EXPORT        = "/api/v1/company/:id/export"
	ROUTE_V1_COMPANY_ID_EXPORT_SHARED = "/api/v1/company/shared/export"
	// option
	ROUTE_V1_OPTION     = "/api/v1/option"
	ROUTE_V1_OPTION_GET = "/api/v1/option/:key"
	// installation
	ROUTE_V1_INSTALL           = "/api/v1/install"
	ROUTE_V1_INSTALL_TEMPLATES = "/api/v1/install/templates"
	// domain
	ROUTE_V1_DOMAIN                   = "/api/v1/domain"
	ROUTE_V1_DOMAIN_SUBSET            = "/api/v1/domain/subset"
	ROUTE_V1_DOMAIN_SUBSET_NO_PROXIES = "/api/v1/domain/subset/noproxies"
	ROUTE_V1_DOMAIN_ID                = "/api/v1/domain/:id"
	ROUTE_V1_DOMAIN_NAME              = "/api/v1/domain/name/:domain"
	// page
	ROUTE_V1_PAGE            = "/api/v1/page"
	ROUTE_V1_PAGE_OVERVIEW   = "/api/v1/page/overview"
	ROUTE_V1_PAGE_ID         = "/api/v1/page/:id"
	ROUTE_V1_PAGE_CONTENT_ID = "/api/v1/page/:id/content"
	// proxy
	ROUTE_V1_PROXY          = "/api/v1/proxy"
	ROUTE_V1_PROXY_OVERVIEW = "/api/v1/proxy/overview"
	ROUTE_V1_PROXY_ID       = "/api/v1/proxy/:id"
	// ip allow list
	ROUTE_V1_IP_ALLOW_LIST_PROXY_CONFIG       = "/api/v1/ip-allow-list/proxy-config/:id"
	ROUTE_V1_IP_ALLOW_LIST_CLEAR_PROXY_CONFIG = "/api/v1/ip-allow-list/clear-proxy-config/:id"
	// recipient and groups
	ROUTE_V1_RECIPIENT                  = "/api/v1/recipient"
	ROUTE_V1_RECIPIENT_IMPORT           = "/api/v1/recipient/import"
	ROUTE_V1_RECIPIENT_EXPORT           = "/api/v1/recipient/:id/export"
	ROUTE_V1_RECIPIENT_ID               = "/api/v1/recipient/:id"
	ROUTE_V1_RECIPIENT_ID_EVENTS        = "/api/v1/recipient/:id/events"
	ROUTE_V1_RECIPIENT_ID_STATS         = "/api/v1/recipient/:id/stats"
	ROUTE_V1_RECIPIENT_REPEAT_OFFENDERS = "/api/v1/recipient/repeat-offenders"
	ROUTE_V1_RECIPIENT_ORPHANED         = "/api/v1/recipient/orphaned"
	ROUTE_V1_RECIPIENT_ORPHANED_DELETE  = "/api/v1/recipient/orphaned/delete"
	ROUTE_V1_RECIPIENT_GROUP            = "/api/v1/recipient/group"
	ROUTE_V1_RECIPIENT_GROUP_ID         = "/api/v1/recipient/group/:id"
	ROUTE_V1_RECIPIENT_GROUP_ID_IMPORT  = "/api/v1/recipient/group/:id/import"
	ROUTE_V1_RECIPIENT_GROUP_RECIPIENTS = "/api/v1/recipient/group/:id/recipients"
	// logging
	ROUTE_V1_LOG      = "/api/v1/log"
	ROUTE_V1_LOG_TEST = "/api/v1/log/test"
	// smtp configuration
	ROUTE_V1_SMTP_CONFIGURATION               = "/api/v1/smtp-configuration"
	ROUTE_V1_SMTP_CONFIGURATION_ID            = "/api/v1/smtp-configuration/:id"
	ROUTE_V1_SMTP_CONFIGURATION_ID_TEST_EMAIL = "/api/v1/smtp-configuration/:id/test-email"
	ROUTE_V1_SMTP_CONFIGURATION_HEADERS       = "/api/v1/smtp-configuration/:id/header"
	ROUTE_V1_SMTP_HEADER_ID                   = "/api/v1/smtp-configuration/:id/header/:headerID"
	// email
	ROUTE_V1_EMAIL            = "/api/v1/email"
	ROUTE_V1_EMAIL_OVERVIEW   = "/api/v1/email/overview"
	ROUTE_V1_EMAIL_ID         = "/api/v1/email/:id"
	ROUTE_V1_EMAIL_SEND_TEST  = "/api/v1/email/:id/send-test"
	ROUTE_V1_EMAIL_CONTENT_ID = "/api/v1/email/:id/content"
	// campaign
	ROUTE_V1_CAMPAIGN_TEMPLATE           = "/api/v1/campaign/template"
	ROUTE_v1_CAMPAIGN_TEMPLATE_ID        = "/api/v1/campaign/template/:id"
	ROUTE_V1_CAMPAIGN                    = "/api/v1/campaign"
	ROUTE_V1_CAMPAIGN_CALENDAR           = "/api/v1/campaign/calendar"
	ROUTE_V1_CAMPAIGN_ACTIVE             = "/api/v1/campaign/active"
	ROUTE_V1_CAMPAIGN_UPCOMING           = "/api/v1/campaign/upcoming"
	ROUTE_V1_CAMPAIGN_FINISHED           = "/api/v1/campaign/finished"
	ROUTE_V1_CAMPAIGN_CLOSE              = "/api/v1/campaign/:id/close"
	ROUTE_V1_CAMPAIGN_EXPORT_EVENTS      = "/api/v1/campaign/:id/export/events"
	ROUTE_V1_CAMPAIGN_EXPORT_SUBMISSIONS = "/api/v1/campaign/:id/export/submissions"
	ROUTE_V1_CAMPAIGN_ANONYMIZE          = "/api/v1/campaign/:id/anonymize"
	ROUTE_V1_CAMPAIGN_ID                 = "/api/v1/campaign/:id"
	ROUTE_V1_CAMPAIGN_NAME               = "/api/v1/campaign/name/:name"
	ROUTE_V1_CAMPAIGN_RECIPIENTS         = "/api/v1/campaign/:id/recipients"
	ROUTE_V1_CAMPAIGN_RESULT_STATS       = "/api/v1/campaign/:id/statistics"
	ROUTE_V1_CAMPAIGN_EVENTS             = "/api/v1/campaign/:id/events"
	ROUTE_V1_CAMPAIGN_ALL_EVENTS         = "/api/v1/campaign/events"
	ROUTE_V1_CAMPAIGN_EVENT_ID           = "/api/v1/campaign/event/:id"
	ROUTE_V1_CAMPAIGN_EVENT_NAMES        = "/api/v1/campaign/event-types"
	ROUTE_V1_CAMPAIGN_STATS              = "/api/v1/campaign/statistics"
	ROUTE_V1_CAMPAIGN_STATS_ID           = "/api/v1/campaign/:id/stats"
	ROUTE_V1_CAMPAIGN_STATS_ALL          = "/api/v1/campaign/stats/all"
	ROUTE_V1_CAMPAIGN_STATS_CREATE       = "/api/v1/campaign/stats"
	ROUTE_V1_CAMPAIGN_STATS_MANUAL       = "/api/v1/campaign/stats/manual"
	ROUTE_V1_CAMPAIGN_STATS_UPDATE       = "/api/v1/campaign/stats/:id"
	ROUTE_V1_CAMPAIGN_STATS_DELETE       = "/api/v1/campaign/stats/:id"
	ROUTE_V1_CAMPAIGN_UPLOAD_REPORTED    = "/api/v1/campaign/:id/upload/reported"
	// campaign-recipient
	ROUTE_V1_CAMPAIGN_RECIPIENT_EMAIL      = "/api/v1/campaign/recipient/:id/email"
	ROUTE_V1_CAMPAIGN_RECIPIENT_URL        = "/api/v1/campaign/recipient/:id/url"
	ROUTE_V1_CAMPAIGN_RECIPIENT_SET_SENT   = "/api/v1/campaign/recipient/:id/sent"
	ROUTE_V1_CAMPAIGN_RECIPIENT_SEND_EMAIL = "/api/v1/campaign/recipient/:id/send"
	// asset
	ROUTE_V1_ASSET                = "/api/v1/asset"
	ROUTE_V1_ASSET_ID             = "/api/v1/asset/:id"
	ROUTE_V1_ASSET_DOMAIN_CONTEXT = "/api/v1/asset/domain/:domain"
	ROUTE_V1_ASSET_GLOBAL_CONTEXT = "/api/v1/asset/domain/"
	ROUTE_V1_ASSET_DOMAIN_VIEW    = "/api/v1/asset/view/domain/:domain/*path"
	// attachments
	ROUTE_V1_ATTACHMENT                 = "/api/v1/attachment"
	ROUTE_V1_ATTACHMENT_ID              = "/api/v1/attachment/:id"
	ROUTE_V1_ATTACHMENT_ID_CONTENT      = "/api/v1/attachment/:id/content"
	ROUTE_V1_ATTACHMENT_COMPANY_CONTEXT = "/api/v1/attachment/company/:companyID"
	ROUTE_V1_ATTACHMENT_GLOBAL_CONTEXT  = "/api/v1/attachment/company/"
	ROUTE_V1_EMAIL_ATTACHMENT           = "/api/v1/email/:id/attachment"
	// api sender
	ROUTE_V1_API_SENDER          = "/api/v1/api-sender"
	ROUTE_V1_API_SENDER_OVERVIEW = "/api/v1/api-sender/overview"
	ROUTE_V1_API_SENDER_ID       = "/api/v1/api-sender/:id"
	ROUTE_V1_API_SENDER_ID_TEST  = "/api/v1/api-sender/:id/test"
	// deny allow
	ROUTE_V1_ALLOW_DENY          = "/api/v1/allow-deny"
	ROUTE_V1_ALLOW_DENY_OVERVIEW = "/api/v1/allow-deny/overview"
	ROUTE_V1_ALLOW_DENY_ID       = "/api/v1/allow-deny/:id"
	// geoip
	ROUTE_V1_GEOIP_METADATA = "/api/v1/geoip/metadata"
	ROUTE_V1_GEOIP_LOOKUP   = "/api/v1/geoip/lookup"
	// web hooks
	ROUTE_V1_WEBHOOK         = "/api/v1/webhook"
	ROUTE_V1_WEBHOOK_ID      = "/api/v1/webhook/:id"
	ROUTE_V1_WEBHOOK_ID_TEST = "/api/v1/webhook/:id/test"
	// identifiers
	ROUTE_V1_IDENTIFIER = "/api/v1/identifier"
	// oauth providers
	ROUTE_V1_OAUTH_PROVIDER             = "/api/v1/oauth-provider"
	ROUTE_V1_OAUTH_PROVIDER_ID          = "/api/v1/oauth-provider/:id"
	ROUTE_V1_OAUTH_PROVIDER_REMOVE_AUTH = "/api/v1/oauth-provider/:id/remove-authorization"
	ROUTE_V1_OAUTH_AUTHORIZE            = "/api/v1/oauth-authorize/:id"
	ROUTE_V1_OAUTH_CALLBACK             = "/api/v1/oauth-callback"
	ROUTE_V1_OAUTH_IMPORT_TOKENS        = "/api/v1/oauth-provider/import-tokens"
	ROUTE_V1_OAUTH_EXPORT_TOKENS        = "/api/v1/oauth-provider/:id/export-tokens"
	// license
	ROUTE_V1_LICENSE = "/api/v1/license"
	// version
	ROUTE_V1_VERSION = "/api/v1/version"
	// import
	ROUTE_V1_IMPORT = "/api/v1/import"
)

// administrationServer is the administrationServer app
type administrationServer struct {
	Server          *http.Server
	router          *gin.Engine
	logger          *zap.SugaredLogger
	production      bool
	embedBackendFS  *embed.FS
	certMagicConfig *certmagic.Config
}

// NewAdministrationServer creates a new administration app
func NewAdministrationServer(
	router *gin.Engine,
	controllers *Controllers,
	middlewares *Middlewares,
	logger *zap.SugaredLogger,
	certMagicConfig *certmagic.Config,
	production bool,
) *administrationServer {
	router = setupRoutes(router, controllers, middlewares)

	return &administrationServer{
		router:          router,
		logger:          logger,
		production:      production,
		certMagicConfig: certMagicConfig,
	}
}

func (a *administrationServer) Router() *gin.Engine {
	return a.router
}

// setupRoutes sets up the routes for the administration app
func setupRoutes(
	r *gin.Engine,
	controllers *Controllers,
	middleware *Middlewares,
) *gin.Engine {

	if !build.Flags.Production {
		r.
			GET("/api/v1/_debug/panic", middleware.SessionHandler, controllers.Log.Panic).
			GET("/api/v1/_debug/slow", middleware.SessionHandler, controllers.Log.Slow)
	}

	r.
		// log
		GET(ROUTE_V1_LOG, middleware.SessionHandler, controllers.Log.GetLevel).
		POST(ROUTE_V1_LOG, middleware.SessionHandler, controllers.Log.SetLevel).
		GET(ROUTE_V1_LOG_TEST, middleware.SessionHandler, controllers.Log.TestLog).
		// application
		GET(ROUTE_V1_UPDATE_AVAILABLE, middleware.SessionHandler, controllers.Update.CheckForUpdate).
		GET(ROUTE_V1_UPDATE_AVAILABLE_CACHED, middleware.SessionHandler, controllers.Update.CheckForUpdateCached).
		// health
		GET(ROUTE_V1_HEALTH, controllers.Health.Health).
		GET(ROUTE_V1_LIVE, controllers.Health.Health).
		GET(ROUTE_V1_READY, controllers.Health.Health).
		// login, logout and session
		GET(ROUTE_V1_SESSION_PING, middleware.SessionHandler, controllers.User.SessionPing).
		POST(ROUTE_V1_USER_LOGIN, middleware.LoginRateLimiter, controllers.User.Login).
		POST(ROUTE_V1_USER_LOGOUT, controllers.User.Logout).
		// install
		POST(ROUTE_V1_INSTALL, middleware.SessionHandler, controllers.Installer.Install).
		POST(ROUTE_V1_INSTALL_TEMPLATES, middleware.SessionHandler, controllers.Installer.InstallTemplates).
		// user
		GET(ROUTE_V1_USER, middleware.SessionHandler, controllers.User.GetAll).
		GET(ROUTE_V1_USER_ID, middleware.SessionHandler, controllers.User.GetByID).
		POST(ROUTE_V1_USER_ID, middleware.SessionHandler, controllers.User.UpdateByID).
		POST(ROUTE_V1_USER, middleware.SessionHandler, controllers.User.Create).
		DELETE(ROUTE_V1_USER_ID, middleware.SessionHandler, controllers.User.Delete).
		POST(ROUTE_V1_USER_PASSWORD, middleware.SessionHandler, controllers.User.ChangePasswordOnLoggedInUser).
		POST(ROUTE_V1_USER_USERNAME, middleware.SessionHandler, controllers.User.ChangeUsernameOnLoggedInUser).
		POST(ROUTE_V1_USER_FULLNAME, middleware.SessionHandler, controllers.User.ChangeFullnameOnLoggedInUser).
		POST(ROUTE_V1_USER_EMAIL, middleware.SessionHandler, controllers.User.ChangeEmailOnLoggedInUser).
		GET(ROUTE_V1_USER_SESSIONS, middleware.SessionHandler, controllers.User.GetSessionsOnLoggedInUser).
		POST(ROUTE_V1_USER_SESSIONS_INVALIDATE, middleware.SessionHandler, controllers.User.InvalidateAllSessionByUserID).
		DELETE(ROUTE_V1_SESSION_ID, middleware.SessionHandler, controllers.User.ExpireSessionByID).
		GET(ROUTE_V1_USER_API, middleware.SessionHandler, controllers.User.GetMaskedAPIKey).
		POST(ROUTE_V1_USER_API, middleware.SessionHandler, controllers.User.UpsertAPIKey).
		DELETE(ROUTE_V1_USER_API, middleware.SessionHandler, controllers.User.RemoveAPIKey).
		// sso
		GET(ROUTE_V1_SSO_ENTRA_ID_ENABLED, controllers.SSO.IsEnabled).
		POST(ROUTE_V1_SSO_ENTRA_ID, middleware.SessionHandler, controllers.SSO.Upsert).
		GET(ROUTE_V1_SSO_ENTRA_ID_LOGIN, controllers.SSO.EntreIDLogin).
		GET(ROUTE_V1_SSO_ENTRA_ID_CALLBACK, controllers.SSO.EntreIDCallBack).
		// user mfa
		GET(ROUTE_V1_USER_MFA_TOTP, middleware.SessionHandler, controllers.User.IsTOTPEnabled).
		POST(ROUTE_V1_USER_MFA_TOTP_SETUP, middleware.LoginRateLimiter, middleware.SessionHandler, controllers.User.SetupTOTP).
		POST(ROUTE_V1_USER_MFA_TOTP_SETUP_VERIFY, middleware.LoginRateLimiter, middleware.SessionHandler, controllers.User.SetupVerifyTOTP).
		POST(ROUTE_V1_USER_MFA_TOTP_VERIFY, middleware.LoginRateLimiter, middleware.SessionHandler, controllers.User.VerifyTOTP).
		POST(ROUTE_V1_USER_MFA_TOTP, middleware.LoginRateLimiter, middleware.SessionHandler, controllers.User.DisableTOTP).
		// qr
		POST(ROUTE_V1_QR_FROM_TOTP, middleware.SessionHandler, controllers.QR.ToTOTPURL).
		POST(ROUTE_V1_QR_URL_TO_HTML, middleware.SessionHandler, controllers.QR.ToHTML).
		// company
		POST(ROUTE_V1_COMPANY, middleware.SessionHandler, controllers.Company.Create).
		POST(ROUTE_V1_COMPANY_ID, middleware.SessionHandler, controllers.Company.ChangeName).
		GET(ROUTE_V1_COMPANY, middleware.SessionHandler, controllers.Company.GetAll).
		GET(ROUTE_V1_COMPANY_ID_EXPORT, middleware.SessionHandler, controllers.Company.ExportByCompanyID).
		GET(ROUTE_V1_COMPANY_ID_EXPORT_SHARED, middleware.SessionHandler, controllers.Company.ExportShared).
		GET(ROUTE_V1_COMPANY_ID, middleware.SessionHandler, controllers.Company.GetByID).
		DELETE(ROUTE_V1_COMPANY_ID, middleware.SessionHandler, controllers.Company.DeleteByID).
		// options
		GET(ROUTE_V1_OPTION_GET, middleware.SessionHandler, controllers.Option.Get).
		POST(ROUTE_V1_OPTION, middleware.SessionHandler, middleware.SessionHandler, controllers.Option.Update).
		// domain
		GET(ROUTE_V1_DOMAIN, middleware.SessionHandler, controllers.Domain.GetAll).
		GET(ROUTE_V1_DOMAIN_SUBSET, middleware.SessionHandler, controllers.Domain.GetAllOverview).
		GET(ROUTE_V1_DOMAIN_SUBSET_NO_PROXIES, middleware.SessionHandler, controllers.Domain.GetAllOverviewWithoutProxies).
		GET(ROUTE_V1_DOMAIN_ID, middleware.SessionHandler, controllers.Domain.GetByID).
		GET(ROUTE_V1_DOMAIN_NAME, middleware.SessionHandler, controllers.Domain.GetByName).
		POST(ROUTE_V1_DOMAIN, middleware.SessionHandler, controllers.Domain.Create).
		POST(ROUTE_V1_DOMAIN_ID, middleware.SessionHandler, controllers.Domain.UpdateByID).
		DELETE(ROUTE_V1_DOMAIN_ID, middleware.SessionHandler, controllers.Domain.DeleteByID).
		// recipient
		GET(ROUTE_V1_RECIPIENT, middleware.SessionHandler, controllers.Recipient.GetAll).
		GET(ROUTE_V1_RECIPIENT_ID, middleware.SessionHandler, controllers.Recipient.GetByID).
		GET(ROUTE_V1_RECIPIENT_ID_EVENTS, middleware.SessionHandler, controllers.Recipient.GetCampaignEvents).
		GET(ROUTE_V1_RECIPIENT_ID_STATS, middleware.SessionHandler, controllers.Recipient.GetStatsByID).
		POST(ROUTE_V1_RECIPIENT, middleware.SessionHandler, controllers.Recipient.Create).
		POST(ROUTE_V1_RECIPIENT_IMPORT, middleware.SessionHandler, controllers.Recipient.Import).
		GET(ROUTE_V1_RECIPIENT_EXPORT, middleware.SessionHandler, controllers.Recipient.Export).
		PATCH(ROUTE_V1_RECIPIENT_ID, middleware.SessionHandler, controllers.Recipient.UpdateByID).
		DELETE(ROUTE_V1_RECIPIENT_ID, middleware.SessionHandler, controllers.Recipient.DeleteByID).
		GET(ROUTE_V1_RECIPIENT_REPEAT_OFFENDERS, middleware.SessionHandler, controllers.Recipient.GetRepeatOffenderCount).
		GET(ROUTE_V1_RECIPIENT_ORPHANED, middleware.SessionHandler, controllers.Recipient.GetOrphaned).
		DELETE(ROUTE_V1_RECIPIENT_ORPHANED_DELETE, middleware.SessionHandler, controllers.Recipient.DeleteAllOrphaned).
		// recipient group
		GET(ROUTE_V1_RECIPIENT_GROUP, middleware.SessionHandler, controllers.RecipientGroup.GetAll).
		GET(ROUTE_V1_RECIPIENT_GROUP_ID, middleware.SessionHandler, controllers.RecipientGroup.GetByID).
		GET(ROUTE_V1_RECIPIENT_GROUP_RECIPIENTS, middleware.SessionHandler, controllers.RecipientGroup.GetRecipientsByGroupID).
		POST(ROUTE_V1_RECIPIENT_GROUP_RECIPIENTS, middleware.SessionHandler, controllers.RecipientGroup.AddRecipients).
		DELETE(ROUTE_V1_RECIPIENT_GROUP_RECIPIENTS, middleware.SessionHandler, controllers.RecipientGroup.RemoveRecipients).
		POST(ROUTE_V1_RECIPIENT_GROUP, middleware.SessionHandler, controllers.RecipientGroup.Create).
		PATCH(ROUTE_V1_RECIPIENT_GROUP_ID, middleware.SessionHandler, controllers.RecipientGroup.UpdateByID).
		PUT(ROUTE_V1_RECIPIENT_GROUP_ID_IMPORT, middleware.SessionHandler, controllers.RecipientGroup.Import).
		DELETE(ROUTE_V1_RECIPIENT_GROUP_ID, middleware.SessionHandler, controllers.RecipientGroup.DeleteByID).
		// page
		GET(ROUTE_V1_PAGE, middleware.SessionHandler, controllers.Page.GetAll).
		GET(ROUTE_V1_PAGE_OVERVIEW, middleware.SessionHandler, controllers.Page.GetOverview).
		GET(ROUTE_V1_PAGE_ID, middleware.SessionHandler, controllers.Page.GetByID).
		Any(ROUTE_V1_PAGE_CONTENT_ID, middleware.SessionHandler, controllers.Page.GetContentByID).
		POST(ROUTE_V1_PAGE, middleware.SessionHandler, controllers.Page.Create).
		PATCH(ROUTE_V1_PAGE_ID, middleware.SessionHandler, controllers.Page.UpdateByID).
		DELETE(ROUTE_V1_PAGE_ID, middleware.SessionHandler, controllers.Page.DeleteByID).
		// proxy
		GET(ROUTE_V1_PROXY, middleware.SessionHandler, controllers.Proxy.GetAll).
		GET(ROUTE_V1_PROXY_OVERVIEW, middleware.SessionHandler, controllers.Proxy.GetOverview).
		GET(ROUTE_V1_PROXY_ID, middleware.SessionHandler, controllers.Proxy.GetByID).
		POST(ROUTE_V1_PROXY, middleware.SessionHandler, controllers.Proxy.Create).
		PATCH(ROUTE_V1_PROXY_ID, middleware.SessionHandler, controllers.Proxy.UpdateByID).
		DELETE(ROUTE_V1_PROXY_ID, middleware.SessionHandler, controllers.Proxy.DeleteByID).
		// ip allow list
		GET(ROUTE_V1_IP_ALLOW_LIST_PROXY_CONFIG, middleware.SessionHandler, controllers.IPAllowList.GetEntriesForProxyConfig).
		DELETE(ROUTE_V1_IP_ALLOW_LIST_CLEAR_PROXY_CONFIG, middleware.SessionHandler, controllers.IPAllowList.ClearForProxyConfig).
		// smtp configuration
		GET(ROUTE_V1_SMTP_CONFIGURATION, middleware.SessionHandler, controllers.SMTPConfiguration.GetAll).
		GET(ROUTE_V1_SMTP_CONFIGURATION_ID, middleware.SessionHandler, controllers.SMTPConfiguration.GetByID).
		POST(ROUTE_V1_SMTP_CONFIGURATION, middleware.SessionHandler, controllers.SMTPConfiguration.Create).
		POST(ROUTE_V1_SMTP_CONFIGURATION_ID_TEST_EMAIL, middleware.SessionHandler, controllers.SMTPConfiguration.TestEmail).
		PATCH(ROUTE_V1_SMTP_CONFIGURATION_ID, middleware.SessionHandler, controllers.SMTPConfiguration.UpdateByID).
		DELETE(ROUTE_V1_SMTP_CONFIGURATION_ID, middleware.SessionHandler, controllers.SMTPConfiguration.DeleteByID).
		// smtp configuration headers
		PATCH(ROUTE_V1_SMTP_CONFIGURATION_HEADERS, middleware.SessionHandler, controllers.SMTPConfiguration.AddHeader).
		DELETE(ROUTE_V1_SMTP_HEADER_ID, middleware.SessionHandler, controllers.SMTPConfiguration.RemoveHeader).
		// oauth providers
		GET(ROUTE_V1_OAUTH_PROVIDER, middleware.SessionHandler, controllers.OAuthProvider.GetAll).
		GET(ROUTE_V1_OAUTH_PROVIDER_ID, middleware.SessionHandler, controllers.OAuthProvider.GetByID).
		POST(ROUTE_V1_OAUTH_PROVIDER, middleware.SessionHandler, controllers.OAuthProvider.Create).
		PATCH(ROUTE_V1_OAUTH_PROVIDER_ID, middleware.SessionHandler, controllers.OAuthProvider.UpdateByID).
		DELETE(ROUTE_V1_OAUTH_PROVIDER_ID, middleware.SessionHandler, controllers.OAuthProvider.DeleteByID).
		POST(ROUTE_V1_OAUTH_PROVIDER_REMOVE_AUTH, middleware.SessionHandler, controllers.OAuthProvider.RemoveAuthorization).
		GET(ROUTE_V1_OAUTH_AUTHORIZE, middleware.SessionHandler, controllers.OAuthProvider.GetAuthorizationURL).
		GET(ROUTE_V1_OAUTH_CALLBACK, controllers.OAuthProvider.HandleCallback).
		POST(ROUTE_V1_OAUTH_IMPORT_TOKENS, middleware.SessionHandler, controllers.OAuthProvider.ImportAuthorizedTokens).
		GET(ROUTE_V1_OAUTH_EXPORT_TOKENS, middleware.SessionHandler, controllers.OAuthProvider.ExportAuthorizedTokens).
		// emails
		GET(ROUTE_V1_EMAIL, middleware.SessionHandler, controllers.Email.GetAll).
		GET(ROUTE_V1_EMAIL_OVERVIEW, middleware.SessionHandler, controllers.Email.GetOverviews).
		GET(ROUTE_V1_EMAIL_ID, middleware.SessionHandler, controllers.Email.GetByID).
		GET(ROUTE_V1_EMAIL_CONTENT_ID, middleware.SessionHandler, controllers.Email.GetContentByID).
		POST(ROUTE_V1_EMAIL_SEND_TEST, middleware.SessionHandler, controllers.Email.SendTestEmail).
		POST(ROUTE_V1_EMAIL, middleware.SessionHandler, controllers.Email.Create).
		// TODO PATCH
		POST(ROUTE_V1_EMAIL_ID, middleware.SessionHandler, controllers.Email.UpdateByID).
		DELETE(ROUTE_V1_EMAIL_ID, middleware.SessionHandler, controllers.Email.DeleteByID).
		// email attachments
		POST(ROUTE_V1_EMAIL_ATTACHMENT, middleware.SessionHandler, controllers.Email.AddAttachments).
		DELETE(ROUTE_V1_EMAIL_ATTACHMENT, middleware.SessionHandler, controllers.Email.RemoveAttachment).
		// campaign templates
		GET(ROUTE_V1_CAMPAIGN_TEMPLATE, middleware.SessionHandler, controllers.CampaignTemplate.GetAll).
		GET(ROUTE_v1_CAMPAIGN_TEMPLATE_ID, middleware.SessionHandler, controllers.CampaignTemplate.GetByID).
		// TODO PATCH
		POST(ROUTE_V1_CAMPAIGN_TEMPLATE, middleware.SessionHandler, controllers.CampaignTemplate.Create).
		POST(ROUTE_v1_CAMPAIGN_TEMPLATE_ID, middleware.SessionHandler, controllers.CampaignTemplate.UpdateByID).
		DELETE(ROUTE_v1_CAMPAIGN_TEMPLATE_ID, middleware.SessionHandler, controllers.CampaignTemplate.DeleteByID).
		// campaigns
		GET(ROUTE_V1_CAMPAIGN, middleware.SessionHandler, controllers.Campaign.GetAll).
		GET(ROUTE_V1_CAMPAIGN_CALENDAR, middleware.SessionHandler, controllers.Campaign.GetAllWithinDates).
		GET(ROUTE_V1_CAMPAIGN_ACTIVE, middleware.SessionHandler, controllers.Campaign.GetAllActive).
		GET(ROUTE_V1_CAMPAIGN_UPCOMING, middleware.SessionHandler, controllers.Campaign.GetAllUpcoming).
		GET(ROUTE_V1_CAMPAIGN_FINISHED, middleware.SessionHandler, controllers.Campaign.GetAllFinished).
		GET(ROUTE_V1_CAMPAIGN_EVENT_NAMES, middleware.SessionHandler, controllers.Campaign.GetAllEventTypes).
		GET(ROUTE_V1_CAMPAIGN_ALL_EVENTS, middleware.SessionHandler, controllers.Campaign.GetAllEvents).
		GET(ROUTE_V1_CAMPAIGN_EVENTS, middleware.SessionHandler, controllers.Campaign.GetEventsByCampaignID).
		DELETE(ROUTE_V1_CAMPAIGN_EVENT_ID, middleware.SessionHandler, controllers.Campaign.DeleteEventByID).
		GET(ROUTE_V1_CAMPAIGN_STATS, middleware.SessionHandler, controllers.Campaign.GetStats).
		GET(ROUTE_V1_CAMPAIGN_RESULT_STATS, middleware.SessionHandler, controllers.Campaign.GetResultStats).
		GET(ROUTE_V1_CAMPAIGN_STATS_ID, middleware.SessionHandler, controllers.Campaign.GetCampaignStats).
		GET(ROUTE_V1_CAMPAIGN_STATS_ALL, middleware.SessionHandler, controllers.Campaign.GetAllCampaignStats).
		POST(ROUTE_V1_CAMPAIGN_STATS_CREATE, middleware.SessionHandler, controllers.Campaign.CreateCampaignStats).
		GET(ROUTE_V1_CAMPAIGN_STATS_MANUAL, middleware.SessionHandler, controllers.Campaign.GetManualCampaignStats).
		PUT(ROUTE_V1_CAMPAIGN_STATS_UPDATE, middleware.SessionHandler, controllers.Campaign.UpdateCampaignStats).
		DELETE(ROUTE_V1_CAMPAIGN_STATS_DELETE, middleware.SessionHandler, controllers.Campaign.DeleteCampaignStatsManual).
		GET(ROUTE_V1_CAMPAIGN_ID, middleware.SessionHandler, controllers.Campaign.GetByID).
		GET(ROUTE_V1_CAMPAIGN_NAME, middleware.SessionHandler, controllers.Campaign.GetByName).
		POST(ROUTE_V1_CAMPAIGN, middleware.SessionHandler, controllers.Campaign.Create).
		// TODO PATCH
		POST(ROUTE_V1_CAMPAIGN_ID, middleware.SessionHandler, controllers.Campaign.UpdateByID).
		POST(ROUTE_V1_CAMPAIGN_CLOSE, middleware.SessionHandler, controllers.Campaign.CloseCampaignByID).
		GET(ROUTE_V1_CAMPAIGN_EXPORT_EVENTS, middleware.SessionHandler, controllers.Campaign.ExportEventsAsCSV).
		GET(ROUTE_V1_CAMPAIGN_EXPORT_SUBMISSIONS, middleware.SessionHandler, controllers.Campaign.ExportSubmissionsAsCSV).
		POST(ROUTE_V1_CAMPAIGN_UPLOAD_REPORTED, middleware.SessionHandler, controllers.Campaign.UploadReportedCSV).
		POST(ROUTE_V1_CAMPAIGN_ANONYMIZE, middleware.SessionHandler, controllers.Campaign.AnonymizeByID).
		DELETE(ROUTE_V1_CAMPAIGN_ID, middleware.SessionHandler, controllers.Campaign.DeleteByID).
		// campaign-recipient
		GET(ROUTE_V1_CAMPAIGN_RECIPIENTS, middleware.SessionHandler, controllers.Campaign.GetRecipientsByCampaignID).
		GET(ROUTE_V1_CAMPAIGN_RECIPIENT_EMAIL, middleware.SessionHandler, controllers.Campaign.GetCampaignEmail).
		GET(ROUTE_V1_CAMPAIGN_RECIPIENT_URL, middleware.SessionHandler, controllers.Campaign.GetCampaignURL).
		POST(ROUTE_V1_CAMPAIGN_RECIPIENT_SET_SENT, middleware.SessionHandler, controllers.Campaign.SetSentAtByCampaignRecipientID).
		POST(ROUTE_V1_CAMPAIGN_RECIPIENT_SEND_EMAIL, middleware.SessionHandler, controllers.Campaign.SendEmailByCampaignRecipientID).
		// asset
		GET(ROUTE_V1_ASSET_DOMAIN_VIEW, middleware.SessionHandler, controllers.Asset.GetContentByID).
		GET(ROUTE_V1_ASSET_ID, middleware.SessionHandler, controllers.Asset.GetByID).
		PATCH(ROUTE_V1_ASSET_ID, middleware.SessionHandler, controllers.Asset.UpdateByID).
		GET(ROUTE_V1_ASSET_DOMAIN_CONTEXT, middleware.SessionHandler, controllers.Asset.GetAllForContext).
		GET(ROUTE_V1_ASSET_GLOBAL_CONTEXT, middleware.SessionHandler, controllers.Asset.GetAllForContext).
		POST(ROUTE_V1_ASSET, middleware.SessionHandler, controllers.Asset.Create).
		DELETE(ROUTE_V1_ASSET_ID, middleware.SessionHandler, controllers.Asset.RemoveByID).
		// attachments
		POST(ROUTE_V1_ATTACHMENT, middleware.SessionHandler, controllers.Attachment.Create).
		GET(ROUTE_V1_ATTACHMENT_ID, middleware.SessionHandler, controllers.Attachment.GetByID).
		GET(ROUTE_V1_ATTACHMENT_ID_CONTENT, middleware.SessionHandler, controllers.Attachment.GetContentByID).
		GET(ROUTE_V1_ATTACHMENT, middleware.SessionHandler, controllers.Attachment.GetAllForContext).
		PATCH(ROUTE_V1_ATTACHMENT_ID, middleware.SessionHandler, controllers.Attachment.UpdateByID).
		DELETE(ROUTE_V1_ATTACHMENT_ID, middleware.SessionHandler, controllers.Attachment.RemoveByID).
		// api sender
		GET(ROUTE_V1_API_SENDER, middleware.SessionHandler, controllers.APISender.GetAll).
		GET(ROUTE_V1_API_SENDER_OVERVIEW, middleware.SessionHandler, controllers.APISender.GetAllOverview).
		GET(ROUTE_V1_API_SENDER_ID, middleware.SessionHandler, controllers.APISender.GetByID).
		POST(ROUTE_V1_API_SENDER, middleware.SessionHandler, controllers.APISender.Create).
		PATCH(ROUTE_V1_API_SENDER_ID, middleware.SessionHandler, controllers.APISender.UpdateByID).
		POST(ROUTE_V1_API_SENDER_ID_TEST, middleware.SessionHandler, controllers.APISender.SendTest).
		DELETE(ROUTE_V1_API_SENDER_ID, middleware.SessionHandler, controllers.APISender.DeleteByID).
		// allow deny
		GET(ROUTE_V1_ALLOW_DENY, middleware.SessionHandler, controllers.AllowDeny.GetAll).
		GET(ROUTE_V1_ALLOW_DENY_OVERVIEW, middleware.SessionHandler, controllers.AllowDeny.GetAllOverview).
		GET(ROUTE_V1_ALLOW_DENY_ID, middleware.SessionHandler, controllers.AllowDeny.GetByID).
		POST(ROUTE_V1_ALLOW_DENY, middleware.SessionHandler, controllers.AllowDeny.Create).
		PATCH(ROUTE_V1_ALLOW_DENY_ID, middleware.SessionHandler, controllers.AllowDeny.UpdateByID).
		DELETE(ROUTE_V1_ALLOW_DENY_ID, middleware.SessionHandler, controllers.AllowDeny.DeleteByID).
		// geoip
		GET(ROUTE_V1_GEOIP_METADATA, middleware.SessionHandler, controllers.GeoIP.GetMetadata).
		GET(ROUTE_V1_GEOIP_LOOKUP, middleware.SessionHandler, controllers.GeoIP.Lookup).
		// web hooks
		GET(ROUTE_V1_WEBHOOK, middleware.SessionHandler, controllers.Webhook.GetAll).
		GET(ROUTE_V1_WEBHOOK_ID, middleware.SessionHandler, controllers.Webhook.GetByID).
		POST(ROUTE_V1_WEBHOOK, middleware.SessionHandler, controllers.Webhook.Create).
		PATCH(ROUTE_V1_WEBHOOK_ID, middleware.SessionHandler, controllers.Webhook.UpdateByID).
		DELETE(ROUTE_V1_WEBHOOK_ID, middleware.SessionHandler, controllers.Webhook.DeleteByID).
		POST(ROUTE_V1_WEBHOOK_ID_TEST, middleware.SessionHandler, controllers.Webhook.SendTest).
		// identifiers
		GET(ROUTE_V1_IDENTIFIER, middleware.SessionHandler, controllers.Identifier.GetAll).
		// version
		GET(ROUTE_V1_VERSION, middleware.SessionHandler, controllers.Version.Get).
		// update
		GET(ROUTE_V1_UPDATE, middleware.SessionHandler, controllers.Update.GetUpdateDetails).
		POST(ROUTE_V1_UPDATE, middleware.ExtendedTimeout(3*time.Minute), middleware.SessionHandler, controllers.Update.RunUpdate).
		// backup
		POST(ROUTE_V1_BACKUP_CREATE, middleware.SessionHandler, controllers.Backup.CreateBackup).
		GET(ROUTE_V1_BACKUP_LIST, middleware.SessionHandler, controllers.Backup.ListBackups).
		GET(ROUTE_V1_BACKUP_DOWNLOAD, middleware.SessionHandler, controllers.Backup.DownloadBackup).
		// import
		POST(ROUTE_V1_IMPORT, middleware.SessionHandler, controllers.Import.Import)

	return r
}

func (a *administrationServer) handleTLSCertificate(
	conf *config.Config,
) error {
	publicCertExists := true
	privateCertExists := true
	if _, err := os.Stat(conf.TLSCertPath()); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		privateCertExists = false
	}
	if _, err := os.Stat(conf.TLSKeyPath()); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		publicCertExists = false
	}

	// determine hostnames to include in the certificate
	hostnames := []string{}
	if h := conf.TLSHost(); len(h) > 0 {
		hostnames = append(hostnames, h)
	}
	// get the address from config
	if conf.AdminNetAddress() != "" {
		host, _, err := net.SplitHostPort(conf.AdminNetAddress())
		if err == nil && host != "" && host != "0.0.0.0" && host != "::" {
			hostnames = append(hostnames, host)
		}
	}

	// try to get all non-loopback IP addresses
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				ip := ipnet.IP

				// skip private IPs (RFC 1918)
				if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					continue
				}

				// only add public IPs to the certificate
				hostnames = append(hostnames, ip.String())
			}
		}
	}

	needToCreateCert := !privateCertExists || !publicCertExists

	// check if we need to recreate the certificate because host/IP has changed
	if privateCertExists && publicCertExists {
		// read the existing certificate to check the hostnames
		certData, err := os.ReadFile(conf.TLSCertPath())
		if err == nil {
			block, _ := pem.Decode(certData)
			if block != nil && block.Type == "CERTIFICATE" {
				cert, err := x509.ParseCertificate(block.Bytes)
				if err == nil {
					// vheck if all requested hostnames are in the certificate
					missingHosts := false
					hostMap := make(map[string]bool)

					// add all current certificate SANs to the map
					for _, dnsName := range cert.DNSNames {
						hostMap[dnsName] = true
					}
					for _, ip := range cert.IPAddresses {
						hostMap[ip.String()] = true
					}

					// check if the common name is in our hostnames
					if cert.Subject.CommonName != "" {
						hostMap[cert.Subject.CommonName] = true
					}

					// Check if all requested hostnames are covered
					for _, host := range hostnames {
						if !hostMap[host] {
							missingHosts = true
							a.logger.Debugw("host not found in existing certificate", "host", host)
							break
						}
					}

					// if the TLSHost is specified and not in the certificate, or other hosts are missing, regenerate
					if missingHosts {
						a.logger.Debug("recreating certificate due to changed host/IP configuration")
						needToCreateCert = true
					}
				} else {
					a.logger.Warnw("could not parse existing certificate, will recreate", "error", err)
					needToCreateCert = true
				}
			} else {
				a.logger.Warn("invalid certificate format, will recreate")
				needToCreateCert = true
			}
		} else {
			a.logger.Warnw("could not read existing certificate, will recreate", "error", err)
			needToCreateCert = true
		}
	}

	// create certificates if needed
	if needToCreateCert {
		a.logger.Debug("creating self signed certificate for administration server")

		info := acme.NewInformationWithDefault()
		if len(hostnames) > 0 {
			info.CommonName = hostnames[0]
		}

		a.logger.Debugw("generating certificate with hostnames", "hostnames", hostnames)

		err = acme.CreateSelfSignedCert(
			a.logger,
			info,
			hostnames,
			conf.TLSCertPath(),
			conf.TLSKeyPath(),
		)

		if err != nil {
			return fmt.Errorf("failed to create self signed certificate: %s", err)
		}

		a.logger.Debugw(
			"saved self signed certificate for administration servers",
			"TLS certificate", conf.TLSCertPath(),
			"TLS key path", conf.TLSKeyPath(),
		)
	} else {
		a.logger.Debug("using existing certificate for administration server")
	}

	return nil
}

// LoadFrontend loads the frontend
// if this is a production build, the fronten will be embedded
// else the routes will be setup to load the frontend resources on every request
func (a *administrationServer) LoadFrontend(
	ln net.Listener,
) error {
	if build.Flags.Production {
		return a.loadEmbeddedFileSystem(
			ln,
		)
	}
	return a.loadPerRequestLoading()
}

// loadPerRequestLoading loads the frontend resources on every request
// this is only used in a dev enviroment using nodemon as is a
// backup if the current vite proxy stragegy does not work.
func (a *administrationServer) loadPerRequestLoading() error {
	a.router.GET("/", func(c *gin.Context) {
		c.File("./frontend/website/build/index.html")
	})
	// perform manual lookup for the frontend files on each request
	// build files might have been added or removed, so each request must
	// do a check if the file exists
	a.router.NoRoute(func(c *gin.Context) {
		// a.logger.Infow("serving frontend file", "path", c.Request.URL.Path)
		// check if the request url path exists in the root directory
		if _, err := os.Stat("./frontend/website/build" + c.Request.URL.Path); err == nil {
			c.File("./frontend/website/build" + c.Request.URL.Path)
			return
		}
		// if the path ends with / or does not have a file extension, then it should fallback to index.html as
		// it is a SPA path such as /company/foo/
		if c.Request.URL.Path[len(c.Request.URL.Path)-1:] == "/" || !strings.Contains(c.Request.URL.Path, ".") {
			c.File("./frontend/website/build/index.html")
		}
		// file not found - return 404
		c.AbortWithStatus(http.StatusNotFound)
	})
	return nil
}

func (a *administrationServer) loadEmbeddedFileSystem(
	ln net.Listener,
) error {
	_ = ln
	embedFS := frontend.GetEmbededFS()
	// make embedded .html work
	frontend.LoadHTMLFromEmbedFS(a.router, *embedFS, "build/*.html")
	rootDir, err := embedFS.ReadDir("build")
	if err != nil {
		return errs.Wrap(err)
	}
	for _, entry := range rootDir {
		path := entry.Name()
		// add root files
		if !entry.IsDir() {
			// special case for the frontpage
			if path == "index.html" {
				a.router.GET("/", func(c *gin.Context) {
					c.HTML(http.StatusOK, "build/index.html", nil)
				})
				continue
			}
			// any file in the root folder gets server as a file
			a.router.GET("/"+path, func(c *gin.Context) {
				c.FileFromFS("build/"+path, http.FS(*embedFS))
			})
			continue
		}
		// add static folders
		staticFS, err := fs.Sub(embedFS, "build/"+path)
		if err != nil {
			return errs.Wrap(err)
		}
		switch path {
		case ".well-known":
			fallthrough
		case "_app":
			a.router.StaticFS(path, http.FS(staticFS))
		}
	}
	// fall back to the root index.html
	a.router.NoRoute(func(c *gin.Context) {
		c.HTML(http.StatusOK, "build/index.html", nil)
	})

	return nil
}

func (a *administrationServer) StartServer(
	conf *config.Config,
) (chan server.StartupMessage, net.Listener, error) {
	startupMessage := server.NewStartupMessageChannel()
	ln, err := net.Listen("tcp", conf.AdminNetAddress())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %s due to: %s", conf.AdminNetAddress(), err)
	}
	err = a.LoadFrontend(ln)
	if err != nil {
		return nil, nil, errs.Wrap(err)
	}
	err = a.handleTLSCertificate(conf)
	if err != nil {
		return nil, nil, errs.Wrap(err)
	}

	a.Server = &http.Server{
		Handler: a.router,
		// The maximum duration for reading the entire request, including the request line, headers, and body
		ReadTimeout: 15 * time.Second,
		// The maximum duration for writing the entire response, including the response headers and body
		WriteTimeout: 15 * time.Second, // Timeout for writing the response
		// The maximum duration to wait for the next request when the connection is in the idle state
		IdleTimeout: 10 * time.Second,
		// The maximum duration for reading the request headers.
		ReadHeaderTimeout: 2 * time.Second,
		// Maximum size of request headers (512 KB)
		MaxHeaderBytes: 1 << 19,
	}
	a.Server.ErrorLog = log.New(
		&SkipFirstTlsToZapWriter{
			logger:    a.logger,
			serverPtr: a.Server,
		}, "", 0,
	)

	a.logger.Debugw("TLS settings",
		"certPath", conf.TLSCertPath(),
		"certKeyPath", conf.TLSKeyPath(),
	)

	// start the administration server
	adminHost := "admin.test"
	err = a.certMagicConfig.ManageSync(context.Background(), []string{adminHost})
	if err != nil {
		a.logger.Errorw("certmagic managesync failed", "error", err)
		return nil, nil, errs.Wrap(err)
	}
	go func() {
		if !conf.TLSAuto() {
			a.logger.Debugw("starting administration",
				"address", ln.Addr().String(),
			)
			err := a.Server.ServeTLS(
				ln,
				conf.TLSCertPath(),
				conf.TLSKeyPath(),
			)
			if err != nil && err != http.ErrServerClosed {
				log.Fatalf("failed to start administration server due to: %s", err)
			}
		} else {
			// Setup TLS config from CertMagic
			tlsConfig := a.certMagicConfig.TLSConfig()
			tlsConfig.NextProtos = append([]string{"h2", "http/1.1"}, tlsConfig.NextProtos...)

			// Create new TLS listener with the config
			tlsLn := tls.NewListener(ln, tlsConfig)
			a.logger.Debugw("starting administration with automatic TLS",
				"address", ln.Addr().String(),
				"domain", adminHost,
			)
			err := a.Server.Serve(tlsLn)
			if err != nil && err != http.ErrServerClosed {
				log.Fatalf("failed to start administration server due to: %s", err)
			}
		}
	}()

	// test the connection to the administration server
	// and send a startup message
	// TODO the connectivity check has been disabled as it fucks up the auto tls
	// as it calls the certmagic DecisionFunc from addreses such as ::1 and I am not
	// sure we it is safe to allow list all of them or if I know all of the potential addresses.
	/*
		go func() {
			a.logger.Debug("testing connectivity to administration server...")
			// wait for connection to the server
			attempts := 1
			for {
				dialer := &net.Dialer{
					Timeout:   time.Second,
					KeepAlive: time.Second,
				}
				conn, err := tls.DialWithDialer(
					dialer,
					"tcp",
					ln.Addr().String(),
					&tls.Config{
						InsecureSkipVerify: true,
					},
				)
				if err != nil {
					a.logger.Debugw("failed to connect to administration server",
						"attempt", attempts,
					)
					time.Sleep(1 * time.Second)
					if attempts == 3 {
						startupMessage <- server.NewStartupMessage(
							false,
							fmt.Errorf("failed to connect to administration server"),
						)
						break
					}
					attempts += 1
					continue
				}
				conn.Close()
				startupMessage <- server.NewStartupMessage(true, nil)
				break
			}
		}()
	*/
	startupMessage <- server.NewStartupMessage(true, nil)

	return startupMessage, ln, nil
}

// https://stackoverflow.com/questions/52294334/net-http-set-custom-logger
type fwdToZapWriter struct {
	logger *zap.SugaredLogger
}

func (fw *fwdToZapWriter) Write(p []byte) (n int, err error) {
	fw.logger.Errorw(string(p))
	return len(p), nil
}

// SkipFirstTlsToZapWriter is a weird Writer that replaces itself
// when it has seen a TLS handshake error it is used for handling
// a special annoying case where a health check on startup creates
// a tls handshake that we want to ignore
type SkipFirstTlsToZapWriter struct {
	logger *zap.SugaredLogger
	// ignore first tls
	serverPtr *http.Server
}

func (fw *SkipFirstTlsToZapWriter) Write(p []byte) (n int, err error) {
	if strings.Contains(string(p), "TLS handshake error") {
		// After catching the first TLS error, replace the ErrorLog with direct logger
		fw.serverPtr.ErrorLog = log.New(
			&fwdToZapWriter{
				logger: fw.logger,
			},
			"",
			0,
		)
		return len(p), nil
	}
	fw.logger.Errorw(string(p))
	return len(p), nil
}

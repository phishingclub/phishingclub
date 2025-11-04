package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	textTmpl "text/template"
	"time"

	"github.com/go-errors/errors"
	"gopkg.in/yaml.v3"

	"github.com/caddyserver/certmagic"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/proxy"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/server"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const MAX_USER_AGENT_SAVED = 255

// Server is the public phishing server
type Server struct {
	HTTPServer            *http.Server
	HTTPSServer           *http.Server
	db                    *gorm.DB
	logger                *zap.SugaredLogger
	certMagicConfig       *certmagic.Config
	staticPath            string
	ownManagedTLSCertPath string
	controllers           *Controllers
	services              *Services
	repositories          *Repositories
	proxyServer           *proxy.ProxyHandler
}

// NewServer returns a new server
func NewServer(
	staticPath string,
	ownManagedTLSCertPath string,
	db *gorm.DB,
	controllers *Controllers,
	services *Services,
	repositories *Repositories,
	logger *zap.SugaredLogger,
	certMagicConfig *certmagic.Config,
) *Server {
	// setup goproxy-based proxy server
	proxyServer := proxy.NewProxyHandler(
		logger,
		services.ProxySessionManager,
		repositories.Page,
		repositories.CampaignRecipient,
		repositories.Campaign,
		repositories.CampaignTemplate,
		repositories.Domain,
		repositories.Proxy,
		repositories.Identifier,
		services.Campaign,
		services.Template,
		services.IPAllowList,
		repositories.Option,
	)

	// setup proxy session cleanup routine
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				proxyServer.CleanupExpiredSessions()
			}
		}
	}()

	return &Server{
		staticPath:            staticPath,
		ownManagedTLSCertPath: ownManagedTLSCertPath,
		db:                    db,
		controllers:           controllers,
		services:              services,
		repositories:          repositories,
		logger:                logger,
		certMagicConfig:       certMagicConfig,
		proxyServer:           proxyServer,
	}
}

// defaultServer creates a new default HTTP server
// skipFirstTLS sets a writer that ignores the first TLS handshake error and then
// replaces the logger with the normal logger, this is a hack to fix a annoying output
// created from the port ready probing done while booting the application
func (s *Server) defaultServer(handler http.Handler, skipFirstTLS bool) *http.Server {
	server := &http.Server{
		Handler: handler,
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
		ErrorLog:       log.New(&fwdToZapWriter{logger: s.logger}, "", 0),
	}
	if skipFirstTLS {
		server.ErrorLog = log.New(
			&SkipFirstTlsToZapWriter{
				logger:    s.logger,
				serverPtr: server,
			}, "", 0,
		)
	}
	return server
}

// host extract the host part of the request
func (s *Server) getHostOnly(host string) (string, error) {
	if strings.Contains(host, ":") {
		hostOnly, _, err := net.SplitHostPort(host)
		if err != nil {
			return "", errs.Wrap(err)
		}
		return hostOnly, nil
	}
	return host, nil
}

// testConnection tests the connection to the server
// it starts a gorutine that attempts to connect via. tcp 3 times and
// it returns a channel that will be called with the result
func (s *Server) testTCPConnection(identifier string, addr string) chan server.StartupMessage {
	c := server.NewStartupMessageChannel()
	go func() {
		s.logger.Debugw("testing connection",
			"server", identifier,
		)
		attempts := 1
		for {
			dialer := &net.Dialer{
				Timeout:   time.Second,
				KeepAlive: time.Second,
			}
			conn, err := dialer.Dial("tcp", addr)
			if err != nil {
				s.logger.Debugw(
					"failed to connect to server",
					"server", identifier,
					"attempt", attempts,
					"error", err,
				)
				time.Sleep(1 * time.Second)
				if attempts == 3 {
					c <- server.NewStartupMessage(
						false,
						fmt.Errorf("failed to connect to %s server", identifier),
					)
					break
				}
				attempts += 1
				continue
			}
			// #nosec
			conn.Close()
			c <- server.NewStartupMessage(true, nil)
			break
		}

	}()
	return c
}

// checkAndServeAssets checks if the request is for static content
// and serves it if it is
// return true if the request was for static content
func (s *Server) checkAndServeAssets(c *gin.Context, host string) bool {
	// create root filesystem for asset validation
	root, err := os.OpenRoot(s.staticPath)
	if err != nil {
		s.logger.Infow("failed to open static path root",
			"error", err,
		)
		return false
	}
	defer root.Close()

	// validate host folder path is safe
	_, err = root.Stat(host)
	if err != nil && !os.IsNotExist(err) {
		s.logger.Infow("insecure host path attempted",
			"host", host,
			"error", err,
		)
		return false
	}

	// clean path and remove leading slash
	cleanPath := strings.TrimPrefix(filepath.Clean(c.Request.URL.Path), "/")

	// validate full path is safe by checking it against root
	fullRelativePath := filepath.Join(host, cleanPath)
	_, err = root.Stat(fullRelativePath)
	if err != nil && !os.IsNotExist(err) {
		s.logger.Infow("insecure path attempted on asset",
			"path", fullRelativePath,
			"error", err,
		)
		return false
	}

	// check if file exists and get info through root
	fullRelativePathForFile := filepath.Join(host, cleanPath)
	info, err := root.Stat(fullRelativePathForFile)
	if err != nil {
		s.logger.Debugw("not found on domain: %s",
			"path", fullRelativePathForFile,
		)
		// check if this is a global asset
		return s.checkAndServeSharedAsset(c)
	}
	if info.IsDir() {
		return false
	}

	// open and serve file through root to maintain security boundaries
	file, err := root.Open(fullRelativePathForFile)
	if err != nil {
		s.logger.Infow("failed to open file through root",
			"path", fullRelativePathForFile,
			"error", err,
		)
		return false
	}
	defer file.Close()

	c.Header("Content-Type", mime.TypeByExtension(filepath.Ext(cleanPath)))
	c.DataFromReader(http.StatusOK, info.Size(), mime.TypeByExtension(filepath.Ext(cleanPath)), file, nil)
	return true
}

func (s *Server) checkAndServeSharedAsset(c *gin.Context) bool {
	// create root filesystem for secure shared asset validation
	root, err := os.OpenRoot(s.staticPath)
	if err != nil {
		s.logger.Infow("failed to open static path root",
			"error", err,
		)
		return false
	}
	defer root.Close()

	// validate shared folder path is safe
	_, err = root.Stat("shared")
	if err != nil && !os.IsNotExist(err) {
		s.logger.Infow("insecure shared path",
			"error", err,
		)
		return false
	}

	// clean path and remove leading slash
	cleanPath := strings.TrimPrefix(filepath.Clean(c.Request.URL.Path), "/")

	// validate full path is safe by checking it against root
	fullRelativePath := filepath.Join("shared", cleanPath)
	_, err = root.Stat(fullRelativePath)
	if err != nil && !os.IsNotExist(err) {
		s.logger.Infow("insecure shared asset path",
			"path", fullRelativePath,
			"error", err,
		)
		return false
	}

	// check if file exists and get info through root
	sharedRelativePath := filepath.Join("shared", cleanPath)
	info, err := root.Stat(sharedRelativePath)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}

	// open and serve file through root to maintain security boundaries
	file, err := root.Open(sharedRelativePath)
	if err != nil {
		s.logger.Infow("failed to open shared file through root",
			"path", sharedRelativePath,
			"error", err,
		)
		return false
	}
	defer file.Close()

	c.Header("Content-Type", mime.TypeByExtension(filepath.Ext(cleanPath)))
	c.DataFromReader(http.StatusOK, info.Size(), mime.TypeByExtension(filepath.Ext(cleanPath)), file, nil)
	return true
}

// Handler is middleware that takes care of everything related to incoming phishing requests
// checks if the domain is valid and usable
// checks if the request is for a phishing page
// checks if the request is for a assets
// checks if the request should be redirected
// checks if the request is for a static page or static not found page
func (s *Server) Handler(c *gin.Context) {
	// add error recovery for handler
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorw("panic in handler",
				"panic", r,
				"host", c.Request.Host,
				"url", c.Request.URL.String(),
				"method", c.Request.Method,
				"userAgent", c.Request.UserAgent(),
				"remoteAddr", c.Request.RemoteAddr,
				"stack", string(debug.Stack()),
			)
			c.Status(http.StatusInternalServerError)
			c.Abort()
		}
	}()

	host, err := s.getHostOnly(c.Request.Host)
	if err != nil {
		s.logger.Debugw("failed to parse host",
			"rawHost", c.Request.Host,
			"error", err,
		)
		c.Status(http.StatusNotFound)
		c.Abort()
		return
	}

	// check if the domain is valid
	// use DB directly here to avoid getting unnecessary data
	// as a domain contains big blobs for static content
	var domain *database.Domain
	res := s.db.
		Select("id, name, type, proxy_id, proxy_target_domain, host_website, redirect_url").
		Where("name = ?", host).
		First(&domain)

	if res.RowsAffected == 0 {
		s.logger.Debug("domain not found")
		c.Status(http.StatusNotFound)
		c.Abort()
		return
	}

	// check if this is a proxy domain - if so, handle it with proxy server
	if domain.Type == "proxy" {
		s.logger.Debugw("handling proxy domain request",
			"host", host,
			"targetDomain", domain.ProxyTargetDomain,
			"path", c.Request.URL.Path,
		)
		err = s.proxyServer.HandleHTTPRequest(c.Writer, c.Request, domain)
		if err != nil {
			s.logger.Errorw("failed to handle proxy request",
				"error", err,
				"host", host,
			)
			c.Status(http.StatusInternalServerError)
		}
		c.Abort()
		return
	}

	// check if the request is for a tacking pixel
	if c.Request.URL.Path == "/wf/open" {
		s.controllers.Campaign.TrackingPixel(c)
		c.Abort()
		return
	}

	// check if the request is for a phishing page or is denied by allow/deny list
	// this must come BEFORE proxy cookie check to ensure initial requests with campaign recipient IDs
	// are treated as initial requests even if they have existing proxy cookies
	isRequestForPhishingPageOrDenied, err := s.checkAndServePhishingPage(c, domain)
	if err != nil {
		s.logger.Errorw("failed to serve phishing page",
			"error", err,
		)
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	// if this was a request for the phishing page and there was no error
	if isRequestForPhishingPageOrDenied {
		return
	}

	// check for proxy cookie - only if this wasn't a phishing page request
	// this ensures that requests with campaign recipient IDs are handled as initial requests
	if s.proxyServer.IsValidProxyCookie(s.getProxyCookieValue(c)) {
		err = s.proxyServer.HandleHTTPRequest(c.Writer, c.Request, domain)
		if err != nil {
			s.logger.Errorw("failed to handle proxy request",
				"error", err,
			)
			c.Status(http.StatusInternalServerError)
		}
		c.Abort()
		return
	}
	// check if the request is for assets
	servedAssets := s.checkAndServeAssets(c, host)
	if servedAssets {
		s.logger.Debug("served static asset")
		c.Abort()
		return
	}
	// check if the request should be redirected
	if domain.RedirectURL != "" {
		c.Redirect(http.StatusMovedPermanently, domain.RedirectURL)
		c.Abort()
		return
	}
	// check if the domain should serve static content
	if !domain.HostWebsite {
		s.logger.Debugw("404 - Domain does not serve static content",
			"host", host,
		)
		c.Status(http.StatusNotFound)
		c.Abort()
		return
	}
	// check if the path is a OK page or not found
	if c.Request.URL.Path != "/" {
		res := s.db.
			Select("page_not_found_content").
			Where("name = ?", host).
			First(&domain)

		if res.RowsAffected == 0 {
			s.logger.Errorw("domain page unexpectedly not found",
				"host", host,
			)
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
		// TODO extract this into another method, maybe file
		t, err := textTmpl.
			New("staticContent").
			Funcs(service.TemplateFuncs()).
			Parse(string(domain.PageNotFoundContent))

		if err != nil {
			s.logger.Errorw("failed to parse static content template",
				"error", err,
			)
			c.Status(http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		err = t.Execute(&buf, map[string]any{
			"Domain":  host,
			"BaseURL": "https://" + host + "/",
			"URL":     c.Request.URL.String(),
		})
		if err != nil {
			s.logger.Errorw("failed to execute static content template",
				"error", err,
			)
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(
			http.StatusNotFound,
			"text/html; charset=utf-8",
			[]byte(buf.Bytes()),
		)
		c.Abort()
		return
	}
	// serve the static page
	res = s.db.
		Select("page_content").
		Where("name = ?", host).
		First(&domain)

	if res.RowsAffected == 0 {
		s.logger.Errorw("static page was unexpectedly not found",
			"host", host,
		)
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	t, err := textTmpl.
		New("staticContent").
		Funcs(service.TemplateFuncs()).
		Parse(domain.PageContent)

	if err != nil {
		s.logger.Errorw("failed to parse static content template",
			"error", errs.Wrap(err),
		)
		c.Status(http.StatusInternalServerError)
		return
	}
	buf := &bytes.Buffer{}
	err = t.Execute(buf, map[string]any{
		"Domain":  host,
		"BaseURL": "https://" + host + "/",
		"URL":     "https://" + host + c.Request.URL.String(),
	})
	if err != nil {
		s.logger.Errorw("failed to execute static content template",
			"error", errs.Wrap(err),
		)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Data(
		http.StatusOK,
		"text/html; charset=utf-8",
		buf.Bytes(),
	)
	c.Abort()
}

// handlerNotFound handles the request for a not found page
func (s *Server) handlerNotFound(c *gin.Context) {
	host, err := s.getHostOnly(c.Request.Host)
	if err != nil {
		s.logger.Debugw("failed to parse host",
			"host", c.Request.Host,
			"error", err,
		)
		c.Status(http.StatusNotFound)
		return
	}
	var domain *database.Domain
	res := s.db.
		Select("page_not_found_content").
		Where("name = ?", host).
		Find(&domain)

	if res.RowsAffected == 0 {
		s.logger.Debugw("host not found",
			"host", host,
		)
		c.Status(http.StatusNotFound)
		return
	}
	t := textTmpl.New("staticContent")
	t = t.Funcs(service.TemplateFuncs())
	tmpl, err := t.Parse(string(domain.PageNotFoundContent))
	if err != nil {
		s.logger.Errorw("failed to parse static content template",
			"error", errs.Wrap(err),
		)
		c.Status(http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"Domain":  host,
		"BaseURL": "https://" + host + "/",
		"URL":     c.Request.URL.String(),
	})
	if err != nil {
		s.logger.Errorw("failed to execute static content template",
			"error", err,
		)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(
		http.StatusNotFound,
		"text/html; charset=utf-8",
		[]byte(buf.Bytes()),
	)
}

// checkAndServePhishingPage serves a phishing page
// returns a bool if the request was for a phishing page
// and an error if there was an error
func (s *Server) checkAndServePhishingPage(
	c *gin.Context,
	domain *database.Domain,
) (bool, error) {
	// get campaign recipient from URL parameters
	campaignRecipient, _, err := server.GetCampaignRecipientFromURLParams(
		c,
		c.Request,
		s.repositories.Identifier,
		s.repositories.CampaignRecipient,
	)
	if err != nil {
		s.logger.Debugw("failed to get campaign recipient from URL parameters",
			"error", err,
		)
		return false, errs.Wrap(err)
	}
	if campaignRecipient == nil {
		s.logger.Debugw("'campaignrecipient' not found")
		return false, nil
	}

	campaignRecipientID := campaignRecipient.ID.MustGet()
	campaignRecipientIDPtr := &campaignRecipientID
	// at this point we know which url param matched the campaignrecipientID, however
	// it could have been any available identifier and not the one matching the campaign template
	// it is possible now to check if it is correct, however it does not matter as the campaign
	// recipient is already found
	campaignID := campaignRecipient.CampaignID.MustGet()
	campaign, err := s.repositories.Campaign.GetByID(
		c,
		&campaignID,
		&repository.CampaignOption{},
	)
	// if there was an error
	if err != nil {
		s.logger.Debugw("failed to get active campaign",
			"error", err,
		)
		return false, fmt.Errorf("failed to get active campaign and campaign recipient by public id: %s", err)
	}
	// check if the campaign is active
	if !campaign.IsActive() {
		s.logger.Debugw("campaign is not active",
			"campaignID", campaign.ID.MustGet(),
		)
		return false, nil
	}
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		s.logger.Debugw("campaign has no template",
			"error", err,
		)
		return false, nil
	}
	cTemplate, err := s.repositories.CampaignTemplate.GetByID(
		c,
		&templateID,
		&repository.CampaignTemplateOption{
			WithIdentifier: true,
		},
	)
	if err != nil {
		s.logger.Debugw("failed to get campaign template",
			"templateID", templateID.String(),
			"error", err,
		)
		return false, fmt.Errorf("failed to get campaign template: %s", err)
	}
	// check that the requesters IP is allow listed
	ip := utils.ExtractClientIP(c.Request)
	servedByIPFilter, err := s.checkIPFilter(c, ip, campaign, domain, &campaignID)
	if err != nil {
		return false, err
	}
	if servedByIPFilter {
		return true, nil
	}
	// get the recipient
	// if the recipient has been anonymized or removed, stop
	recipientID, err := campaignRecipient.RecipientID.Get()
	if err != nil {
		return false, nil
	}
	recipient, err := s.repositories.Recipient.GetByID(
		c,
		&recipientID,
		&repository.RecipientOption{},
	)
	if err != nil {
		return false, fmt.Errorf("failed to get recipient: %s", err)
	}
	// check for evasion page first
	var evasionPageID *uuid.UUID
	if v, err := campaign.EvasionPageID.Get(); err == nil {
		evasionPageID = &v
	}

	// figure out which page types this template has
	var beforePageID *uuid.UUID
	var beforeProxyID *uuid.UUID
	if v, err := cTemplate.BeforeLandingPageID.Get(); err == nil {
		beforePageID = &v
	} else if v, err := cTemplate.BeforeLandingProxyID.Get(); err == nil {
		beforeProxyID = &v
	}

	var landingPageID *uuid.UUID
	var landingProxyID *uuid.UUID
	if v, err := cTemplate.LandingPageID.Get(); err == nil {
		landingPageID = &v
	} else if v, err := cTemplate.LandingProxyID.Get(); err == nil {
		landingProxyID = &v
	} else {
		return false, fmt.Errorf("Template is incomplete, missing landing page or Proxy ID")
	}

	var afterPageID *uuid.UUID
	var afterProxyID *uuid.UUID
	if v, err := cTemplate.AfterLandingPageID.Get(); err == nil {
		afterPageID = &v
	} else if v, err := cTemplate.AfterLandingProxyID.Get(); err == nil {
		afterProxyID = &v
	}

	stateParamKey := cTemplate.StateIdentifier.Name.MustGet()
	pageTypeQuery := ""
	encryptedParam := c.Query(stateParamKey)
	secret := utils.UUIDToSecret(&campaignID)
	if v, err := utils.Decrypt(encryptedParam, secret); err == nil {
		pageTypeQuery = v
	}
	// if there is no page type then this is the before landing page or the landing page
	var pageID *uuid.UUID
	var proxyID *uuid.UUID
	nextPageType := ""
	currentPageType := ""

	/*
		s.logger.Debugw("determining page flow",
			"pageTypeQuery", pageTypeQuery,
			"hasBeforePage", beforePageID != nil,
			"hasBeforeProxy", beforeProxyID != nil,
			"hasLandingPage", landingPageID != nil,
			"hasLandingProxy", landingProxyID != nil,
			"hasAfterPage", afterPageID != nil,
			"hasAfterProxy", afterProxyID != nil,
			"campaignRecipientID", campaignRecipientID.String(),
		)
	*/

	if len(pageTypeQuery) == 0 {
		// check if there's an evasion page to serve first
		if evasionPageID != nil {
			pageID = evasionPageID
			s.logger.Debugw("initial request - serving evasion page",
				"pageID", pageID.String(),
			)
			currentPageType = data.PAGE_TYPE_EVASION
			// determine next page type based on template structure
			if beforePageID != nil || beforeProxyID != nil {
				nextPageType = data.PAGE_TYPE_BEFORE
			} else {
				nextPageType = data.PAGE_TYPE_LANDING
			}
		} else if beforePageID != nil || beforeProxyID != nil {
			if beforePageID != nil {
				pageID = beforePageID
				s.logger.Debugw("initial request - serving before landing page",
					"pageID", pageID.String(),
				)
			} else {
				proxyID = beforeProxyID
				s.logger.Debugw("initial request - serving before landing Proxy",
					"proxyID", proxyID.String(),
				)
			}
			currentPageType = data.PAGE_TYPE_BEFORE
			nextPageType = data.PAGE_TYPE_LANDING
		} else {
			if landingPageID != nil {
				pageID = landingPageID
				s.logger.Debugw("initial request - serving landing page",
					"pageID", pageID.String(),
				)
			} else {
				proxyID = landingProxyID
				s.logger.Debugw("initial request - serving landing Proxy",
					"proxyID", proxyID.String(),
				)
			}
			currentPageType = data.PAGE_TYPE_LANDING
			if afterPageID != nil || afterProxyID != nil {
				nextPageType = data.PAGE_TYPE_AFTER
			} else {
				nextPageType = data.PAGE_TYPE_DONE // landing page is final page
			}
		}
		// if there is a page type, then we use that
	} else {
		switch pageTypeQuery {
		// this is set if the previous page was an evasion page
		case data.PAGE_TYPE_EVASION:
			// after evasion page, go to before page or landing page
			if beforePageID != nil || beforeProxyID != nil {
				if beforePageID != nil {
					pageID = beforePageID
					s.logger.Debugw("serving before landing page from evasion state",
						"pageID", pageID.String(),
					)
				} else {
					proxyID = beforeProxyID
					s.logger.Debugw("serving before landing Proxy from evasion state",
						"proxyID", proxyID.String(),
					)
				}
				currentPageType = data.PAGE_TYPE_BEFORE
				nextPageType = data.PAGE_TYPE_LANDING
			} else {
				if landingPageID != nil {
					pageID = landingPageID
					s.logger.Debugw("serving landing page from evasion state",
						"pageID", pageID.String(),
					)
				} else {
					proxyID = landingProxyID
					s.logger.Debugw("serving landing Proxy from evasion state",
						"proxyID", proxyID.String(),
					)
				}
				currentPageType = data.PAGE_TYPE_LANDING
				if afterPageID != nil || afterProxyID != nil {
					nextPageType = data.PAGE_TYPE_AFTER
				} else {
					nextPageType = data.PAGE_TYPE_DONE
				}
			}
		// special case for deny page access from evasion page
		case "deny":
			// serve the deny page if one is configured
			if denyPageID, err := campaign.DenyPageID.Get(); err == nil {
				err = s.renderDenyPage(c, domain, &denyPageID)
				if err != nil {
					return true, fmt.Errorf("failed to render deny page from evasion: %s", err)
				}
				return true, nil
			} else {
				// if no deny page configured, return 403
				c.String(http.StatusForbidden, "Access denied")
				c.Abort()
				return true, nil
			}
		// this is set when transitioning to the before page
		case data.PAGE_TYPE_BEFORE:
			if beforePageID != nil {
				pageID = beforePageID
				s.logger.Debugw("serving before landing page from state",
					"pageID", pageID.String(),
				)
			} else {
				proxyID = beforeProxyID
				s.logger.Debugw("serving before landing Proxy from state",
					"proxyID", proxyID.String(),
				)
			}
			currentPageType = data.PAGE_TYPE_BEFORE
			nextPageType = data.PAGE_TYPE_LANDING
		// this is set if the previous page was a before page
		case data.PAGE_TYPE_LANDING:
			if landingPageID != nil {
				pageID = landingPageID
				s.logger.Debugw("serving landing page from state",
					"pageID", pageID.String(),
				)
			} else {
				proxyID = landingProxyID
				s.logger.Debugw("serving landing Proxy from state",
					"proxyID", proxyID.String(),
				)
			}
			currentPageType = data.PAGE_TYPE_LANDING
			if afterPageID != nil || afterProxyID != nil {
				nextPageType = data.PAGE_TYPE_AFTER
			} else {
				nextPageType = data.PAGE_TYPE_DONE // landing page is final page
			}
		// this is set if the previous page was a landing page
		case data.PAGE_TYPE_AFTER:
			if afterPageID != nil {
				pageID = afterPageID
				s.logger.Debugw("serving after landing page from state",
					"pageID", pageID.String(),
				)
			} else if afterProxyID != nil {
				proxyID = afterProxyID
				s.logger.Debugw("serving after landing Proxy from state",
					"proxyID", proxyID.String(),
				)
			} else if landingPageID != nil {
				pageID = landingPageID
				s.logger.Debugw("fallback to landing page for after state",
					"pageID", pageID.String(),
				)
			} else {
				proxyID = landingProxyID
				s.logger.Debugw("fallback to landing Proxy for after state",
					"proxyID", proxyID.String(),
				)
			}
			// next page after a after landing page, is the same page
			currentPageType = data.PAGE_TYPE_AFTER
			nextPageType = data.PAGE_TYPE_DONE
		case data.PAGE_TYPE_DONE:
			if afterPageID != nil {
				pageID = afterPageID
				s.logger.Debugw("serving after landing page for done state",
					"pageID", pageID.String(),
				)
			} else if afterProxyID != nil {
				proxyID = afterProxyID
				s.logger.Debugw("serving after landing Proxy for done state",
					"proxyID", proxyID.String(),
				)
			} else if landingPageID != nil {
				pageID = landingPageID
				s.logger.Debugw("fallback to landing page for done state",
					"pageID", pageID.String(),
				)
			} else {
				proxyID = landingProxyID
				s.logger.Debugw("fallback to landing Proxy for done state",
					"proxyID", proxyID.String(),
				)
			}
			currentPageType = data.PAGE_TYPE_DONE
			nextPageType = data.PAGE_TYPE_DONE
		}
	}
	isPOSTRequest := c.Request.Method == http.MethodPost
	// if this is a POST request, then save the submitted data
	if isPOSTRequest {
		submitDataEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
		err = c.Request.ParseForm()
		if err != nil {
			return true, fmt.Errorf("failed to parse submitted form data: %s", err)
		}
		newEventID := uuid.New()
		campaignID := campaign.ID.MustGet()
		clientIP := vo.NewOptionalString64Must(utils.ExtractClientIP(c.Request))
		userAgent := vo.NewOptionalString255Must(utils.Substring(c.Request.UserAgent(), 0, MAX_USER_AGENT_SAVED))
		submittedData := vo.NewEmptyOptionalString1MB()
		if campaign.SaveSubmittedData.MustGet() {
			submittedData, err = vo.NewOptionalString1MB(c.Request.PostForm.Encode())
			if err != nil {
				return true, fmt.Errorf("user submitted phishing data too large: %s", err)
			}
		}
		var event *model.CampaignEvent
		// only save data if red team flag is set
		if !campaign.IsAnonymous.MustGet() {
			event = &model.CampaignEvent{
				ID:          &newEventID,
				CampaignID:  &campaignID,
				RecipientID: &recipientID,
				IP:          clientIP,
				UserAgent:   userAgent,
				EventID:     submitDataEventID,
				Data:        submittedData,
			}
		} else {
			ua := vo.NewEmptyOptionalString255()
			data := vo.NewEmptyOptionalString1MB()
			event = &model.CampaignEvent{
				ID:          &newEventID,
				CampaignID:  &campaignID,
				RecipientID: nil,
				IP:          vo.NewEmptyOptionalString64(),
				UserAgent:   ua,
				EventID:     submitDataEventID,
				Data:        data,
			}
		}
		err = s.repositories.Campaign.SaveEvent(c, event)
		if err != nil {
			return true, fmt.Errorf("failed to save campaign event: %s", err)
		}
		// check and update if most notable event for recipient
		currentNotableEventID, _ := campaignRecipient.NotableEventID.Get()
		if cache.IsMoreNotableCampaignRecipientEventID(
			&currentNotableEventID,
			submitDataEventID,
		) {
			campaignRecipient.NotableEventID.Set(*submitDataEventID)
			err := s.repositories.CampaignRecipient.UpdateByID(
				c,
				campaignRecipientIDPtr,
				campaignRecipient,
			)
			if err != nil {
				s.logger.Errorw(
					"failed to update notable event",
					"campaignRecipientID", campaignRecipientID.String(),
					"error", err,
				)
				return true, errs.Wrap(err)
			}
		}
		// handle webhook
		webhookID, err := s.repositories.Campaign.GetWebhookIDByCampaignID(
			c,
			&campaignID,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			s.logger.Errorw("failed to get webhook id by campaign id",
				"campaignID", campaignID.String(),
				"error", err,
			)
			return true, errs.Wrap(err)
		}
		if webhookID != nil {
			err = s.services.Campaign.HandleWebhook(
				// TODO this should be tied to a application wide context not the request
				context.TODO(),
				webhookID,
				&campaignID,
				&recipientID,
				data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
			)
			if err != nil {
				return true, fmt.Errorf("failed to handle webhook: %s", err)
			}
		}
	}
	// if redirect && POST && final page
	if isPOSTRequest {
		if redirectURL, err := cTemplate.AfterLandingPageRedirectURL.Get(); err == nil {
			if v := redirectURL.String(); len(v) > 0 {
				// if the current page is landing and there is no after, redirect
				if currentPageType == data.PAGE_TYPE_DONE {
					c.Redirect(http.StatusSeeOther, v)
					c.Abort()
					return true, nil
				}
			}
		}
	}

	// handle Proxy pages
	if proxyID != nil {
		// this is a Proxy page - redirect to the phishing domain
		proxy, err := s.repositories.Proxy.GetByID(
			c,
			proxyID,
			&repository.ProxyOption{},
		)
		if err != nil {
			return true, fmt.Errorf("failed to get Proxy page: %s", err)
		}

		startURL, err := proxy.StartURL.Get()
		if err != nil {
			return true, fmt.Errorf("Proxy page has no start URL: %s", err)
		}

		// parse proxy config to find the phishing domain
		proxyConfig, err := proxy.ProxyConfig.Get()
		if err != nil {
			return true, fmt.Errorf("Proxy page has no configuration: %s", err)
		}

		// extract the phishing domain from Proxy configuration
		var rawConfig map[string]interface{}
		err = yaml.Unmarshal([]byte(proxyConfig.String()), &rawConfig)
		if err != nil {
			return true, fmt.Errorf("invalid Proxy configuration YAML: %s", err)
		}

		// parse the start URL to get the target domain
		parsedStartURL, err := url.Parse(startURL.String())
		if err != nil {
			return true, fmt.Errorf("invalid proxy start URL: %s", err)
		}
		startDomain := parsedStartURL.Host

		// find the phishing domain mapping for the start URL domain
		phishingDomain := ""
		for originalHost, domainData := range rawConfig {
			if originalHost == "proxy" || originalHost == "global" {
				continue
			}
			if originalHost == startDomain {
				if domainMap, ok := domainData.(map[string]interface{}); ok {
					if to, exists := domainMap["to"]; exists {
						if toStr, ok := to.(string); ok {
							phishingDomain = toStr
							break
						}
					}
				}
			}
		}

		if phishingDomain == "" {
			return true, fmt.Errorf("no phishing domain mapping found for start URL domain: %s", startDomain)
		}

		// save the event of Proxy page being accessed
		visitEventID := uuid.New()
		eventName := ""
		switch currentPageType {
		case data.PAGE_TYPE_EVASION:
			eventName = data.EVENT_CAMPAIGN_RECIPIENT_EVASION_PAGE_VISITED
		case data.PAGE_TYPE_BEFORE:
			eventName = data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED
		case data.PAGE_TYPE_LANDING:
			eventName = data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED
		case data.PAGE_TYPE_AFTER:
			eventName = data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED
		default:
			eventName = data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED
		}
		eventID := cache.EventIDByName[eventName]
		clientIP := vo.NewOptionalString64Must(utils.ExtractClientIP(c.Request))
		userAgent := vo.NewOptionalString255Must(utils.Substring(c.Request.UserAgent(), 0, MAX_USER_AGENT_SAVED))
		var visitEvent *model.CampaignEvent
		if !campaign.IsAnonymous.MustGet() {
			visitEvent = &model.CampaignEvent{
				ID:          &visitEventID,
				CampaignID:  &campaignID,
				RecipientID: &recipientID,
				IP:          clientIP,
				UserAgent:   userAgent,
				EventID:     eventID,
				Data:        vo.NewEmptyOptionalString1MB(),
			}
		} else {
			ua := vo.NewEmptyOptionalString255()
			visitEvent = &model.CampaignEvent{
				ID:          &visitEventID,
				CampaignID:  &campaignID,
				RecipientID: nil,
				IP:          vo.NewEmptyOptionalString64(),
				UserAgent:   ua,
				EventID:     eventID,
				Data:        vo.NewEmptyOptionalString1MB(),
			}
		}

		// save the visit event unless it's the final page repeat
		if currentPageType != data.PAGE_TYPE_DONE {
			err = s.repositories.Campaign.SaveEvent(
				c,
				visitEvent,
			)
			if err != nil {
				s.logger.Errorw("failed to save proxy visit event",
					"error", err,
					"proxyID", proxyID.String(),
				)
			}

			// check and update if most notable event for recipient
			currentNotableEventID, _ := campaignRecipient.NotableEventID.Get()
			if cache.IsMoreNotableCampaignRecipientEventID(
				&currentNotableEventID,
				eventID,
			) {
				campaignRecipient.NotableEventID.Set(*eventID)
				err := s.repositories.CampaignRecipient.UpdateByID(
					c,
					campaignRecipientIDPtr,
					campaignRecipient,
				)
				if err != nil {
					s.logger.Errorw("failed to update notable event for proxy",
						"campaignRecipientID", campaignRecipientID.String(),
						"eventID", eventID.String(),
						"error", err,
					)
				}
			}
		}

		// handle webhook for Proxy page visit
		webhookID, err := s.repositories.Campaign.GetWebhookIDByCampaignID(
			c,
			&campaignID,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			s.logger.Errorw("failed to get webhook id by campaign id for proxy",
				"campaignID", campaignID.String(),
				"error", err,
			)
		}
		if webhookID != nil && currentPageType != data.PAGE_TYPE_DONE {
			err = s.services.Campaign.HandleWebhook(
				// TODO this should be tied to a application wide context not the request
				context.TODO(),
				webhookID,
				&campaignID,
				&recipientID,
				eventName,
			)
			if err != nil {
				s.logger.Errorw("failed to handle webhook for Proxy page",
					"error", err,
					"proxyID", proxyID.String(),
				)
			}
		}

		// validate phishing domain format
		if strings.Contains(phishingDomain, "://") || strings.Contains(phishingDomain, "/") {
			return true, fmt.Errorf("invalid phishing domain format: %s", phishingDomain)
		}

		// validate that the phishing domain is configured as a proxy domain
		var phishingDomainRecord *database.Domain
		res := s.db.
			Select("id, name, type, proxy_id, proxy_target_domain").
			Where("name = ?", phishingDomain).
			First(&phishingDomainRecord)

		if res.RowsAffected == 0 {
			return true, fmt.Errorf("phishing domain '%s' is not configured in the system", phishingDomain)
		}

		if phishingDomainRecord.Type != "proxy" {
			return true, fmt.Errorf("phishing domain '%s' is not configured as proxy type", phishingDomain)
		}

		s.logger.Debugw("redirecting to Proxy phishing domain",
			"proxyID", proxyID.String(),
			"startURL", startURL.String(),
			"phishingDomain", phishingDomain,
			"currentPageType", currentPageType,
			"phishingDomainType", phishingDomainRecord.Type,
		)

		// build the redirect URL to the phishing domain with campaign recipient ID
		urlParam := cTemplate.URLIdentifier.Name.MustGet()

		// construct the redirect URL properly
		u := &url.URL{
			Scheme: "https",
			Host:   phishingDomain,
			Path:   parsedStartURL.Path,
		}

		q := u.Query()
		q.Set(urlParam, campaignRecipientID.String())
		if encryptedParam != "" {
			q.Set(stateParamKey, encryptedParam)
		}
		// preserve any existing query params from start URL
		if parsedStartURL.RawQuery != "" {
			startQuery, _ := url.ParseQuery(parsedStartURL.RawQuery)
			for key, values := range startQuery {
				for _, value := range values {
					q.Add(key, value)
				}
			}
		}
		u.RawQuery = q.Encode()

		s.logger.Debugw("built proxy redirect URL",
			"redirectURL", u.String(),
			"phishingDomain", phishingDomain,
			"originalPath", parsedStartURL.Path,
		)

		// validate the final URL
		finalURL := u.String()
		if !strings.HasPrefix(finalURL, "https://") {
			return true, fmt.Errorf("invalid redirect URL scheme: %s", finalURL)
		}

		s.logger.Infow("redirecting to proxy domain",
			"from", c.Request.Host+c.Request.URL.Path,
			"to", finalURL,
			"campaignRecipientID", campaignRecipientID.String(),
		)

		c.Redirect(http.StatusSeeOther, finalURL)
		c.Abort()
		return true, nil
	}

	// ensure we have a page ID if we're not handling a proxy
	if pageID == nil {
		return true, fmt.Errorf("no page or proxy configured for current step")
	}

	// fetch the regular page
	page, err := s.repositories.Page.GetByID(
		c,
		pageID,
		&repository.PageOption{},
	)
	if err != nil {
		return true, fmt.Errorf("failed to get landing page: %s", err)
	}

	// fetch the sender email to use for the template
	emailID := cTemplate.EmailID.MustGet()
	email, err := s.repositories.Email.GetByID(
		c,
		&emailID,
		&repository.EmailOption{},
	)
	if err != nil {
		return true, fmt.Errorf("failed to get email: %s", err)
	}
	encryptedParam, err = utils.Encrypt(nextPageType, secret)
	if err != nil {
		return true, fmt.Errorf("failed to encrypt next page type: %s", err)
	}
	urlPath := cTemplate.URLPath.MustGet().String()

	err = s.renderPageTemplate(
		c,
		domain,
		email,
		campaignRecipientIDPtr,
		recipient,
		page,
		cTemplate,
		encryptedParam,
		urlPath,
		campaign,
	)
	if err != nil {
		return true, fmt.Errorf("failed to render phishing page: %s", err)
	}
	// save the event of page has been visited
	visitEventID := uuid.New()
	eventName := ""
	switch currentPageType {
	case data.PAGE_TYPE_EVASION:
		eventName = data.EVENT_CAMPAIGN_RECIPIENT_EVASION_PAGE_VISITED
	case data.PAGE_TYPE_BEFORE:
		eventName = data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED
	case data.PAGE_TYPE_LANDING:
		eventName = data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED
	case data.PAGE_TYPE_AFTER:
		eventName = data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED
	}
	eventID := cache.EventIDByName[eventName]
	clientIP := vo.NewOptionalString64Must(utils.ExtractClientIP(c.Request))
	userAgent := vo.NewOptionalString255Must(utils.Substring(c.Request.UserAgent(), 0, MAX_USER_AGENT_SAVED))
	var visitEvent *model.CampaignEvent
	if !campaign.IsAnonymous.MustGet() {
		visitEvent = &model.CampaignEvent{
			ID:          &visitEventID,
			CampaignID:  &campaignID,
			RecipientID: &recipientID,
			IP:          clientIP,
			UserAgent:   userAgent,
			EventID:     eventID,
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	} else {
		ua := vo.NewEmptyOptionalString255()
		visitEvent = &model.CampaignEvent{
			ID:          &visitEventID,
			CampaignID:  &campaignID,
			RecipientID: nil,
			IP:          vo.NewEmptyOptionalString64(),
			UserAgent:   ua,
			EventID:     eventID,
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	}
	// only log the page visit if it is not after the final page
	if currentPageType != data.PAGE_TYPE_DONE {
		err = s.repositories.Campaign.SaveEvent(
			c,
			visitEvent,
		)
		if err != nil {
			return true, fmt.Errorf("failed to save campaign event: %s", err)
		}
	}
	// check and update if most notable event for recipient
	currentNotableEventID, _ := campaignRecipient.NotableEventID.Get()
	if cache.IsMoreNotableCampaignRecipientEventID(
		&currentNotableEventID,
		eventID,
	) {
		campaignRecipient.NotableEventID.Set(*eventID)
		err := s.repositories.CampaignRecipient.UpdateByID(
			c,
			campaignRecipientIDPtr,
			campaignRecipient,
		)
		if err != nil {
			s.logger.Errorw("failed to update notable event",
				"campaignRecipientID", campaignRecipientID.String(),
				"eventID", eventID.String(),
				"error", err,
			)
			return true, errs.Wrap(err)
		}
	}
	// handle webhook
	webhookID, err := s.repositories.Campaign.GetWebhookIDByCampaignID(
		c,
		&campaignID,
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.logger.Errorw("failed to get webhook id by campaign id %s",
			"campaignID", campaignID.String(),
			"error", err,
		)
		return true, errs.Wrap(err)
	}
	if webhookID == nil {
		return true, nil
	}
	// do not notify on visiting the page done as it is a repeat of the flow
	if currentPageType != data.PAGE_TYPE_DONE {
		err = s.services.Campaign.HandleWebhook(
			// TODO this should be tied to a application wide context not the request
			context.TODO(),
			webhookID,
			&campaignID,
			&recipientID,
			eventName,
		)
		if err != nil {
			return true, fmt.Errorf("failed to handle webhook: %s", err)
		}
	}

	return true, nil
}

func (s *Server) renderDenyPage(
	c *gin.Context,
	domain *database.Domain,
	pageID *uuid.UUID,
) error {
	ctx := c.Request.Context()
	page, err := s.repositories.Page.GetByID(
		ctx,
		pageID,
		&repository.PageOption{},
	)
	if err != nil {
		return fmt.Errorf("failed to get deny page: %s", err)
	}

	// get campaign recipient - there MUST be one if we're rendering a deny page
	campaignRecipient, _, err := server.GetCampaignRecipientFromURLParams(
		ctx,
		c.Request,
		s.repositories.Identifier,
		s.repositories.CampaignRecipient,
	)
	if err != nil {
		return fmt.Errorf("failed to get campaign recipient for deny page: %s", err)
	}
	if campaignRecipient == nil {
		return fmt.Errorf("campaign recipient is nil")
	}

	// get recipient ID from campaign recipient with nil check
	recipientID, err := campaignRecipient.RecipientID.Get()
	if err != nil {
		return fmt.Errorf("campaign recipient has no recipient ID: %s", err)
	}
	recipient, err := s.repositories.Recipient.GetByID(ctx, &recipientID, &repository.RecipientOption{})
	if err != nil {
		return fmt.Errorf("failed to get recipient: %s", err)
	}
	if recipient == nil {
		return fmt.Errorf("recipient is nil")
	}

	// get campaign with nil check
	campaignID, err := campaignRecipient.CampaignID.Get()
	if err != nil {
		return fmt.Errorf("campaign recipient has no campaign ID: %s", err)
	}
	campaign, err := s.repositories.Campaign.GetByID(ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		return fmt.Errorf("failed to get campaign: %s", err)
	}
	if campaign == nil {
		return fmt.Errorf("campaign is nil")
	}

	// get campaign template with email and nil check
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		return fmt.Errorf("campaign has no template ID: %s", err)
	}
	cTemplate, err := s.repositories.CampaignTemplate.GetByID(
		ctx,
		&templateID,
		&repository.CampaignTemplateOption{
			WithEmail:      true,
			WithIdentifier: true,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to get campaign template: %s", err)
	}
	if cTemplate == nil {
		return fmt.Errorf("campaign template is nil")
	}

	// get email with nil check
	emailID, err := cTemplate.EmailID.Get()
	if err != nil {
		return fmt.Errorf("campaign template has no email ID: %s", err)
	}
	email, err := s.repositories.Email.GetByID(ctx, &emailID, &repository.EmailOption{})
	if err != nil {
		return fmt.Errorf("failed to get email: %s", err)
	}
	if email == nil {
		return fmt.Errorf("email is nil")
	}

	// get campaign recipient ID with nil check
	campaignRecipientID, err := campaignRecipient.ID.Get()
	if err != nil {
		return fmt.Errorf("campaign recipient has no ID: %s", err)
	}

	// render with full template context
	buf, err := s.services.Template.CreatePhishingPageWithCampaign(
		domain,
		email,
		&campaignRecipientID,
		recipient,
		page.Content.MustGet().String(),
		cTemplate,
		"", // no state parameter for deny pages
		c.Request.URL.Path,
		campaign,
	)
	if err != nil {
		return fmt.Errorf("failed to render deny page template: %s", err)
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
	c.Abort()
	// safely log with nil checks
	pageName := "unknown"
	if pageNameVal, err := page.Name.Get(); err == nil {
		pageName = pageNameVal.String()
	}
	pageIDStr := "unknown"
	if pageIDVal, err := page.ID.Get(); err == nil {
		pageIDStr = pageIDVal.String()
	}
	recipientEmailStr := "unknown"
	if recipientEmailVal, err := recipient.Email.Get(); err == nil {
		recipientEmailStr = recipientEmailVal.String()
	}

	s.logger.Debugw("rendered deny page",
		"pageName", pageName,
		"pageID", pageIDStr,
		"recipientEmail", recipientEmailStr,
	)
	return nil
}

// AssignRoutes assigns the routes to the server
func (s *Server) AssignRoutes(r *gin.Engine) {
	r.Use(s.Handler)
	r.NoRoute(s.handlerNotFound)
}

// getProxyCookieValue extracts proxy cookie value from gin context
func (s *Server) getProxyCookieValue(c *gin.Context) string {
	cookieName := s.proxyServer.GetCookieName()
	if cookieValue, err := c.Cookie(cookieName); err == nil {
		return cookieValue
	}
	return ""
}

func (s *Server) StartHTTP(
	r *gin.Engine,
	conf *config.Config,
) (chan server.StartupMessage, net.Listener, error) {
	addr := conf.PhishingHTTPNetAddress()
	ln, err := net.Listen(
		"tcp",
		addr,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %s due to: %s", addr, err)
	}
	s.HTTPServer = s.defaultServer(r, false)

	go func() {
		s.logger.Debugw("starting phishing HTTP server",
			"address", addr,
		)
		// handle on-demand http TLS challenges
		myACME := certmagic.NewACMEIssuer(s.certMagicConfig, certmagic.DefaultACME)
		myACME.HTTPChallengeHandler(r)
		err := s.HTTPServer.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			s.logger.Fatalw("failed to start phishing HTTP server",
				"error", err,
			)
		}
	}()
	// start a routine to test the connection
	startupMessage := s.testTCPConnection("HTTP phishing server", addr)
	return startupMessage, ln, nil
}

// StartHTTPS starts the server and returns a signal channel
func (s *Server) StartHTTPS(
	r *gin.Engine,
	conf *config.Config,
) (chan server.StartupMessage, net.Listener, error) {
	addr := conf.PhishingHTTPSNetAddress()
	// create supplied cert path if it does not exist
	err := os.MkdirAll(s.ownManagedTLSCertPath, 0750)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create supplied certs path %s: %s", s.ownManagedTLSCertPath, err)
	}
	// cache all own supplied certs
	folders, err := os.ReadDir(s.ownManagedTLSCertPath)
	if err != nil {
		s.logger.Warnw("failed to read supplied certs folder",
			"path", s.ownManagedTLSCertPath,
			"error", err,
		)
	}
	for _, folder := range folders {
		if !folder.IsDir() {
			continue
		}
		// get the folder path
		folderPath := filepath.Join(s.ownManagedTLSCertPath, folder.Name())
		// find .pem and .key files
		certFile := filepath.Join(folderPath, "cert.pem")
		keyFile := filepath.Join(folderPath, "cert.key")
		// check if both files exist
		_, err := os.Stat(certFile)
		if err != nil {
			s.logger.Warnw("certificate file missing",
				"folder", folder.Name(),
				"error", err,
			)
			continue
		}
		_, err = os.Stat(keyFile)
		if err != nil {
			s.logger.Warnw("certificate key file missing",
				"folder", folder.Name(),
				"error", err,
			)
			continue
		}
		hash, err := s.certMagicConfig.CacheUnmanagedCertificatePEMFile(
			context.Background(),
			certFile,
			keyFile,
			[]string{},
		)
		if err != nil {
			s.logger.Warnw("failed to cache certificate",
				"folder", folder.Name(),
				"error", err,
			)
			continue
		}
		s.logger.Debugw("cached certificate",
			"folder", folder.Name(),
			"hash", hash,
		)
	}
	// setup TLS config
	tlsConf := s.certMagicConfig.TLSConfig()
	tlsConf.NextProtos = append([]string{"h2"}, tlsConf.NextProtos...)
	// setup gin
	ln, err := tls.Listen(
		"tcp",
		addr,
		tlsConf,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %s due to: %s", ln.Addr().String(), err)
	}
	s.HTTPSServer = s.defaultServer(r, true)
	// start server
	go func() {
		s.logger.Debugw("starting phishing HTTPS server",
			"address", addr,
		)
		err := s.HTTPSServer.Serve(ln)

		if err != nil && err != http.ErrServerClosed {
			s.logger.Fatalw("failed to start phishing HTTPS server",
				"error", err,
			)
		}
	}()
	// start a routine to test the connection
	startupMessage := s.testTCPConnection("HTTPS phishing server", addr)
	return startupMessage, ln, nil
}

// renderPageTempate renders a page template
func (s *Server) renderPageTemplate(
	c *gin.Context,
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipient *model.Recipient,
	page *model.Page,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
	campaign *model.Campaign,
) error {
	content, err := page.Content.Get()
	if err != nil {
		return fmt.Errorf("no page content set to render: %s", err)
	}
	phishingPage, err := s.services.Template.CreatePhishingPageWithCampaign(
		domain,
		email,
		campaignRecipientID,
		recipient,
		content.String(),
		campaignTemplate,
		stateParam,
		urlPath,
		campaign,
	)
	if err != nil {
		return fmt.Errorf("failed to create phishing page: %s", err)
	}

	// apply obfuscation if enabled
	pageContent := phishingPage.Bytes()
	if campaign != nil {
		if obfuscate, err := campaign.Obfuscate.Get(); err == nil && obfuscate {
			s.logger.Debugw("obfuscating page", "campaignID", campaign.ID.MustGet().String(), "pageID", page.ID.MustGet().String())
			obfuscated, err := utils.ObfuscateHTML(string(pageContent), utils.DefaultObfuscationConfig())
			if err != nil {
				s.logger.Errorw("failed to obfuscate page", "error", err)
			} else {
				s.logger.Debugw("page obfuscated successfully", "originalSize", len(pageContent), "obfuscatedSize", len(obfuscated))
				pageContent = []byte(obfuscated)
			}
		} else {
			s.logger.Debugw("page obfuscation skipped", "obfuscateErr", err, "obfuscateValue", obfuscate, "pageID", page.ID.MustGet().String())
		}
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", pageContent)
	c.Abort()
	s.logger.Debugw("served phishing page",
		"pageID", page.ID.MustGet().String(),
		"pageName", page.Name.MustGet().String(),
	)
	return nil
}

func (s *Server) checkIPFilter(
	ctx *gin.Context,
	ip string,
	campaign *model.Campaign,
	domain *database.Domain,
	campaignID *uuid.UUID,
) (bool, error) {
	allowDenyLEntries, err := s.repositories.Campaign.GetAllDenyByCampaignID(ctx, campaignID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.logger.Debugw("failed to get deny list for campaign",
			"campaignID", campaignID.String(),
			"error", err,
		)
		return false, fmt.Errorf("failed to get deny list for campaign: %s", err)
	}
	// if there is a deny list, check if the IP allowed / denied
	// when allow listing we must check all entries to see if we have a allowed IP
	// when deny listing only a single entry needs to deny the IP
	isAllowListing := false
	allowed := len(allowDenyLEntries) == 0
	for i, allowDeny := range allowDenyLEntries {
		if i == 0 {
			isAllowListing = allowDeny.Allowed.MustGet()
			if !isAllowListing {
				// if deny listing, then by default the IP is allowed until proven otherwise
				allowed = true
			}
		}
		ok, err := allowDeny.IsIPAllowed(ip)
		if err != nil {
			return false, errs.Wrap(err)
		}
		if isAllowListing && ok {
			s.logger.Debugw("IP is allow listed",
				"ip", ip,
				"list name", allowDeny.Name.MustGet().String(),
				"list id", allowDeny.ID.MustGet().String(),
			)
			allowed = true
			break
			// if it is a deny list and a IP is not ok, we can break
		} else if !isAllowListing && !ok {
			s.logger.Debugw("IP is deny listed",
				"ip", ip,
				"list name", allowDeny.Name.MustGet().String(),
				"list id", allowDeny.ID.MustGet().String(),
			)
			allowed = false
			break
		}
	}
	if !allowed {
		s.logger.Debugw("IP is not allowed",
			"ip", ip,
		)
		if denyPageID, err := campaign.DenyPageID.Get(); err == nil {
			err = s.renderDenyPage(ctx, domain, &denyPageID)
			if err != nil {
				return true, fmt.Errorf("failed to render deny page: %s", err)
			}
			return true, nil
		}
		ctx.AbortWithStatus(http.StatusNotFound)
		return true, nil
	}
	return false, nil
}

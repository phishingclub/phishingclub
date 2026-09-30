package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/enetx/g"
	"github.com/enetx/surf"
	"github.com/phishingclub/phishingclub/service"
)

// browserProfile represents detected browser and platform information
type browserProfile struct {
	isChrome  bool
	isFirefox bool
	isSafari  bool
	isEdge    bool
	// platform
	isWindows bool
	isMacOS   bool
	isLinux   bool
	isAndroid bool
	isIOS     bool
}

// detectBrowserFromUserAgent analyzes user-agent to determine browser type and platform
func (m *ProxyHandler) detectBrowserFromUserAgent(userAgent string) *browserProfile {
	profile := &browserProfile{}

	// normalize user-agent for comparison
	ua := strings.ToLower(userAgent)

	// detect browser from user-agent
	// note: order matters - edge and chrome both contain "chrome" in ua
	if strings.Contains(ua, "edg/") || strings.Contains(ua, "edge/") {
		profile.isEdge = true
	} else if strings.Contains(ua, "chrome/") || strings.Contains(ua, "crios/") {
		profile.isChrome = true
	} else if strings.Contains(ua, "firefox/") || strings.Contains(ua, "fxios/") {
		profile.isFirefox = true
	} else if strings.Contains(ua, "safari/") && !strings.Contains(ua, "chrome") && strings.Contains(ua, "version/") {
		profile.isSafari = true
	}

	// detect operating system from user-agent
	// note: order matters - android contains "linux", so check mobile platforms first
	switch {
	case strings.Contains(ua, "android"):
		profile.isAndroid = true
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad"):
		profile.isIOS = true
	case strings.Contains(ua, "windows nt"):
		profile.isWindows = true
	case strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os x"):
		profile.isMacOS = true
	case strings.Contains(ua, "x11") || strings.Contains(ua, "linux"):
		profile.isLinux = true
	}

	return profile
}

// createSurfClient creates a surf http client with optional browser impersonation
func (m *ProxyHandler) createSurfClient(userAgent string, proxyConfig *service.ProxyServiceConfigYAML, acceptLanguage string, retainUA bool, enableImpersonation bool, proxyOverride string) (*http.Client, error) {
	// build surf client
	builder := surf.NewClient().Builder()

	// apply impersonation if enabled
	if enableImpersonation {
		// detect browser profile from user-agent
		profile := m.detectBrowserFromUserAgent(userAgent)

		// apply platform (OS) impersonation first
		impersonate := builder.Impersonate()
		switch {
		case profile.isWindows:
			impersonate = impersonate.Windows()
			m.logger.Debugw("applying windows platform impersonation", "userAgent", userAgent)
		case profile.isMacOS:
			impersonate = impersonate.MacOS()
			m.logger.Debugw("applying macos platform impersonation", "userAgent", userAgent)
		case profile.isLinux:
			impersonate = impersonate.Linux()
			m.logger.Debugw("applying linux platform impersonation", "userAgent", userAgent)
		case profile.isAndroid:
			impersonate = impersonate.Android()
			m.logger.Debugw("applying android platform impersonation", "userAgent", userAgent)
		case profile.isIOS:
			impersonate = impersonate.IOS()
			m.logger.Debugw("applying ios platform impersonation", "userAgent", userAgent)
		default:
			// default to windows as most common platform
			impersonate = impersonate.Windows()
			m.logger.Debugw("applying default windows platform impersonation", "userAgent", userAgent)
		}

		// KNOWN BUG (surf impersonation, upstream utls): the impersonation
		// profiles are unstable in the current surf/utls versions.
		//   - Firefox: the handshake fails with "tls: invalid server key share"
		//     against many targets, so the Firefox profile is effectively broken.
		//   - Chrome: works most of the time but the per connection ClientHello
		//     extension shuffle occasionally produces a hello the target resets,
		//     so a request can intermittently fail to connect.
		// Impersonation is off by default. The non impersonated client is
		// reliable and still uses HTTP/2. Revisit when surf/utls is updated.
		// apply browser impersonation based on detected profile
		switch {
		case profile.isChrome || profile.isEdge:
			// chrome impersonation (edge uses chromium engine)
			builder = impersonate.Chrome()
			m.logger.Debugw("applying chrome browser impersonation")
		case profile.isFirefox:
			// firefox impersonation. see KNOWN BUG above: the firefox profile
			// currently fails the tls handshake with invalid server key share
			builder = impersonate.Firefox()
			m.logger.Debugw("applying firefox browser impersonation")
		case profile.isSafari:
			// safari uses webkit - default to chrome for now as surf doesn't have safari profile
			builder = impersonate.Chrome()
			m.logger.Debugw("applying chrome browser impersonation for safari")
		default:
			// default to chrome as most common browser
			builder = impersonate.Chrome()
			m.logger.Debugw("applying default chrome browser impersonation")
		}

		// when retainUA is true, explicitly set the client's user-agent to override
		// the impersonation profile's default user-agent
		if retainUA {
			builder = builder.UserAgent(userAgent)
			m.logger.Debugw("retaining client user-agent with impersonation", "userAgent", userAgent)
		}
	}

	// configure timeout
	builder = builder.Timeout(30 * time.Second)

	// disable surf's response decompression. surf decodes eagerly: it builds the
	// gzip reader as soon as the headers arrive, so a response that advertises
	// Content-Encoding with an empty body (a 302 redirect from the login flow, a
	// 304, a 204) makes the gzip reader read from an empty stream and return EOF,
	// which surf surfaces as the whole request failing. readAndDecompressBody
	// decompresses from the fully buffered body instead and handles empty bodies.
	builder = builder.DisableCompression()

	// preserve client's accept-language header if provided
	if acceptLanguage != "" {
		builder = builder.AddHeaders("Accept-Language", acceptLanguage)
	}

	// configure proxy if specified. a session override chosen by the session
	// script takes precedence over the proxy from the yaml config.
	proxyStr := proxyConfig.Proxy
	if proxyOverride != "" {
		proxyStr = proxyOverride
	}
	if proxyStr != "" {
		proxyURL, err := m.parseProxyURL(proxyStr)
		if err != nil {
			return nil, err
		}
		builder = builder.Proxy(g.String(proxyURL.String()))
		m.logger.Debugw("configured surf client with proxy",
			"proxy", proxyURL.String(),
		)
	}

	// build the client
	result := builder.Build()
	if result.IsErr() {
		return nil, result.Err()
	}

	// convert surf client to standard http.Client for compatibility
	return result.Ok().Std(), nil
}

// createHTTPClientWithImpersonation creates surf http client with optional impersonation
func (m *ProxyHandler) createHTTPClientWithImpersonation(req *http.Request, reqCtx *RequestContext, proxyConfig *service.ProxyServiceConfigYAML) (*http.Client, error) {
	// check if impersonation is enabled in config
	impersonateEnabled := false
	retainUA := false
	if proxyConfig.Global != nil && proxyConfig.Global.Impersonate != nil {
		impersonateEnabled = proxyConfig.Global.Impersonate.Enabled
		retainUA = proxyConfig.Global.Impersonate.RetainUA
	}

	// extract user-agent and accept-language from current request headers
	userAgent := req.Header.Get("User-Agent")
	acceptLanguage := req.Header.Get("Accept-Language")

	if impersonateEnabled {
		m.logger.Debugw("impersonation enabled, using surf client with impersonation",
			"userAgent", userAgent,
			"retainUA", retainUA,
		)
	} else {
		m.logger.Debugw("using surf client without impersonation",
			"userAgent", userAgent,
		)
	}

	// use the upstream proxy the session script selected for this session, if any
	proxyOverride := ""
	if reqCtx.Session != nil {
		proxyOverride = reqCtx.Session.UpstreamProxy
	}

	// always use surf, but conditionally apply impersonation
	client, err := m.createSurfClient(userAgent, proxyConfig, acceptLanguage, retainUA, impersonateEnabled, proxyOverride)
	if err != nil {
		m.logger.Errorw("failed to create surf client",
			"error", err,
		)
		return nil, err
	}

	reqCtx.UsedImpersonation = impersonateEnabled
	return client, nil
}

// parseProxyURL parses and normalizes the proxy URL string
// if the proxy string is just an IP:port, it prepends "http://"
// otherwise it uses the full string to support socks4/socks5 and authentication
func (m *ProxyHandler) parseProxyURL(proxyStr string) (*url.URL, error) {
	// check if the string already contains a scheme (http://, https://, socks4://, socks5://)
	hasScheme := strings.Contains(proxyStr, "://")

	// check if it contains authentication credentials
	hasAuth := strings.Contains(proxyStr, "@")

	// if it has a scheme or auth, use it as-is
	if hasScheme || hasAuth {
		// if it has auth but no scheme, default to http://
		if hasAuth && !hasScheme {
			proxyStr = "http://" + proxyStr
		}
		return url.Parse(proxyStr)
	}

	// otherwise, it's just an IP:port, so prepend http://
	return url.Parse("http://" + proxyStr)
}

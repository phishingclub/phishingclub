package proxy

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/enetx/surf"
	"github.com/phishingclub/phishingclub/service"
	"golang.org/x/net/proxy"
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

// createSurfClient creates a surf http client with browser impersonation
func (m *ProxyHandler) createSurfClient(userAgent string, proxyConfig *service.ProxyServiceConfigYAML, acceptLanguage string, retainUA bool) (*http.Client, error) {
	// detect browser profile from user-agent
	profile := m.detectBrowserFromUserAgent(userAgent)

	// build surf client with impersonation
	builder := surf.NewClient().Builder()

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

	// apply browser impersonation based on detected profile
	switch {
	case profile.isChrome || profile.isEdge:
		// chrome impersonation (edge uses chromium engine)
		builder = impersonate.Chrome()
		m.logger.Debugw("applying chrome browser impersonation")
	case profile.isFirefox:
		// firefox impersonation
		builder = impersonate.FireFox()
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

	// configure timeout
	builder = builder.Timeout(30 * time.Second)

	// note: surf automatically decompresses response bodies via decodeBodyMW middleware
	// even when using .Std(), but keeps the Content-Encoding header
	// our proxy code will detect this and remove the header before sending to client

	// retain original user-agent if configured
	if retainUA && userAgent != "" {
		builder = builder.UserAgent(userAgent)
	}

	// preserve client's accept-language header if provided
	if acceptLanguage != "" {
		builder = builder.AddHeaders("Accept-Language", acceptLanguage)
	}

	// configure proxy if specified
	if proxyConfig.Proxy != "" {
		proxyURL, err := m.parseProxyURL(proxyConfig.Proxy)
		if err != nil {
			return nil, err
		}
		builder = builder.Proxy(proxyURL.String())
		m.logger.Debugw("configured surf client with proxy",
			"proxy", proxyURL.String(),
		)
	}

	// build the client
	client := builder.Build()

	// convert surf client to standard http.Client for compatibility
	return client.Std(), nil
}

// createHTTPClientWithImpersonation creates http client with optional surf impersonation
func (m *ProxyHandler) createHTTPClientWithImpersonation(req *http.Request, reqCtx *RequestContext, proxyConfig *service.ProxyServiceConfigYAML) (*http.Client, error) {
	// check if impersonation is enabled in config
	impersonateEnabled := false
	retainUA := false
	if proxyConfig.Global != nil && proxyConfig.Global.Impersonate != nil {
		impersonateEnabled = proxyConfig.Global.Impersonate.Enabled
		retainUA = proxyConfig.Global.Impersonate.RetainUA
	}

	if !impersonateEnabled {
		reqCtx.UsedImpersonation = false
		return m.createStandardHTTPClient(proxyConfig)
	}

	// extract user-agent and accept-language from current request headers
	userAgent := req.Header.Get("User-Agent")
	acceptLanguage := req.Header.Get("Accept-Language")

	m.logger.Debugw("impersonation enabled, using surf client",
		"userAgent", userAgent,
		"retainUA", retainUA,
	)

	client, err := m.createSurfClient(userAgent, proxyConfig, acceptLanguage, retainUA)
	if err != nil {
		m.logger.Warnw("failed to create surf client, falling back to standard client",
			"error", err,
		)
		reqCtx.UsedImpersonation = false
		return m.createStandardHTTPClient(proxyConfig)
	}
	reqCtx.UsedImpersonation = true
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

// createStandardHTTPClient creates a standard http client without impersonation
func (m *ProxyHandler) createStandardHTTPClient(proxyConfig *service.ProxyServiceConfigYAML) (*http.Client, error) {
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{},
	}

	if proxyConfig.Proxy != "" {
		proxyURL, err := m.parseProxyURL(proxyConfig.Proxy)
		if err != nil {
			return nil, err
		}

		// handle socks5 proxies
		if proxyURL.Scheme == "socks5" {
			var auth *proxy.Auth
			if proxyURL.User != nil {
				password, _ := proxyURL.User.Password()
				auth = &proxy.Auth{
					User:     proxyURL.User.Username(),
					Password: password,
				}
			}

			// create socks5 dialer
			dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, auth, proxy.Direct)
			if err != nil {
				return nil, err
			}

			client.Transport = &http.Transport{
				Dial: dialer.Dial,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			}
		} else {
			// handle http/https proxies
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			}
		}
	}
	return client, nil
}

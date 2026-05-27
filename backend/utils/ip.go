package utils

import (
	"net"
	"net/http"
	"strings"
)

// ExtractClientIP extracts the real client IP from an HTTP request.
// trustedProxies is a list of proxy IPs/CIDRs whose forwarded headers should
// be trusted. When empty all forwarded headers are trusted (legacy behaviour).
// When non-empty, forwarded headers are only honoured if RemoteAddr is in the
// trusted list; otherwise RemoteAddr is returned directly.
func ExtractClientIP(req *http.Request, trustedProxies []string) string {
	// start with direct connection IP
	clientIP := req.RemoteAddr
	// strip port for comparison and return value
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	if len(trustedProxies) > 0 && !IPMatchesList(clientIP, trustedProxies) {
		return clientIP
	}

	// check common proxy headers in order of preference
	proxyHeaders := []string{
		"CF-Connecting-IP", // cloudflare - most reliable for cloudflare setups
		"True-Client-IP",   // cloudflare enterprise
		"X-Forwarded-For",  // most common proxy header
		"X-Real-IP",        // nginx standard
		"X-Client-IP",      // some proxies
		"Connecting-IP",    // some cdns
		"Client-IP",        // some load balancers
	}

	for _, header := range proxyHeaders {
		if headerValue := req.Header.Get(header); headerValue != "" {
			// take first IP if comma-separated list
			ip := strings.SplitN(headerValue, ",", 2)[0]
			ip = strings.TrimSpace(ip)
			if ip != "" {
				if host, _, err := net.SplitHostPort(ip); err == nil {
					return host
				}
				return ip
			}
		}
	}

	return clientIP
}

// IPMatchesList reports whether ip matches any entry in the list, which may
// contain plain IPs or CIDR ranges.
func IPMatchesList(ip string, list []string) bool {
	parsed := net.ParseIP(ip)
	for _, entry := range list {
		if strings.Contains(entry, "/") {
			_, ipNet, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if parsed != nil && ipNet.Contains(parsed) {
				return true
			}
		} else {
			if ip == entry {
				return true
			}
		}
	}
	return false
}

package utils

import (
	"net"
	"net/http"
	"strings"
)

// IPTrust says which connections may tell us the client IP and how.
type IPTrust struct {
	// Proxies holds the IPs/CIDRs of reverse proxies whose forwarded headers
	// are read. When empty no forwarded header is read.
	Proxies []string
	// Header is the one header a trusted proxy sets with the client IP.
	// When set, only this header is read.
	Header string
}

// ExtractClientIP extracts the real client IP from an HTTP request.
// Forwarded headers are only read when RemoteAddr is one of trust.Proxies.
// When trust.Proxies is empty RemoteAddr is returned, so a client can not pick
// its own IP by sending a header. When trust.Header is set only that header is
// read, so headers the proxy passes through from the client are ignored.
func ExtractClientIP(req *http.Request, trust IPTrust) string {
	trustedProxies := trust.Proxies
	// start with direct connection IP
	clientIP := req.RemoteAddr
	// strip port for comparison and return value
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	if len(trustedProxies) == 0 || !IPMatchesList(clientIP, trustedProxies) {
		return clientIP
	}

	if trust.Header != "" {
		if ip, ok := headerIP(req.Header.Get(trust.Header), trustedProxies); ok {
			return ip
		}
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
			// take first IP if comma-separated list, only a valid IP is used,
			// anything else falls back to the next header and then the
			// connection address
			if ip, ok := parseIP(strings.SplitN(headerValue, ",", 2)[0]); ok {
				return ip
			}
		}
	}

	return clientIP
}

// headerIP reads the client IP from the value of the configured header.
// A comma separated list is read from the right, because each proxy appends
// the address it saw. Entries that are trusted proxies are skipped and the
// first other entry is the client. Entries left of it were sent by the client
// and are ignored. A value that is not an IP gives no result.
func headerIP(value string, trustedProxies []string) (string, bool) {
	if value == "" {
		return "", false
	}
	parts := strings.Split(value, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip, ok := parseIP(parts[i])
		if !ok {
			return "", false
		}
		if i > 0 && IPMatchesList(ip, trustedProxies) {
			continue
		}
		return ip, true
	}
	return "", false
}

// parseIP trims an address, drops a port and returns it in normal form.
// It reports false when the value is not an IP.
func parseIP(value string) (string, bool) {
	ip := strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", false
	}
	return parsed.String(), true
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
			if e := net.ParseIP(entry); e != nil && parsed != nil && e.Equal(parsed) {
				return true
			}
		}
	}
	return false
}

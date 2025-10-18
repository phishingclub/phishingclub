package utils

import (
	"net"
	"net/http"
	"strings"
)

// ExtractClientIP extracts the real client IP from an HTTP request,
// checking common proxy headers in order of preference.
// This provides consistent IP extraction across the application.
func ExtractClientIP(req *http.Request) string {
	// start with direct connection IP
	clientIP := req.RemoteAddr

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

			// use first non-empty value found
			if ip != "" {
				clientIP = ip
				break
			}
		}
	}
	// strip port
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	return clientIP
}

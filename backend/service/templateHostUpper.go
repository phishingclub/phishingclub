package service

import (
	"errors"
	"net"
	"strings"
)

// HostUpper uppercases letters in the host of a URL so a link can show a look
// alike domain such as mIcrosoft.test while the server still matches it, since
// hostnames are case insensitive. letters is found in the host ignoring case.
// The URL is the last argument so the function works in a pipe:
//
//	{{.URL | hostUpper "i"}}    every match
//	{{.URL | hostUpper "i" 2}}  only the second match
//
// Only the host changes and only in case. Scheme, user info, port, path, query
// and fragment are kept byte for byte, so a lure code or a sensitive query
// value is never touched. When the value has no host, the host is an IP, or
// letters is not found, the value is returned unchanged so a template reused
// on another domain still sends a working link.
func HostUpper(letters string, args ...any) (string, error) {
	if letters == "" {
		return "", errors.New("hostUpper: letters must not be empty")
	}
	if len(args) == 0 || len(args) > 2 {
		return "", errors.New("hostUpper: expected letters, an optional occurrence number and a URL")
	}
	rawURL, ok := args[len(args)-1].(string)
	if !ok {
		return "", errors.New("hostUpper: the last argument must be a URL")
	}
	// 0 means every match
	occurrence := 0
	if len(args) == 2 {
		n, ok := args[0].(int)
		if !ok || n < 1 {
			return "", errors.New("hostUpper: occurrence number must be a whole number of 1 or more")
		}
		occurrence = n
	}

	start, end, ok := hostSpan(rawURL)
	if !ok {
		return rawURL, nil
	}
	host := rawURL[start:end]
	if net.ParseIP(host) != nil {
		return rawURL, nil
	}

	lowerHost := asciiLower(host)
	needle := asciiLower(letters)
	b := []byte(rawURL)
	count := 0
	from := 0
	for {
		idx := strings.Index(lowerHost[from:], needle)
		if idx < 0 {
			break
		}
		pos := from + idx
		from = pos + len(needle)
		count++
		if occurrence != 0 && count != occurrence {
			continue
		}
		for i := start + pos; i < start+pos+len(needle); i++ {
			if b[i] >= 'a' && b[i] <= 'z' {
				b[i] -= 'a' - 'A'
			}
		}
		if occurrence != 0 {
			break
		}
	}
	return string(b), nil
}

// hostSpan returns the byte range of the host name in an absolute URL, without
// user info, port or brackets. It works on the raw string so the rest of the
// URL is never parsed and written back.
func hostSpan(rawURL string) (int, int, bool) {
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd <= 0 {
		return 0, 0, false
	}
	for i := 0; i < schemeEnd; i++ {
		c := rawURL[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isOther := i > 0 && ((c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.')
		if !isLetter && !isOther {
			return 0, 0, false
		}
	}
	authStart := schemeEnd + 3
	authEnd := len(rawURL)
	// browsers also end the authority at a backslash for http and https
	if i := strings.IndexAny(rawURL[authStart:], "/?#\\"); i >= 0 {
		authEnd = authStart + i
	}
	hostStart := authStart
	if at := strings.LastIndex(rawURL[authStart:authEnd], "@"); at >= 0 {
		hostStart = authStart + at + 1
	}
	if hostStart < authEnd && rawURL[hostStart] == '[' {
		// IPv6 literal, nothing to style
		return 0, 0, false
	}
	hostEnd := authEnd
	if colon := strings.IndexByte(rawURL[hostStart:authEnd], ':'); colon >= 0 {
		hostEnd = hostStart + colon
	}
	if hostEnd <= hostStart {
		return 0, 0, false
	}
	return hostStart, hostEnd, true
}

// asciiLower lowercases ASCII letters only, so byte positions stay the same as
// in the input
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

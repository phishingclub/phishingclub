package model

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// AllowDeny is a model for allow deny listing
type AllowDeny struct {
	ID              nullable.Nullable[uuid.UUID]     `json:"id"`
	CreatedAt       *time.Time                       `json:"createdAt"`
	UpdatedAt       *time.Time                       `json:"updatedAt"`
	Name            nullable.Nullable[vo.String127]  `json:"name"`
	Cidrs           nullable.Nullable[vo.IPNetSlice] `json:"cidrs"`
	JA4Fingerprints nullable.Nullable[string]        `json:"ja4Fingerprints"`
	CountryCodes    nullable.Nullable[string]        `json:"countryCodes"`
	Headers         nullable.Nullable[string]        `json:"headers"`
	Allowed         nullable.Nullable[bool]          `json:"allowed"`
	CompanyID       nullable.Nullable[uuid.UUID]     `json:"companyID"`
}

// Validate checks if the allow deny list has a valid state
func (r *AllowDeny) Validate() error {
	if err := validate.NullableFieldRequired("name", r.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("filter type", r.Allowed); err != nil {
		return err
	}

	// at least one of cidrs, ja4 fingerprints, country codes, or headers must be provided
	hasCidrs := false
	if r.Cidrs.IsSpecified() {
		if cidrs, err := r.Cidrs.Get(); err == nil && len(cidrs) > 0 {
			hasCidrs = true
		}
	}

	hasJA4 := false
	if r.JA4Fingerprints.IsSpecified() {
		if ja4, err := r.JA4Fingerprints.Get(); err == nil && ja4 != "" {
			hasJA4 = true
		}
	}

	hasCountryCodes := false
	if r.CountryCodes.IsSpecified() {
		if codes, err := r.CountryCodes.Get(); err == nil && codes != "" {
			hasCountryCodes = true
		}
	}

	hasHeaders := false
	if r.Headers.IsSpecified() {
		if headers, err := r.Headers.Get(); err == nil && headers != "" {
			hasHeaders = true
		}
	}

	if !hasCidrs && !hasJA4 && !hasCountryCodes && !hasHeaders {
		return errs.NewValidationError(
			errors.New("at least one of CIDRs, JA4 fingerprints, country codes, or headers must be provided"),
		)
	}

	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (r *AllowDeny) ToDBMap() map[string]any {
	m := map[string]any{}
	if r.Name.IsSpecified() {
		m["name"] = nil
		if name, err := r.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if r.Cidrs.IsSpecified() {
		m["cidrs"] = nil
		if cidrs, err := r.Cidrs.Get(); err == nil {
			cidrsStr := ""
			cidrsLen := len(cidrs)
			for i, cidr := range cidrs {
				if i == cidrsLen {
					cidrsStr += fmt.Sprintf("%s", cidr.String())

				} else {
					cidrsStr += fmt.Sprintf("%s\n", cidr.String())
				}
			}
			m["cidrs"] = cidrsStr
		}
	}
	if r.JA4Fingerprints.IsSpecified() {
		m["ja4_fingerprints"] = ""
		if ja4, err := r.JA4Fingerprints.Get(); err == nil {
			m["ja4_fingerprints"] = ja4
		}
	}
	if r.CountryCodes.IsSpecified() {
		m["country_codes"] = ""
		if codes, err := r.CountryCodes.Get(); err == nil {
			m["country_codes"] = codes
		}
	}
	if r.Headers.IsSpecified() {
		m["headers"] = ""
		if headers, err := r.Headers.Get(); err == nil {
			m["headers"] = headers
		}
	}
	if r.Allowed.IsSpecified() {
		m["allowed"] = nil
		if allowed, err := r.Allowed.Get(); err == nil {
			m["allowed"] = allowed
		}
	}
	if r.CompanyID.IsSpecified() {
		if r.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = r.CompanyID.MustGet()
		}
	}

	return m
}

func (r *AllowDeny) IsIPAllowed(ip string) (bool, error) {
	isTypeAllowList := r.Allowed.MustGet()

	// if no cidrs configured, skip ip check (always pass)
	cidrs, err := r.Cidrs.Get()
	if err != nil || len(cidrs) == 0 {
		return true, nil
	}

	netIP := net.ParseIP(ip)
	if netIP == nil {
		return false, fmt.Errorf("invalid ip address: %s", ip)
	}

	for _, cidr := range cidrs {
		isInRange := cidr.Contains(netIP)
		// if allow list and ip is within range
		if isTypeAllowList && isInRange {
			return true, nil
		}
		// if deny list and ip is within range
		if !isTypeAllowList && isInRange {
			return false, nil
		}
	}

	// If this is an allow list and we didn't find the IP, it's not allowed
	if isTypeAllowList {
		return false, nil
	}

	// If this is a deny list and we didn't find the IP, it is allowed
	return true, nil
}

// IsJA4Allowed checks if a JA4 fingerprint is allowed based on the filter rules
func (r *AllowDeny) IsJA4Allowed(ja4 string) (bool, error) {
	if ja4 == "" {
		// if no ja4 fingerprint available, skip ja4 check
		return true, nil
	}

	isTypeAllowList := r.Allowed.MustGet()

	// get ja4 fingerprints list
	ja4FingerprintsStr, err := r.JA4Fingerprints.Get()
	if err != nil || ja4FingerprintsStr == "" {
		// if no ja4 fingerprints configured, skip ja4 check
		return true, nil
	}

	// parse fingerprints (newline separated)
	fingerprints := parseFingerprints(ja4FingerprintsStr)

	// check if ja4 matches any fingerprint (supports wildcard patterns with *)
	isMatch := false
	for _, fp := range fingerprints {
		if matchJA4Pattern(fp, ja4) {
			isMatch = true
			break
		}
	}

	// if allow list and ja4 matches
	if isTypeAllowList && isMatch {
		return true, nil
	}
	// if deny list and ja4 matches
	if !isTypeAllowList && isMatch {
		return false, nil
	}

	// If this is an allow list and ja4 didn't match, not allowed
	if isTypeAllowList {
		return false, nil
	}

	// If this is a deny list and ja4 didn't match, it is allowed
	return true, nil
}

// matchJA4Pattern checks if a JA4 fingerprint matches a pattern with wildcard support
// supports * as wildcard to match any characters
// examples:
//
//	t13d151*h2_8daaf6152771_* matches any fingerprint with that prefix and cipher hash
//	t13d*_*_* matches any TLS 1.3 fingerprint with SNI
//	* matches everything
func matchJA4Pattern(pattern, ja4 string) bool {
	// exact match (no wildcards)
	if pattern == ja4 {
		return true
	}

	// if pattern doesn't contain *, no match
	if !strings.Contains(pattern, "*") {
		return false
	}

	// wildcard-only pattern matches everything
	if pattern == "*" {
		return true
	}

	// split pattern by * and check each part exists in order
	parts := strings.Split(pattern, "*")
	pos := 0

	for i, part := range parts {
		if part == "" {
			continue
		}

		// find the part in the remaining string
		idx := strings.Index(ja4[pos:], part)
		if idx == -1 {
			return false
		}

		// for first part, must match at beginning
		if i == 0 && idx != 0 {
			return false
		}

		pos += idx + len(part)
	}

	// for last part, must match at end
	lastPart := parts[len(parts)-1]
	if lastPart != "" && !strings.HasSuffix(ja4, lastPart) {
		return false
	}

	return true
}

// parseFingerprints splits newline-separated fingerprints and trims whitespace
func parseFingerprints(input string) []string {
	var result []string
	lines := splitLines(input)
	for _, line := range lines {
		trimmed := trimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// splitLines splits a string by newlines
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSpace removes leading and trailing whitespace
func trimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}

	return s[start:end]
}

// isSpace checks if a byte is whitespace
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// IsCountryAllowed checks if a country code is allowed based on the filter rules
func (r *AllowDeny) IsCountryAllowed(countryCode string) bool {
	if countryCode == "" {
		// if no country code available, skip country check
		return true
	}

	isTypeAllowList := r.Allowed.MustGet()

	// get country codes list
	countryCodesStr, err := r.CountryCodes.Get()
	if err != nil || countryCodesStr == "" {
		// if no country codes configured, skip country check
		return true
	}

	// if country code is empty but we have country filters configured
	if countryCode == "" {
		// in allow list mode: unknown country should be denied
		// in deny list mode: unknown country should be allowed
		if isTypeAllowList {
			return false
		}
		return true
	}

	// parse country codes (newline separated)
	codes := parseCountryCodes(countryCodesStr)

	// check if country code matches any in the list (case-insensitive)
	isMatch := false
	countryCodeUpper := strings.ToUpper(countryCode)
	for _, code := range codes {
		if strings.ToUpper(code) == countryCodeUpper {
			isMatch = true
			break
		}
	}

	// if allow list and country matches
	if isTypeAllowList && isMatch {
		return true
	}
	// if deny list and country matches
	if !isTypeAllowList && isMatch {
		return false
	}

	// If this is an allow list and country didn't match, not allowed
	if isTypeAllowList {
		return false
	}

	// If this is a deny list and country didn't match, it is allowed
	return true
}

// parseCountryCodes splits newline-separated country codes and trims whitespace
func parseCountryCodes(input string) []string {
	var result []string
	lines := splitLines(input)
	for _, line := range lines {
		trimmed := trimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// IsHeaderAllowed checks if request headers are allowed based on the filter rules
// headers parameter is a map of header key-value pairs from the HTTP request
func (r *AllowDeny) IsHeaderAllowed(headers map[string][]string) (bool, error) {
	if headers == nil || len(headers) == 0 {
		// if no headers provided, skip header check
		return true, nil
	}

	isTypeAllowList := r.Allowed.MustGet()

	// get headers filter configuration
	headersStr, err := r.Headers.Get()
	if err != nil || headersStr == "" {
		// if no headers configured, skip header check
		return true, nil
	}

	// parse header rules from json array
	rules, err := parseHeaderRules(headersStr)
	if err != nil {
		return false, err
	}

	if len(rules) == 0 {
		// if no valid rules, skip header check
		return true, nil
	}

	// check if any header rule matches
	isMatch := false
	for _, rule := range rules {
		matched, err := matchHeaderRule(rule, headers)
		if err != nil {
			continue
		}
		if matched {
			isMatch = true
			break
		}
	}

	// if allow list and header matches
	if isTypeAllowList && isMatch {
		return true, nil
	}
	// if deny list and header matches
	if !isTypeAllowList && isMatch {
		return false, nil
	}

	// If this is an allow list and header didn't match, not allowed
	if isTypeAllowList {
		return false, nil
	}

	// If this is a deny list and header didn't match, it is allowed
	return true, nil
}

// HeaderRule represents a header matching rule with key and value regex patterns
type HeaderRule struct {
	KeyRegex   string `json:"keyRegex"`
	ValueRegex string `json:"valueRegex"`
}

// parseHeaderRules parses json array of header rules
func parseHeaderRules(input string) ([]HeaderRule, error) {
	var rules []HeaderRule

	if err := json.Unmarshal([]byte(input), &rules); err != nil {
		return nil, errors.New("invalid header rules json format")
	}

	// validate rules
	for _, rule := range rules {
		if rule.KeyRegex == "" || rule.ValueRegex == "" {
			return nil, errors.New("header key regex and value regex cannot be empty")
		}
	}

	return rules, nil
}

// matchHeaderRule checks if any request header matches the rule
// both key and value must match their respective regex patterns
func matchHeaderRule(rule HeaderRule, headers map[string][]string) (bool, error) {
	keyRegex, err := regexp.Compile(rule.KeyRegex)
	if err != nil {
		return false, err
	}
	valueRegex, err := regexp.Compile(rule.ValueRegex)
	if err != nil {
		return false, err
	}

	// iterate through all headers in the request
	for headerKey, headerValues := range headers {
		// check if header key matches the key regex
		if !keyRegex.MatchString(headerKey) {
			continue
		}

		// if key matches, check if any value matches the value regex
		for _, headerValue := range headerValues {
			if valueRegex.MatchString(headerValue) {
				// both key and value match
				return true, nil
			}
		}
	}

	return false, nil
}

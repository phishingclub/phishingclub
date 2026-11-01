package model

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
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
	Asns            nullable.Nullable[string]        `json:"asns"`
	Headers         nullable.Nullable[string]        `json:"headers"`
	Allowed         nullable.Nullable[bool]          `json:"allowed"`
	CompanyID       nullable.Nullable[uuid.UUID]     `json:"companyID"`
}

// countryCodePattern is a two letter country code
var countryCodePattern = regexp.MustCompile(`^[A-Za-z]{2}$`)

// Validate checks if the allow deny list has a valid state
func (r *AllowDeny) Validate() error {
	if err := validate.NullableFieldRequired("name", r.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("filter type", r.Allowed); err != nil {
		return err
	}

	// each country line must be a two letter code
	countries := r.countryList()
	for _, code := range countries {
		if !countryCodePattern.MatchString(code) {
			return errs.NewValidationError(
				fmt.Errorf("invalid country code: %s", code),
			)
		}
	}

	// each ASN line must be a number, optionally prefixed with AS
	asnLines := parseList(nullableString(r.Asns))
	for _, line := range asnLines {
		if _, ok := parseASN(line); !ok {
			return errs.NewValidationError(
				fmt.Errorf("invalid ASN: %s", line),
			)
		}
	}

	// header rules must be valid json with regexes that compile
	var rules []compiledHeaderRule
	if headersStr := nullableString(r.Headers); strings.TrimSpace(headersStr) != "" {
		var err error
		rules, err = compileHeaderRules(headersStr)
		if err != nil {
			return errs.NewValidationError(err)
		}
	}

	hasCidrs := len(r.cidrList()) > 0
	hasJA4 := len(parseList(nullableString(r.JA4Fingerprints))) > 0
	if !hasCidrs && !hasJA4 && len(countries) == 0 && len(asnLines) == 0 && len(rules) == 0 {
		return errs.NewValidationError(
			errors.New("at least one of CIDRs, JA4 fingerprints, country codes, ASNs, or headers must be provided"),
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
			lines := make([]string, 0, len(cidrs))
			for _, cidr := range cidrs {
				lines = append(lines, cidr.String())
			}
			m["cidrs"] = strings.Join(lines, "\n")
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
	if r.Asns.IsSpecified() {
		m["asns"] = ""
		if asns, err := r.Asns.Get(); err == nil {
			m["asns"] = asns
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

// FilterVisitor is what a filter can match a request on. Empty values mean
// the value is unknown for this request.
type FilterVisitor struct {
	IP      string
	JA4     string
	Country string
	ASNs    []uint32
	Headers map[string][]string
}

// Matches reports whether the visitor matches any of the rules set on the
// filter. A filter lists things: an IP range, countries, ASNs, fingerprints
// and header rules. The visitor matches the filter when it matches any one of
// them, for allow and deny filters alike. Fields left empty never match, and a
// visitor value that is unknown never matches.
func (r *AllowDeny) Matches(v FilterVisitor) (bool, error) {
	if r.matchesIP(v.IP) ||
		r.matchesJA4(v.JA4) ||
		r.matchesCountry(v.Country) ||
		r.matchesASN(v.ASNs) {
		return true, nil
	}
	return r.matchesHeaders(v.Headers)
}

// EvaluateFilters decides if a visitor may pass the filters of a campaign.
// The visitor is blocked when it matches any deny filter. When the campaign
// has allow filters, the visitor must also match at least one of them. It
// returns the filter that decided the outcome when there is one. On error the
// visitor must be treated as blocked.
func EvaluateFilters(filters []*AllowDeny, v FilterVisitor) (bool, *AllowDeny, error) {
	hasAllow := false
	var allowMatch *AllowDeny
	for _, f := range filters {
		isAllow, err := f.Allowed.Get()
		if err != nil {
			return false, f, fmt.Errorf("filter has no type")
		}
		matched, err := f.Matches(v)
		if err != nil {
			return false, f, err
		}
		if !isAllow {
			if matched {
				return false, f, nil
			}
			continue
		}
		hasAllow = true
		if matched && allowMatch == nil {
			allowMatch = f
		}
	}
	if hasAllow && allowMatch == nil {
		return false, nil, nil
	}
	return true, allowMatch, nil
}

// cidrList returns the configured IP ranges
func (r *AllowDeny) cidrList() vo.IPNetSlice {
	cidrs, err := r.Cidrs.Get()
	if err != nil {
		return nil
	}
	return cidrs
}

// countryList returns the configured country codes
func (r *AllowDeny) countryList() []string {
	return parseList(nullableString(r.CountryCodes))
}

// matchesIP reports whether the ip is inside a configured range
func (r *AllowDeny) matchesIP(ip string) bool {
	cidrs := r.cidrList()
	if len(cidrs) == 0 {
		return false
	}
	netIP := net.ParseIP(ip)
	if netIP == nil {
		return false
	}
	for _, cidr := range cidrs {
		if cidr.Contains(netIP) {
			return true
		}
	}
	return false
}

// matchesJA4 reports whether the fingerprint matches a configured pattern
func (r *AllowDeny) matchesJA4(ja4 string) bool {
	if ja4 == "" {
		return false
	}
	for _, fp := range parseList(nullableString(r.JA4Fingerprints)) {
		if matchJA4Pattern(fp, ja4) {
			return true
		}
	}
	return false
}

// matchesCountry reports whether the country is a configured country
func (r *AllowDeny) matchesCountry(countryCode string) bool {
	if countryCode == "" {
		return false
	}
	for _, code := range r.countryList() {
		if strings.EqualFold(code, countryCode) {
			return true
		}
	}
	return false
}

// matchesASN reports whether any of the systems that announce the visitor IP
// is a configured ASN. asns is usually one number, sometimes several.
func (r *AllowDeny) matchesASN(asns []uint32) bool {
	if len(asns) == 0 {
		return false
	}
	for _, line := range parseList(nullableString(r.Asns)) {
		n, ok := parseASN(line)
		if !ok {
			continue
		}
		for _, a := range asns {
			if a == n {
				return true
			}
		}
	}
	return false
}

// matchesHeaders reports whether a request header matches a configured rule
func (r *AllowDeny) matchesHeaders(headers map[string][]string) (bool, error) {
	headersStr := nullableString(r.Headers)
	if strings.TrimSpace(headersStr) == "" || len(headers) == 0 {
		return false, nil
	}
	rules, err := compileHeaderRules(headersStr)
	if err != nil {
		return false, err
	}
	for _, rule := range rules {
		if rule.matches(headers) {
			return true, nil
		}
	}
	return false, nil
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

	// wildcard only pattern matches everything
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

// nullableString returns the value or an empty string when unset or null
func nullableString(n nullable.Nullable[string]) string {
	v, err := n.Get()
	if err != nil {
		return ""
	}
	return v
}

// parseList splits a list on newlines and commas, trims each entry and drops
// empty ones. The admin form stores one entry per line, other sources such as
// the API or package installs may use commas.
func parseList(input string) []string {
	var result []string
	for _, line := range strings.Split(input, "\n") {
		for _, part := range strings.Split(line, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}

// parseASN parses a single ASN line into a number. It accepts an optional AS
// or ASN prefix, so "AS15169", "asn15169" and "15169" are all valid.
func parseASN(line string) (uint32, bool) {
	s := strings.TrimSpace(line)
	if s == "" {
		return 0, false
	}
	lower := strings.ToLower(s)
	lower = strings.TrimPrefix(lower, "asn")
	lower = strings.TrimPrefix(lower, "as")
	lower = strings.TrimSpace(lower)
	if lower == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(lower, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// HeaderRule represents a header matching rule with key and value regex patterns
type HeaderRule struct {
	KeyRegex   string `json:"keyRegex"`
	ValueRegex string `json:"valueRegex"`
}

// compiledHeaderRule is a header rule with its regexes compiled
type compiledHeaderRule struct {
	key   *regexp.Regexp
	value *regexp.Regexp
}

// compileHeaderRules parses a json array of header rules and compiles them
func compileHeaderRules(input string) ([]compiledHeaderRule, error) {
	var rules []HeaderRule
	if err := json.Unmarshal([]byte(input), &rules); err != nil {
		return nil, errors.New("invalid header rules json format")
	}
	out := make([]compiledHeaderRule, 0, len(rules))
	for _, rule := range rules {
		if rule.KeyRegex == "" || rule.ValueRegex == "" {
			return nil, errors.New("header key regex and value regex cannot be empty")
		}
		key, err := regexp.Compile(rule.KeyRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid header key regex: %s", rule.KeyRegex)
		}
		value, err := regexp.Compile(rule.ValueRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid header value regex: %s", rule.ValueRegex)
		}
		out = append(out, compiledHeaderRule{key: key, value: value})
	}
	return out, nil
}

// matches reports whether any request header matches both the key and value
func (c compiledHeaderRule) matches(headers map[string][]string) bool {
	for headerKey, headerValues := range headers {
		if !c.key.MatchString(headerKey) {
			continue
		}
		for _, headerValue := range headerValues {
			if c.value.MatchString(headerValue) {
				return true
			}
		}
	}
	return false
}

// ValidateAllowDenySameType checks that the lists are all allow lists or all
// deny lists. A campaign or an asset can not mix the two.
func ValidateAllowDenySameType(lists []*AllowDeny) error {
	if len(lists) == 0 {
		return nil
	}
	first, err := lists[0].Allowed.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("filter has no type"), "allowDenyIDs")
	}
	for _, list := range lists[1:] {
		allowed, err := list.Allowed.Get()
		if err != nil {
			return validate.WrapErrorWithField(errors.New("filter has no type"), "allowDenyIDs")
		}
		if allowed != first {
			return validate.WrapErrorWithField(errors.New("allow and deny list are mutually exclusive"), "allowDenyIDs")
		}
	}
	return nil
}

// AllowDenyUsableInCompany reports whether a list can be used by something
// that belongs to the company. A global list can be used everywhere. A company
// list can only be used by the same company. A nil company is the global
// context, where only global lists can be used.
func AllowDenyUsableInCompany(list *AllowDeny, companyID *uuid.UUID) bool {
	listCompanyID, err := list.CompanyID.Get()
	if err != nil {
		return UsableInCompany(nil, companyID)
	}
	return UsableInCompany(&listCompanyID, companyID)
}

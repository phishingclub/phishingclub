package model

import (
	"fmt"
	"unicode/utf8"

	"github.com/go-errors/errors"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/validate"
)

// the recipient fields that can pick a recipient's language
const (
	// LanguageRouteLanguage picks the recipient's own language field: a
	// recipient gets their language when every template has it, else the
	// default. It takes no routes.
	LanguageRouteLanguage   = "language"
	LanguageRouteCountry    = "country"
	LanguageRouteCity       = "city"
	LanguageRouteDepartment = "department"
	LanguageRoutePosition   = "position"
	LanguageRouteMisc       = "misc"
)

// LanguageRouteFields are the recipient fields a campaign or program can pick
// languages by. Empty is off. This list is separate from the template mix
// fields.
var LanguageRouteFields = []string{
	LanguageRouteLanguage,
	LanguageRouteCountry,
	LanguageRouteCity,
	LanguageRouteDepartment,
	LanguageRoutePosition,
	LanguageRouteMisc,
}

// MaxLanguageRoutes is the most values a campaign or program can map to a
// language
const MaxLanguageRoutes = 500

// MaxLanguageRouteKeyLength is the longest value in characters, the length of
// the recipient fields
const MaxLanguageRouteKeyLength = 127

// maxRawLanguageRouteKeyBytes is the longest key in bytes taken from a
// request before it is normalized. A key of MaxLanguageRouteKeyLength
// characters fits, with room for spaces that are trimmed.
const maxRawLanguageRouteKeyBytes = 4*MaxLanguageRouteKeyLength + 64

// maxRawLanguageCodeBytes is the longest language code taken from a request
// before it is normalized
const maxRawLanguageCodeBytes = 16

// IsValidLanguageRouteBy reports whether v is a field of LanguageRouteFields
// or empty
func IsValidLanguageRouteBy(v string) bool {
	if v == "" {
		return true
	}
	for _, f := range LanguageRouteFields {
		if v == f {
			return true
		}
	}
	return false
}

// languageRouting holds the language fields of a campaign or a program
type languageRouting struct {
	routeBy  *nullable.Nullable[string]
	fallback *nullable.Nullable[string]
	routes   *nullable.Nullable[map[string]string]
}

// validate checks the routing and writes the routes back with normalized
// keys. When routing is off the default and the routes are cleared. Only the
// fields that are set are checked, so a partial update passes the rest.
func (r languageRouting) validate() error {
	routeBy := ""
	if v, err := r.routeBy.Get(); err == nil {
		routeBy = v
	}
	if !IsValidLanguageRouteBy(routeBy) {
		return validate.WrapErrorWithField(errors.New("unknown field to pick languages by"), "languageRouteBy")
	}
	if r.fallback.IsSpecified() && !r.fallback.IsNull() {
		r.fallback.Set(NormalizeLanguageCode(r.fallback.MustGet()))
	}
	if routeBy == "" {
		if r.routeBy.IsSpecified() {
			// off, nothing is routed
			r.fallback.Set("")
			r.routes.Set(map[string]string{})
			return nil
		}
		if v, err := r.fallback.Get(); err == nil && v != "" {
			return validate.WrapErrorWithField(errors.New("languageRouteBy is required with a default language"), "languageRouteBy")
		}
		if v, err := r.routes.Get(); err == nil && len(v) > 0 {
			return validate.WrapErrorWithField(errors.New("languageRouteBy is required with language routes"), "languageRouteBy")
		}
		return nil
	}
	fallback := ""
	if v, err := r.fallback.Get(); err == nil {
		fallback = v
	}
	if fallback == "" {
		return validate.WrapErrorWithField(errors.New("a default language is required"), "languageDefault")
	}
	if !data.IsValidLanguage(fallback) {
		return validate.WrapErrorWithField(fmt.Errorf("unknown language %q", fallback), "languageDefault")
	}
	routes, err := r.routes.Get()
	if err != nil {
		if r.routes.IsSpecified() {
			r.routes.Set(map[string]string{})
		}
		return nil
	}
	if routeBy == LanguageRouteLanguage {
		if len(routes) > 0 {
			return validate.WrapErrorWithField(
				errors.New("values can not be mapped when languages are picked by the Language field, each recipient gets their own language"),
				"languageRoutes",
			)
		}
		r.routes.Set(map[string]string{})
		return nil
	}
	normalized, err := NormalizeLanguageRoutes(routes)
	if err != nil {
		return err
	}
	r.routes.Set(normalized)
	return nil
}

// NormalizeLanguageRoutes checks routes from a request and returns them with
// keys normalized by NormalizeLanguageKey. Refused are more than
// MaxLanguageRoutes entries, an empty key, a key longer than
// MaxLanguageRouteKeyLength, two keys that are the same after normalizing and
// a code that is not in the fixed list. Keys are recipient values, so they
// are never named in an error.
func NormalizeLanguageRoutes(routes map[string]string) (map[string]string, error) {
	if len(routes) > MaxLanguageRoutes {
		return nil, validate.WrapErrorWithField(
			fmt.Errorf("at most %d values can be mapped to a language", MaxLanguageRoutes),
			"languageRoutes",
		)
	}
	out := make(map[string]string, len(routes))
	for raw, code := range routes {
		// a key or code far too long is refused before any work is done on it
		if len(raw) > maxRawLanguageRouteKeyBytes {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf("a value can be at most %d characters", MaxLanguageRouteKeyLength),
				"languageRoutes",
			)
		}
		if len(code) > maxRawLanguageCodeBytes {
			return nil, validate.WrapErrorWithField(errors.New("unknown language"), "languageRoutes")
		}
		key := NormalizeLanguageKey(raw)
		if key == "" {
			return nil, validate.WrapErrorWithField(
				errors.New("an empty value can not be mapped, it gets the default language"),
				"languageRoutes",
			)
		}
		if utf8.RuneCountInString(key) > MaxLanguageRouteKeyLength {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf("a value can be at most %d characters", MaxLanguageRouteKeyLength),
				"languageRoutes",
			)
		}
		code = NormalizeLanguageCode(code)
		if !data.IsValidLanguage(code) {
			return nil, validate.WrapErrorWithField(fmt.Errorf("unknown language %q", code), "languageRoutes")
		}
		if _, dup := out[key]; dup {
			return nil, validate.WrapErrorWithField(
				errors.New("two values are the same when case and spaces are ignored"),
				"languageRoutes",
			)
		}
		out[key] = code
	}
	return out, nil
}

// LanguageRouteCount returns how many values the routes map, for audit
func LanguageRouteCount(routes nullable.Nullable[map[string]string]) int {
	if v, err := routes.Get(); err == nil {
		return len(v)
	}
	return 0
}

package vo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/phishingclub/phishingclub/validate"
)

var validHTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE", "CONNECT"}

// HTTPMethod is an http method
type HTTPMethod struct {
	inner string
}

// NewHTTPMethod creates a new http method
func NewHTTPMethod(s string) (*HTTPMethod, error) {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	if err := validate.ErrorIfNotContains(validHTTPMethods, s); err != nil {
		return nil, validate.WrapErrorWithField(
			fmt.Errorf("invalid http method: %s", s),
			"HTTPMethod",
		)
	}
	return &HTTPMethod{
		inner: s,
	}, nil
}

// NewHTTPMethodMust creates a new http method, panicking if the input is invalid
func NewHTTPMethodMust(s string) *HTTPMethod {
	ss, err := NewHTTPMethod(s)
	if err != nil {
		panic(err)
	}
	return ss
}

// MarshalJSON implements the json.Marshaler interface
func (s HTTPMethod) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *HTTPMethod) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewHTTPMethod(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the http method
func (s HTTPMethod) String() string {
	return s.inner
}

// HTTPHeader is an http header
type HTTPHeader struct {
	key   string
	value string
}

// NewHTTPHeader creates a new http header
func NewHTTPHeader(key, val string) (*HTTPHeader, error) {
	key = strings.TrimSpace(key)
	val = strings.TrimSpace(val)
	if validate.ErrorIfStringEqual(key, "") != nil {
		return nil, validate.WrapErrorWithField(
			fmt.Errorf("invalid http header key: %s", key),
			"HTTPHeader",
		)
	}
	if validate.ErrorIfStringEqual(val, "") != nil {
		return nil, validate.WrapErrorWithField(
			fmt.Errorf("invalid http header value: %s", val),
			"HTTPHeader",
		)
	}
	return &HTTPHeader{
		key:   key,
		value: val,
	}, nil
}

// Key returns the key of the header
func (h *HTTPHeader) Key() string {
	return h.key
}

// Value returns the value of the header
func (h *HTTPHeader) Value() string {
	return h.value
}

// MarshalJSON implements the json.Marshaler interface
func (s HTTPHeader) MarshalJSON() ([]byte, error) {
	return json.Marshal(s)
}

// UnmarshalJSON unmarshals the json into a string
func (s *HTTPHeader) UnmarshalJSON(data []byte) error {
	var header *HTTPHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	// create instance to validate data
	ss, err := NewHTTPHeader(header.key, header.value)
	if err != nil {
		return unwrapError(err)
	}
	s.key = ss.key
	s.value = ss.value
	return nil
}

// String returns the string representation of the header
func (h HTTPHeader) String() string {
	return fmt.Sprintf("%s: %s", h.key, h.value)
}

// URLPath is the path in a URL (e.g. /api/v1/users)
type URLPath struct {
	inner string
}

// NewURLPath creates a new URL path
func NewURLPath(s string) (*URLPath, error) {
	s = strings.TrimSpace(s)
	p, err := url.Parse(s)
	if err != nil {
		return nil, validate.WrapErrorWithField(
			fmt.Errorf("invalid url path: %s", s),
			"URLPath",
		)
	}
	return &URLPath{
		inner: p.Path,
	}, nil
}

// NewURLPathMust creates a new URL path, panicking if the input is invalid
func NewURLPathMust(s string) *URLPath {
	ss, err := NewURLPath(s)
	if err != nil {
		panic(err)
	}
	return ss
}

// MarshalJSON implements the json.Marshaler interface
func (s URLPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *URLPath) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewURLPath(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the URL path
func (s URLPath) String() string {
	return s.inner
}

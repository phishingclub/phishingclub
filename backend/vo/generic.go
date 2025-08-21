package vo

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
)

// String64 is a trimmed string with a min of 1 and a max of 64
type String64 struct {
	inner string
}

// NewString64 creates a new short string
func NewString64(s string) (*String64, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 1, 64)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &String64{
		inner: s,
	}, nil
}

// NewString64Must creates a new short string and panics if it fails
func NewString64Must(s string) *String64 {
	a, err := NewString64(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s String64) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *String64) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewString64(str)
	if err != nil {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		return unwrapped
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the short string
func (s String64) String() string {
	return s.inner
}

// OptionalString64 is a trimmed string with a min of 0 and a max of 64
type OptionalString64 struct {
	inner string
}

// NewOptionalString64 creates a new short string
func NewOptionalString64(s string) (*OptionalString64, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 0, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid short string: %w", err)
	}
	return &OptionalString64{
		inner: s,
	}, nil
}

// NewOptionalString64Must creates a new short string and panics if it fails
func NewOptionalString64Must(s string) *OptionalString64 {
	a, err := NewOptionalString64(s)
	if err != nil {
		panic(err)
	}
	return a
}

// NewEmptyOptionalString64 creates a new empty short string
func NewEmptyOptionalString64() *OptionalString64 {
	return &OptionalString64{
		inner: "",
	}
}

// String returns the string representation of the short string
func (s OptionalString64) String() string {
	return s.inner
}

// MarshalJSON implements the json.Marshaler interface
func (s OptionalString64) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *OptionalString64) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewOptionalString64(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// OptionalString127 is a trimmed string with a min of 0 and a max of 127
type OptionalString127 struct {
	inner string
}

// NewOptionalString127 creates a new medium string
func NewOptionalString127(s string) (*OptionalString127, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 0, 127)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &OptionalString127{
		inner: s,
	}, nil
}

// NewEmptyOptionalString127Must creates a new empty medium string
func NewOptionalString127Must(s string) *OptionalString127 {
	a, err := NewOptionalString127(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s OptionalString127) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *OptionalString127) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewOptionalString127(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the medium string
func (s OptionalString127) String() string {
	return s.inner
}

// OptionalString255 is a trimmed string with a min of 0 and a max of 255
type OptionalString255 struct {
	inner string
}

// NewOptionalString255 creates a new long string
func NewOptionalString255(s string) (*OptionalString255, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 0, 255)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &OptionalString255{
		inner: s,
	}, nil
}

// NewOptionalString255Must creates a new long string and panics if it fails
func NewOptionalString255Must(s string) *OptionalString255 {
	a, err := NewOptionalString255(s)
	if err != nil {
		panic(err)
	}
	return a
}

// NewEmptyOptionalString255 creates a new empty long string
func NewEmptyOptionalString255() *OptionalString255 {
	return &OptionalString255{
		inner: "",
	}
}

// Set sets the value of the optional string
func (s *OptionalString255) Set(value string) error {
	str := strings.TrimSpace(value)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(str, 0, 255)
	if err != nil {
		return err
	}
	s.inner = value
	return nil
}

// MarshalJSON implements the json.Marshaler interface
func (s OptionalString255) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *OptionalString255) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewOptionalString255(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s OptionalString255) String() string {
	return s.inner
}

// String127 is a trimmed string with a min of 1 and a max of 127
type String127 struct {
	inner string
}

// NewString127 creates a new medium string
func NewString127(s string) (*String127, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 1, 127)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &String127{
		inner: s,
	}, nil
}

// NewString127Must creates a new medium string and panics if it fails
func NewString127Must(s string) *String127 {
	a, err := NewString127(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s String127) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *String127) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewString127(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the medium string
func (s String127) String() string {
	return s.inner
}

// String255 is a trimmed string with a min of 1 and a max of 255
type String255 struct {
	inner string
}

// NewString255 creates a new long string
func NewString255(s string) (*String255, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 1, 255)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &String255{
		inner: s,
	}, nil
}

// NewString255Must creates a new long string and panics if it fails
func NewString255Must(s string) *String255 {
	a, err := NewString255(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s String255) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *String255) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewString255(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s String255) String() string {
	return s.inner
}

// String512 is a trimmed string with a min of 1 and a max of 512
type String512 struct {
	inner string
}

// NewString512 creates a new long string
func NewString512(s string) (*String512, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 1, 512)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &String512{
		inner: s,
	}, nil
}

// NewString512Must creates a new long string and panics if it fails
func NewString512Must(s string) *String512 {
	a, err := NewString512(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s String512) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *String512) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewString512(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s String512) String() string {
	return s.inner
}

// String1024 is a trimmed string with a min of 1 and a max of 1024
type String1024 struct {
	inner string
}

// NewString1024 creates a new long string
func NewString1024(s string) (*String1024, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 1, 1024)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &String1024{
		inner: s,
	}, nil
}

// NewString1024Must creates a new long string and panics if it fails
func NewString1024Must(s string) *String1024 {
	a, err := NewString1024(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s String1024) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *String1024) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewString1024(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s String1024) String() string {
	return s.inner
}

// OptionalString1024 is a trimmed string with a min of 1 and a max of 1024
type OptionalString1024 struct {
	inner string
}

// NewEmptyOptionalString64 creates a new empty short string
func NewEmptyOptionalString1024() *OptionalString1024 {
	return &OptionalString1024{
		inner: "",
	}
}

// NewString1024 creates a new long string
func NewOptionalString1024(s string) (*OptionalString1024, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 0, 1024)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &OptionalString1024{
		inner: s,
	}, nil
}

// NewOptionalString1024Must creates a new long string and panics if it fails
func NewOptionalString1024Must(s string) *OptionalString1024 {
	a, err := NewOptionalString1024(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s OptionalString1024) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *OptionalString1024) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewOptionalString1024(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s OptionalString1024) String() string {
	return s.inner
}

// String1MB is a trimmed string with a min of 1 and a max of 1000000
type String1MB struct {
	inner string
}

// NewString1MB creates a new long string
func NewString1MB(s string) (*String1MB, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 1, 1000000)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &String1MB{
		inner: s,
	}, nil
}

// NewString1MBMust creates a new long string and panics if it fails
func NewString1MBMust(s string) *String1MB {
	a, err := NewString1MB(s)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (s String1MB) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *String1MB) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewString1MB(str)
	if err != nil {
		return errors.Unwrap(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s String1MB) String() string {
	return s.inner
}

// OptionalString1MB is a trimmed string with a min of 0 and a max of 1000000
type OptionalString1MB struct {
	inner string
}

// NewOptionalString1MB creates a new long string
func NewOptionalString1MB(s string) (*OptionalString1MB, error) {
	s = strings.TrimSpace(s)
	err := validate.ErrorIfStringNotbetweenOrEqualTo(s, 0, 1000000)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &OptionalString1MB{
		inner: s,
	}, nil
}

// NewUnsafeString1MB creates a new long string but does validate the size
// this is a special unsafe method that should only be used for special occasions
func NewUnsafeOptionalString1MB(s string) *OptionalString1MB {
	return &OptionalString1MB{
		inner: s,
	}
}

// NewOptionalString1MBMust creates a new long string and panics if it fails
func NewOptionalString1MBMust(s string) *OptionalString1MB {
	a, err := NewOptionalString1MB(s)
	if err != nil {
		panic(err)
	}
	return a
}

// NewEmptyOptionalString1MB creates a new empty long string
func NewEmptyOptionalString1MB() *OptionalString1MB {
	return &OptionalString1MB{
		inner: "",
	}
}

// MarshalJSON implements the json.Marshaler interface
func (s OptionalString1MB) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *OptionalString1MB) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewOptionalString1MB(str)
	if err != nil {
		return errors.Unwrap(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the long string
func (s OptionalString1MB) String() string {
	return s.inner
}

// Port is a network port number
type Port struct {
	inner uint16
}

// NewPort creates a new port
func NewPort(p uint16) (*Port, error) {
	err := validate.ErrorIfNotbetweenOrEqualTo(int(p), 1, 65535)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &Port{
		inner: p,
	}, nil
}

// NewPortMust creates a new port and panics if it fails
func NewPortMust(p uint16) *Port {
	a, err := NewPort(p)
	if err != nil {
		panic(err)
	}
	return a
}

// MarshalJSON implements the json.Marshaler interface
func (p Port) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.inner)
}

// UnmarshalJSON unmarshals the json into a port
func (p *Port) UnmarshalJSON(data []byte) error {
	var port uint16
	if err := json.Unmarshal(data, &port); err != nil {
		return err
	}
	pp, err := NewPort(port)
	if err != nil {
		return unwrapError(err)
	}
	p.inner = pp.inner
	return nil
}

// Uint16 returns the uint16 representation of the port
func (p Port) Uint16() uint16 {
	return p.inner
}

func (p Port) Int() int {
	return int(p.inner)
}

func (p Port) IntAsString() string {
	return strconv.Itoa(int(p.inner))
}

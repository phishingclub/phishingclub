package vo

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phishingclub/phishingclub/random"
	"github.com/phishingclub/phishingclub/validate"
)

// UserFullname is a user name
type UserFullname struct {
	inner string
}

// NewUserFullname creates a new name for a user
func NewUserFullname(name string) (*UserFullname, error) {
	err := validate.ErrorIfStringNotBetween(name, 0, 65)
	if err != nil {
		return nil, fmt.Errorf("invalid name: %w", err)
	}
	return &UserFullname{
		inner: name,
	}, nil
}

// NewUserFUllnameMust creates a new name for a user, panicking if the input is invalid
func NewUserFullnameMust(name string) *UserFullname {
	ss, err := NewUserFullname(name)
	if err != nil {
		panic(err)
	}
	return ss
}

// MarshalJSON implements the json.Marshaler interface
func (s UserFullname) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *UserFullname) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewUserFullname(str)
	if err != nil {
		return unwrapError(err)
	}
	s.inner = ss.inner
	return nil
}

// String returns the string representation of the name
func (n UserFullname) String() string {
	return n.inner
}

// Username is a username
type Username struct {
	inner string
}

// NewUsername creates a new username
func NewUsername(username string) (*Username, error) {
	err := validate.ErrorIfStringNotBetween(username, 0, 65)
	if err != nil {
		return nil, fmt.Errorf("invalid username: %w", err)
	}
	err = validate.ErrorIfNotAlphaNumeric(username)
	if err != nil {
		return nil, fmt.Errorf("invalid username: %w", err)
	}
	username = strings.ToLower(username)
	return &Username{
		inner: username,
	}, nil
}

// NewUsernameMust creates a new username, panicking if the input is invalid
func NewUsernameMust(username string) *Username {
	ss, err := NewUsername(username)
	if err != nil {
		panic(err)
	}
	return ss
}

// MarshalJSON implements the json.Marshaler interface
func (u Username) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (u *Username) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	uu, err := NewUsername(str)
	if err != nil {
		return unwrapError(err)
	}
	u.inner = uu.inner
	return nil
}

// String returns the string representation of the username
func (u Username) String() string {
	return u.inner
}

// ReasonableLengthPassword is a reasonable length password
type ReasonableLengthPassword struct {
	password string
}

// NewReasonableLengthPassword creates a new reasonable secure password
func NewReasonableLengthPassword(password string) (*ReasonableLengthPassword, error) {
	err := validate.ErrorIfStringNotBetween(password, 15, 65)
	if err != nil {
		return nil, fmt.Errorf("invalid password: %w", err)
	}
	return &ReasonableLengthPassword{
		password: password,
	}, nil
}

// NewReasonableLengthPasswordGenerated creates a new reasonable length password
func NewReasonableLengthPasswordGenerated() (*ReasonableLengthPassword, error) {
	password, err := random.GenerateRandomURLBase64Encoded(32)
	if err != nil {
		return nil, fmt.Errorf("could not generate password: %w", err)
	}
	return NewReasonableLengthPassword(password)
}

// MarshalJSON implements the json.Marshaler interface
func (s ReasonableLengthPassword) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.password)
}

// UnmarshalJSON unmarshals the json into a string
func (s *ReasonableLengthPassword) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewReasonableLengthPassword(str)
	if err != nil {
		return unwrapError(err)
	}
	s.password = ss.password
	return nil
}

// String returns the password
func (p ReasonableLengthPassword) String() string {
	return p.password
}

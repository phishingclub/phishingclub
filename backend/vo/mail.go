package vo

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phishingclub/phishingclub/validate"
)

// Email is an email
type Email struct {
	inner string
}

// NewEmail creates a new email
func NewEmail(email string) (*Email, error) {
	e := strings.TrimSpace(email)
	err := validate.ErrorIfMailInvalid(e)
	if err != nil {
		return nil, fmt.Errorf("invalid email: %w", err)
	}
	return &Email{
		inner: email,
	}, nil
}

// NewEmailMust creates a new email and panics if it is invalid
func NewEmailMust(email string) *Email {
	e, err := NewEmail(email)
	if err != nil {
		panic(err)
	}
	return e
}

// MarshalJSON implements the json.Marshaler interface
func (e Email) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (e *Email) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewEmail(str)
	if err != nil {
		return unwrapError(err)
	}
	e.inner = ss.inner
	return nil
}

// String returns the string representation of the email
func (e Email) String() string {
	return e.inner
}

// OptionalEmail is an optional email
type OptionalEmail struct {
	inner *Email
}

// NewOptionalEmail creates a new optional email
func NewOptionalEmail(email string) (*OptionalEmail, error) {
	if email == "" {
		return &OptionalEmail{
			inner: nil,
		}, nil
	}
	e, err := NewEmail(email)
	if err != nil {
		return nil, fmt.Errorf("invalid optional email: %w", err)
	}
	return &OptionalEmail{
		inner: e,
	}, nil
}

// NewOptionalEmailMust creates a new optional email and panics if it is invalid
func NewOptionalEmailMust(email string) *OptionalEmail {
	e, err := NewOptionalEmail(email)
	if err != nil {
		panic(err)
	}
	return e
}

// MarshalJSON implements the json.Marshaler interface
func (e OptionalEmail) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (e *OptionalEmail) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewOptionalEmail(str)
	if err != nil {
		return unwrapError(err)
	}
	e.inner = ss.inner
	return nil
}

// String returns the string representation of the optional email
func (e OptionalEmail) String() string {
	if e.inner == nil {
		return ""
	}
	return e.inner.String()
}

// MailEnvelopeFrom is the envelope header - Bounce / Return-Path
// the simplified format is: <email> as in <user@domain.tld>
type MailEnvelopeFrom struct {
	inner string
}

// NewMailEnvelopeFrom creates a new mail from
func NewMailEnvelopeFrom(mailFrom string) (*MailEnvelopeFrom, error) {
	email, err := NewEmail(mailFrom)
	if err != nil {
		return nil, fmt.Errorf("invalid envelope from: %w", err)
	}
	// TODO figure if this is required to be in <> or not
	return &MailEnvelopeFrom{
		inner: email.String(),
	}, nil
}

// NewMailEnvelopeFromMust creates a new mail from and panics if it is invalid
func NewMailEnvelopeFromMust(mailFrom string) *MailEnvelopeFrom {
	e, err := NewMailEnvelopeFrom(mailFrom)
	if err != nil {
		panic(err)
	}
	return e
}

// MarshalJSON implements the json.Marshaler interface
func (m MailEnvelopeFrom) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (m *MailEnvelopeFrom) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	ss, err := NewMailEnvelopeFrom(str)
	if err != nil {
		return unwrapError(err)
	}
	m.inner = ss.inner
	return nil
}

// String returns the string representation of the mail from
// the format is "Sender Name <sender@domain.tld>"
func (m MailEnvelopeFrom) String() string {
	return m.inner
}

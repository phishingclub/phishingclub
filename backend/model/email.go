package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Email is a e-mail
type Email struct {
	ID                nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt         *time.Time                              `json:"createdAt"`
	UpdatedAt         *time.Time                              `json:"updatedAt"`
	Name              nullable.Nullable[vo.String64]          `json:"name"`
	MailEnvelopeFrom  nullable.Nullable[vo.MailEnvelopeFrom]  `json:"mailEnvelopeFrom"` // Bounce / Return-Path
	MailHeaderFrom    nullable.Nullable[vo.Email]             `json:"mailHeaderFrom"`
	MailHeaderSubject nullable.Nullable[vo.OptionalString255] `json:"mailHeaderSubject"`
	Content           nullable.Nullable[vo.OptionalString1MB] `json:"content"`
	AddTrackingPixel  nullable.Nullable[bool]                 `json:"addTrackingPixel"`
	CompanyID         nullable.Nullable[uuid.UUID]            `json:"companyID"`

	Attachments []*EmailAttachment `json:"attachments"`
	Company     *Company            `json:"company"`
}

// Validate checks if the mail has a valid state
func (m *Email) Validate() error {
	if err := validate.NullableFieldRequired("name", m.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("mailEnvelopeFrom", m.MailEnvelopeFrom); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("mailHeaderSubject", m.MailHeaderSubject); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("addTrackingPixel", m.MailHeaderFrom); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("Content", m.Content); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (s *Email) ToDBMap() map[string]any {
	m := map[string]any{}
	if s.Name.IsSpecified() {
		m["name"] = nil
		if name, err := s.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if s.MailEnvelopeFrom.IsSpecified() {
		m["mail_from"] = nil
		if envelopeFrom, err := s.MailEnvelopeFrom.Get(); err == nil {
			m["mail_from"] = envelopeFrom.String()
		}
	}
	if s.MailHeaderFrom.IsSpecified() {
		m["from"] = nil
		if headerFrom, err := s.MailHeaderFrom.Get(); err == nil {
			m["from"] = headerFrom.String()
		}
	}
	if s.MailHeaderSubject.IsSpecified() {
		m["subject"] = nil
		if headerSubject, err := s.MailHeaderSubject.Get(); err == nil {
			m["subject"] = headerSubject.String()
		}
	}
	if s.Content.IsSpecified() {
		m["content"] = nil
		if content, err := s.Content.Get(); err == nil {
			m["content"] = content.String()
		}
	}
	if s.AddTrackingPixel.IsSpecified() {
		m["add_tracking_pixel"] = nil
		if addTrackingPixel, err := s.AddTrackingPixel.Get(); err == nil {
			m["add_tracking_pixel"] = addTrackingPixel
		}
	}
	if s.CompanyID.IsSpecified() {
		if s.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = s.CompanyID.MustGet()
		}
	}
	return m
}

func NewEmailExample() *Email {
	return &Email{
		Name: nullable.NewNullableWithValue(
			*vo.NewString64Must("ExampleEmail"),
		),
		MailEnvelopeFrom: nullable.NewNullableWithValue(
			*vo.NewMailEnvelopeFromMust("sender@example.test"),
		),
		MailHeaderFrom: nullable.NewNullableWithValue(
			*vo.NewEmailMust("Mallory <m@example.test>"),
		),
		MailHeaderSubject: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must("SubjectLine"),
		),
		Content: nullable.NewNullableWithValue(
			*vo.NewOptionalString1MBMust("Content"),
		),
		AddTrackingPixel: nullable.NewNullableWithValue(true),
	}
}

// EmailOverview is a e-mail model without content and attachments
type EmailOverview struct {
	ID                nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt         *time.Time                              `json:"createdAt"`
	UpdatedAt         *time.Time                              `json:"updatedAt"`
	Name              nullable.Nullable[vo.String64]          `json:"name"`
	MailEnvelopeFrom  nullable.Nullable[vo.MailEnvelopeFrom]  `json:"mailEnvelopeFrom"` // Bounce / Return-Path
	MailHeaderFrom    nullable.Nullable[vo.Email]             `json:"mailHeaderFrom"`
	MailHeaderSubject nullable.Nullable[vo.OptionalString255] `json:"mailHeaderSubject"`
	AddTrackingPixel  nullable.Nullable[bool]                 `json:"addTrackingPixel"`
	CompanyID         nullable.Nullable[uuid.UUID]            `json:"companyID"`

	Company *Company `json:"company"`
}

// EmailAttachment represents an attachment associated with an email
// with additional metadata about how it should be displayed
type EmailAttachment struct {
	*Attachment
	IsInline bool `json:"isInline"` // if true, use Content-Disposition: inline and set Content-ID for cid: references
}

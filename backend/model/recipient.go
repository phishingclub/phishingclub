package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Recipient is a Recipient
type Recipient struct {
	ID              nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt       *time.Time                              `json:"createdAt"`
	UpdatedAt       *time.Time                              `json:"updatedAt"`
	Email           nullable.Nullable[vo.Email]             `json:"email"`
	Phone           nullable.Nullable[vo.OptionalString127] `json:"phone"`
	ExtraIdentifier nullable.Nullable[vo.OptionalString127] `json:"extraIdentifier"`
	FirstName       nullable.Nullable[vo.OptionalString127] `json:"firstName"`
	LastName        nullable.Nullable[vo.OptionalString127] `json:"lastName"`
	Position        nullable.Nullable[vo.OptionalString127] `json:"position"`
	Department      nullable.Nullable[vo.OptionalString127] `json:"department"`
	City            nullable.Nullable[vo.OptionalString127] `json:"city"`
	Country         nullable.Nullable[vo.OptionalString127] `json:"country"`
	Misc            nullable.Nullable[vo.OptionalString127] `json:"misc"`
	CompanyID       nullable.Nullable[uuid.UUID]            `json:"companyID"`

	Company *Company                             `json:"-"`
	Groups  nullable.Nullable[[]*RecipientGroup] `json:"groups"`
}

// Validate checks if the recipient has a valid state
func (r *Recipient) Validate() error {
	if err := validate.NullableFieldRequired("email", r.Email); err != nil {
		return err
	}
	return nil
}

// NullifyEmptyOptionals sets empty values to a nullable null, so they are not overwritten
func (r *Recipient) NullifyEmptyOptionals() {
	if r.Phone.IsSpecified() && !r.Phone.IsNull() && r.Phone.MustGet().String() == "" {
		r.Phone.SetNull()
	}
	if r.ExtraIdentifier.IsSpecified() && !r.ExtraIdentifier.IsNull() && r.ExtraIdentifier.MustGet().String() == "" {
		r.ExtraIdentifier.SetNull()
	}
	if r.FirstName.IsSpecified() && !r.FirstName.IsNull() && r.FirstName.MustGet().String() == "" {
		r.FirstName.SetNull()
	}

	if r.LastName.IsSpecified() && !r.LastName.IsNull() && r.LastName.MustGet().String() == "" {
		r.LastName.SetNull()
	}
	if r.Position.IsSpecified() && !r.Position.IsNull() && r.Position.MustGet().String() == "" {
		r.Position.SetNull()
	}
	if r.Department.IsSpecified() && !r.Department.IsNull() && r.Department.MustGet().String() == "" {
		r.Department.SetNull()
	}
	if r.City.IsSpecified() && !r.City.IsNull() && r.City.MustGet().String() == "" {
		r.City.SetNull()
	}
	if r.Country.IsSpecified() && !r.Country.IsNull() && r.Country.MustGet().String() == "" {
		r.Country.SetNull()
	}
	if r.Misc.IsSpecified() && !r.Misc.IsNull() && r.Misc.MustGet().String() == "" {
		r.Misc.SetNull()
	}
}

// EmptyStringNulledOptionals sets nulled optional values to a empty string or zero value.
func (r *Recipient) EmptyStringNulledOptionals() {
	if r.Phone.IsSpecified() && r.Phone.IsNull() {
		r.Phone.Set(*vo.NewOptionalString127Must(""))
	}
	if r.ExtraIdentifier.IsSpecified() && r.ExtraIdentifier.IsNull() {
		r.ExtraIdentifier.Set(*vo.NewOptionalString127Must(""))
	}
	if r.FirstName.IsSpecified() && r.FirstName.IsNull() {
		r.FirstName.Set(*vo.NewOptionalString127Must(""))
	}
	if r.LastName.IsSpecified() && r.LastName.IsNull() {
		r.LastName.Set(*vo.NewOptionalString127Must(""))
	}
	if r.Position.IsSpecified() && r.Position.IsNull() {
		r.Position.Set(*vo.NewOptionalString127Must(""))
	}
	if r.Department.IsSpecified() && r.Department.IsNull() {
		r.Department.Set(*vo.NewOptionalString127Must(""))
	}
	if r.City.IsSpecified() && r.City.IsNull() {
		r.City.Set(*vo.NewOptionalString127Must(""))
	}
	if r.Country.IsSpecified() && r.Country.IsNull() {
		r.Country.Set(*vo.NewOptionalString127Must(""))
	}
	if r.Misc.IsSpecified() && r.Misc.IsNull() {
		r.Misc.Set(*vo.NewOptionalString127Must(""))
	}
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (r *Recipient) ToDBMap() map[string]any {
	m := map[string]any{}
	if r.Email.IsSpecified() {
		m["email"] = nil
		if email, err := r.Email.Get(); err == nil {
			if email.String() == "" {
				m["email"] = nil // due to the unique constraint
			} else {
				m["email"] = email.String()
			}
		}
	}
	if r.Phone.IsSpecified() {
		m["phone"] = nil
		if phone, err := r.Phone.Get(); err == nil {
			if phone.String() == "" {
				m["phone"] = nil // due to the unique constraint
			} else {
				m["phone"] = phone.String()
			}
		}
	}
	if r.ExtraIdentifier.IsSpecified() {
		m["extra_identifier"] = nil
		if extraIdentifier, err := r.ExtraIdentifier.Get(); err == nil {
			if extraIdentifier.String() == "" {
				m["extra_identifier"] = nil // due to the unique constraint
			} else {
				m["extra_identifier"] = extraIdentifier.String()
			}
		}
	}
	if r.FirstName.IsSpecified() {
		m["first_name"] = nil
		if firstName, err := r.FirstName.Get(); err == nil {
			m["first_name"] = firstName.String()
		}
	}
	if r.LastName.IsSpecified() {
		m["last_name"] = nil
		if lastName, err := r.LastName.Get(); err == nil {
			m["last_name"] = lastName.String()
		}
	}
	if r.Position.IsSpecified() {
		m["position"] = nil
		if position, err := r.Position.Get(); err == nil {
			m["position"] = position.String()
		}
	}
	if r.Department.IsSpecified() {
		m["department"] = nil
		if department, err := r.Department.Get(); err == nil {
			m["department"] = department.String()
		}
	}
	if r.City.IsSpecified() {
		m["city"] = nil
		if city, err := r.City.Get(); err == nil {
			m["city"] = city.String()
		}
	}
	if r.Country.IsSpecified() {
		m["country"] = nil
		if country, err := r.Country.Get(); err == nil {
			m["country"] = country.String()
		}
	}
	if r.Misc.IsSpecified() {
		m["misc"] = nil
		if misc, err := r.Misc.Get(); err == nil {
			m["misc"] = misc.String()
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

func NewRecipientExample() *Recipient {
	return &Recipient{
		Email: nullable.NewNullableWithValue(
			*vo.NewEmailMust("Rick <rick@company.test>"),
		),
		Phone: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("1234567890"),
		),
		ExtraIdentifier: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("ExtraIdentifier"),
		),
		FirstName: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("Rick"),
		),
		LastName: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("Xanders"),
		),
		Position: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("CEO"),
		),
		Department: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("IT"),
		),
		City: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("Fredericia"),
		),
		Country: nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must("Denmark"),
		),
	}
}

package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

const SYSTEM_USER_ID = "3eb19071-fbbb-4736-9991-02ba532a7849"

// User is a user of the system, including the company and role
type User struct {
	ID                   nullable.Nullable[uuid.UUID]       `json:"id"`
	CreatedAt            *time.Time                         `json:"createdAt"`
	UpdatedAt            *time.Time                         `json:"updatedAt"`
	Name                 nullable.Nullable[vo.UserFullname] `json:"name"`
	Username             nullable.Nullable[vo.Username]     `json:"username"`
	Email                nullable.Nullable[vo.Email]        `json:"email"`
	RequirePasswordRenew nullable.Nullable[bool]            `json:"requirePasswordRenew"`
	CompanyID            nullable.Nullable[uuid.UUID]       `json:"companyID"`
	Company              *Company                           `json:"company"`
	RoleID               nullable.Nullable[uuid.UUID]       `json:"roleID"`
	Role                 *Role                              `json:"role"`
	SSOID                nullable.Nullable[string]          `json:"ssoID"`
	// apiKey is only get/set externally from this and never output except when created
}

// Validate checks if the user has a valid state
func (u *User) Validate() error {
	if err := validate.NullableFieldRequired("name", u.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("username", u.Username); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("email", u.Email); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (u *User) ToDBMap() map[string]any {
	m := map[string]any{}
	if u.Name.IsSpecified() {
		m["name"] = nil
		if name, err := u.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if u.Username.IsSpecified() {
		m["username"] = nil
		if username, err := u.Username.Get(); err == nil {
			m["username"] = username.String()
		}
	}
	if u.Email.IsSpecified() {
		m["email"] = nil
		if email, err := u.Email.Get(); err == nil {
			m["email"] = email.String()
		}
	}
	if u.RequirePasswordRenew.IsSpecified() {
		m["require_password_renew"] = nil
		if requirePasswordRenew, err := u.RequirePasswordRenew.Get(); err == nil {
			m["require_password_renew"] = requirePasswordRenew
		}
	}
	if u.CompanyID.IsSpecified() {
		m["company_id"] = nil
		if companyID, err := u.CompanyID.Get(); err == nil {
			m["company_id"] = companyID
		}
	}
	if u.RoleID.IsSpecified() {
		m["role_id"] = nil
		if roleID, err := u.RoleID.Get(); err == nil {
			m["role_id"] = roleID
		}
	}

	if u.SSOID.IsSpecified() {
		m["sso_id"] = nil
		if ssoID, err := u.SSOID.Get(); err == nil {
			m["sso_id"] = ssoID
		}
	}
	return m
}

// UserUpsertRequest is a request for creating a new user
type UserUpsertRequest struct {
	Username vo.Username                 `json:"username"`
	Password vo.ReasonableLengthPassword `json:"password"`
	Email    vo.Email                    `json:"email"`
	Fullname vo.UserFullname             `json:"fullname"`
}

// UserChangeEmailRequest is a request for changing the email of a user
type UserChangeEmailRequest struct {
	Email vo.Email `json:"email"`
}

// UserChangeFullnameRequest is the change fullname request
type UserChangeFullnameRequest struct {
	NewFullname vo.UserFullname `json:"fullname"`
}

type InvalidateAllSessionRequest struct {
	UserID *uuid.UUID `json:"userID"`
}

// UserChangePasswordRequest is a request for changing password
type UserChangePasswordRequest struct {
	CurrentPassword vo.ReasonableLengthPassword `json:"currentPassword" binding:"required"`
	NewPassword     vo.ReasonableLengthPassword `json:"newPassword" binding:"required"`
}

// UserChangeUsernameOnLoggedInRequest is the change username request
type UserChangeUsernameOnLoggedInRequest struct {
	NewUsername vo.Username `json:"username"`
}

// NewUser creates a new user which is used for internal system actions and
// cant not be used to login or by a human.
func NewSystemUser() (*User, error) {
	role := &Role{
		Name: data.RoleSystem,
	}
	id := uuid.MustParse(SYSTEM_USER_ID)
	return &User{
		ID:       nullable.NewNullableWithValue(id),
		Name:     nullable.NewNullableWithValue(*vo.NewUserFullnameMust("system")),
		Username: nullable.NewNullableWithValue(*vo.NewUsernameMust("system")),
		Email: nullable.NewNullableWithValue(
			*vo.NewEmailMust("system@example.com"),
		),
		RequirePasswordRenew: nullable.NewNullableWithValue(false),
		Company:              nil,
		Role:                 role,
	}, nil
}

type APIUser struct {
	APIKeyHash [32]byte
	ID         *uuid.UUID
}

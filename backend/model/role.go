package model

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
)

// Role is user role and defines the permissions of the user
type Role struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Validate checks if the role has a valid state
func (r *Role) Validate() error {
	if err := validateRoleName(r.Name); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (r *Role) ToDBMap() map[string]any {
	m := map[string]any{}
	if r.Name != "" {
		m["name"] = r.Name
	}
	return m
}

func (r *Role) IsAuthorized(permission string) bool {
	perms := r.Permissions()
	for _, perm := range perms {
		if perm == permission {
			return true
		}
	}
	return false
}

// Permissions gets the permissions of the role
func (r *Role) Permissions() []string {
	perms, ok := data.RolePermissions[r.Name]
	if !ok {
		return []string{}
	}
	return perms
}

// IsSuperAdministrator checks if the role is a super administrator
func (r *Role) IsSuperAdministrator() bool {
	return r.Name == data.RoleSuperAdministrator
}

func validateRoleName(name string) error {
	// ensure only valid role names are used
	switch name {
	case data.RoleSystem:
	case data.RoleSuperAdministrator:
	case data.RoleCompanyUser:
	default:
		return fmt.Errorf("invalid role name: %s", name)
	}
	return nil
}

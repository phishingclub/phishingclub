package model

import (
	"fmt"
	"net"
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
	ID        nullable.Nullable[uuid.UUID]     `json:"id"`
	CreatedAt *time.Time                       `json:"createdAt"`
	UpdatedAt *time.Time                       `json:"updatedAt"`
	Name      nullable.Nullable[vo.String127]  `json:"name"`
	Cidrs     nullable.Nullable[vo.IPNetSlice] `json:"cidrs"`
	Allowed   nullable.Nullable[bool]          `json:"allowed"`
	CompanyID nullable.Nullable[uuid.UUID]     `json:"companyID"`
}

// Validate checks if the allow deny list has a valid state
func (r *AllowDeny) Validate() error {
	if err := validate.NullableFieldRequired("name", r.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("cidrs", r.Cidrs); err != nil {
		return err
	}
	if v := r.Cidrs.MustGet(); len(v) == 0 {
		return errs.NewValidationError(
			errors.New("cidrs must include atleast one CIDR"),
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
			cidrsStr := ""
			cidrsLen := len(cidrs)
			for i, cidr := range cidrs {
				if i == cidrsLen {
					cidrsStr += fmt.Sprintf("%s", cidr.String())

				} else {
					cidrsStr += fmt.Sprintf("%s\n", cidr.String())
				}
			}
			m["cidrs"] = cidrsStr
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

func (r *AllowDeny) IsIPAllowed(ip string) (bool, error) {
	isTypeAllowList := r.Allowed.MustGet()
	cidrs, err := r.Cidrs.Get()
	if err != nil {
		return false, errs.Wrap(err)
	}

	netIP := net.ParseIP(ip)
	if netIP == nil {
		return false, fmt.Errorf("invalid ip address: %s", ip)
	}

	for _, cidr := range cidrs {
		isInRange := cidr.Contains(netIP)
		// if allow list and ip is within range
		if isTypeAllowList && isInRange {
			return true, nil
		}
		// if deny list and ip is within range
		if !isTypeAllowList && isInRange {
			return false, nil
		}
	}

	// If this is an allow list and we didn't find the IP, it's not allowed
	if isTypeAllowList {
		return false, nil
	}

	// If this is a deny list and we didn't find the IP, it is allowed
	return true, nil
}

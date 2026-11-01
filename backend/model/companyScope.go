package model

import "github.com/google/uuid"

// UsableInCompany reports whether a resource owned by ownerCompanyID can be
// used by something in the companyID context. A global resource (nil owner)
// can be used everywhere. A company resource can only be used by the same
// company. A nil companyID is the global context, where only global resources
// can be used.
func UsableInCompany(ownerCompanyID *uuid.UUID, companyID *uuid.UUID) bool {
	if ownerCompanyID == nil {
		return true
	}
	return companyID != nil && *ownerCompanyID == *companyID
}

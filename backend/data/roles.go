package data

// This is name key for the different roles
const (
	// RoleSystem is the system role
	// is is reserved for system actions only
	RoleSystem = "system"
	// RoleSuperAdministrator is the super administrator role
	// this role has access to everything a user can do
	RoleSuperAdministrator = "superadministrator"
	// RoleCompanyAdministrator is the company role
	// this role had read access to their associated company
	RoleCompanyUser = "companyuser"
)

// RolePermissions is a map of roles to their permissions
// these are the roles and their permissions
var RolePermissions = map[string][]string{
	RoleSystem: {
		PERMISSION_ALLOW_GLOBAL,
	},
	RoleSuperAdministrator: {
		PERMISSION_ALLOW_GLOBAL,
	},
	RoleCompanyUser: {},
}

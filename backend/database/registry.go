package database

import (
	"time"

	"github.com/google/uuid"
)

// Table names for the content registry consumer. A registry is a remote baitbox
// a user connects to; an installed package is one snapshot a user installed; an
// installed resource links a real entity (page/email/asset) created by an install
// back to its source package so updates and local edits can be tracked.
const (
	REGISTRY_TABLE                 = "registries"
	INSTALLED_PACKAGE_TABLE        = "installed_packages"
	INSTALLED_RESOURCE_TABLE       = "installed_resources"
	INSTALLED_PACKAGE_MEMBER_TABLE = "installed_package_members"
)

// Registry is a connection to a remote content registry (baitbox). Every download
// is verified against the archive sha256 and the manifest content and per file
// hashes. The token authorizes private registries.
type Registry struct {
	ID          *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt   *time.Time `gorm:"not null;index;"`
	UpdatedAt   *time.Time `gorm:"not null;index"`
	Name        string     `gorm:"not null;index;unique"`
	URL         string     `gorm:"not null"`
	Token       string     `gorm:""`
	InsecureTLS bool       `gorm:"not null;default:false"`
}

func (Registry) TableName() string { return REGISTRY_TABLE }

// InstalledPackage records one installed package snapshot. ContentHash is the
// package version content hash at install time, compared to the registry current
// to decide whether an update is available.
type InstalledPackage struct {
	ID          *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt   *time.Time `gorm:"not null;index"`
	UpdatedAt   *time.Time `gorm:"not null;index"`
	RegistryID  *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_installed_pkg_reg_name"`
	PackageID   string     `gorm:"not null;index;uniqueIndex:idx_installed_pkg_reg_name"`
	PackageType string     `gorm:"not null"`
	Version     string     `gorm:"not null"`
	ContentHash string     `gorm:"not null"`
	// InstalledAsDependency is true when this package was installed only because a
	// bundle referenced it, not by the user directly. Uninstalling the bundle that
	// pulled it in removes it (once no other bundle still references it); a package
	// the user installed standalone is false and is never removed by a bundle.
	InstalledAsDependency bool `gorm:"not null;default:false"`
	// VariablesScoped is true when the install stored this package's values under
	// package scoped keys, because the bundle that installed it did not share
	// variables. Update reads the values the same way. False means the share mode
	// comes from the package manifest.
	VariablesScoped bool `gorm:"not null;default:false"`
	// OAuthPicks is the json list of the OAuth provider pick variables the operator
	// gave a value for. An update reuses a stored value as a pick only when its
	// name is listed here.
	OAuthPicks string `gorm:"type:text"`
}

func (InstalledPackage) TableName() string { return INSTALLED_PACKAGE_TABLE }

// InstalledPackageMember links a bundle's installed package to a referenced
// package's installed package. Uninstalling a bundle counts the remaining
// membership rows for a member to decide whether to remove it; no content is
// scanned. The pair is unique so a bundle links a member once.
type InstalledPackageMember struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index"`
	BundleID  *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_ipm_bundle_member"`
	MemberID  *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_ipm_bundle_member"`
}

func (InstalledPackageMember) TableName() string { return INSTALLED_PACKAGE_MEMBER_TABLE }

// InstalledResource links one real entity created by an install to its source
// package. InstalledContentHash is the fingerprint of the entity content as
// imported; an update compares the entity's current hash to it to detect a local
// edit. BoundVariables is the json map of the @@VAR@@ values the user supplied,
// reused on update so the user is not asked again.
type InstalledResource struct {
	ID                 *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt          *time.Time `gorm:"not null;index"`
	UpdatedAt          *time.Time `gorm:"not null;index"`
	InstalledPackageID *uuid.UUID `gorm:"not null;index;type:uuid"`
	ResourceType       string     `gorm:"not null;index"`
	ResourceID         *uuid.UUID `gorm:"not null;index;type:uuid"`
	// SourceName is the resource name as declared in the package at install time.
	// Updates match a package resource to its installed entity by this name, so a
	// locally renamed entity is still recognized and not duplicated.
	SourceName           string `gorm:"index"`
	InstalledContentHash string `gorm:"not null"`
	BoundVariables       string `gorm:"type:text"`
	// ReplacedContent holds the content an entity had before the install wrote the
	// package content over it. It is set only for a global report template, which
	// exists once per kind and is overwritten in place instead of created. Uninstall
	// and a failed install write this content back instead of deleting the entity.
	ReplacedContent *string `gorm:"type:text"`
}

func (InstalledResource) TableName() string { return INSTALLED_RESOURCE_TABLE }

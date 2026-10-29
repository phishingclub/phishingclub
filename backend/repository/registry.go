package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Registry is the repository for remote content registry connections and the
// provenance records of what was installed from them. It works directly over the
// database structs (there is no heavy model layer for these internal entities).
type Registry struct {
	DB *gorm.DB
}

func now() *time.Time {
	t := time.Now().UTC()
	return &t
}

// InsertRegistry stores a new registry connection and returns its id
func (r *Registry) InsertRegistry(ctx context.Context, row *database.Registry) (*uuid.UUID, error) {
	id := uuid.New()
	row.ID = &id
	row.CreatedAt = now()
	row.UpdatedAt = now()
	if err := r.DB.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return &id, nil
}

// GetAllRegistries returns every configured registry, newest first.
func (r *Registry) GetAllRegistries(ctx context.Context) ([]*database.Registry, error) {
	var rows []*database.Registry
	err := r.DB.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// GetRegistryByID returns one registry
func (r *Registry) GetRegistryByID(ctx context.Context, id *uuid.UUID) (*database.Registry, error) {
	var row database.Registry
	err := r.DB.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateRegistry stores a new name, url, token and insecure flag
func (r *Registry) UpdateRegistry(ctx context.Context, id *uuid.UUID, name, url, token string, insecure bool) error {
	return r.DB.WithContext(ctx).Model(&database.Registry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"name":         name,
			"url":          url,
			"token":        token,
			"insecure_tls": insecure,
			"updated_at":   now(),
		}).Error
}

// DeleteRegistry removes a registry connection
func (r *Registry) DeleteRegistry(ctx context.Context, id *uuid.UUID) error {
	return r.DB.WithContext(ctx).Where("id = ?", id).Delete(&database.Registry{}).Error
}

// InsertInstalledPackage stores an installed snapshot and returns its id
func (r *Registry) InsertInstalledPackage(ctx context.Context, row *database.InstalledPackage) (*uuid.UUID, error) {
	id := uuid.New()
	row.ID = &id
	row.CreatedAt = now()
	row.UpdatedAt = now()
	if err := r.DB.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return &id, nil
}

// GetAllInstalledPackages returns every installed package snapshot, newest first.
func (r *Registry) GetAllInstalledPackages(ctx context.Context) ([]*database.InstalledPackage, error) {
	var rows []*database.InstalledPackage
	err := r.DB.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// GetInstalledPackageByID returns one installed package snapshot
func (r *Registry) GetInstalledPackageByID(ctx context.Context, id *uuid.UUID) (*database.InstalledPackage, error) {
	var row database.InstalledPackage
	err := r.DB.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetInstalledPackage returns the snapshot for a given registry + package id, or
// gorm.ErrRecordNotFound when that package is not installed.
func (r *Registry) GetInstalledPackage(
	ctx context.Context, registryID *uuid.UUID, packageID string,
) (*database.InstalledPackage, error) {
	var row database.InstalledPackage
	err := r.DB.WithContext(ctx).
		Where("registry_id = ? AND package_id = ?", registryID, packageID).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateInstalledPackageVersion stores a new installed version and content hash
func (r *Registry) UpdateInstalledPackageVersion(
	ctx context.Context, id *uuid.UUID, version, contentHash string,
) error {
	return r.DB.WithContext(ctx).Model(&database.InstalledPackage{}).
		Where("id = ?", id).
		Updates(map[string]any{"version": version, "content_hash": contentHash, "updated_at": now()}).Error
}

// SetInstalledOAuthPicks stores the json list of the OAuth provider pick
// variables the operator gave a value for.
func (r *Registry) SetInstalledOAuthPicks(ctx context.Context, id *uuid.UUID, picks string) error {
	return r.DB.WithContext(ctx).Model(&database.InstalledPackage{}).
		Where("id = ?", id).
		Updates(map[string]any{"o_auth_picks": picks, "updated_at": now()}).Error
}

// SetInstalledAsDependency sets whether a package counts as installed only
// because a bundle referenced it.
func (r *Registry) SetInstalledAsDependency(ctx context.Context, id *uuid.UUID, dependency bool) error {
	return r.DB.WithContext(ctx).Model(&database.InstalledPackage{}).
		Where("id = ?", id).
		Updates(map[string]any{"installed_as_dependency": dependency, "updated_at": now()}).Error
}

// DeleteInstalledPackage removes an installed package snapshot
func (r *Registry) DeleteInstalledPackage(ctx context.Context, id *uuid.UUID) error {
	return r.DB.WithContext(ctx).Where("id = ?", id).Delete(&database.InstalledPackage{}).Error
}

// InsertPackageMember links a bundle install to one member install. It is
// idempotent on the (bundle, member) pair so linking twice is harmless.
func (r *Registry) InsertPackageMember(ctx context.Context, bundleID, memberID *uuid.UUID) error {
	id := uuid.New()
	row := &database.InstalledPackageMember{ID: &id, CreatedAt: now(), BundleID: bundleID, MemberID: memberID}
	return r.DB.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(row).Error
}

// GetPackageMembers returns the installed package ids of the members a bundle
// pulled in.
func (r *Registry) GetPackageMembers(ctx context.Context, bundleID *uuid.UUID) ([]*uuid.UUID, error) {
	var rows []database.InstalledPackageMember
	if err := r.DB.WithContext(ctx).Where("bundle_id = ?", bundleID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*uuid.UUID, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].MemberID)
	}
	return out, nil
}

// DeletePackageMembersByBundle removes every membership row for a bundle
func (r *Registry) DeletePackageMembersByBundle(ctx context.Context, bundleID *uuid.UUID) error {
	return r.DB.WithContext(ctx).Where("bundle_id = ?", bundleID).Delete(&database.InstalledPackageMember{}).Error
}

// DeletePackageMember removes the link between one bundle and one member
func (r *Registry) DeletePackageMember(ctx context.Context, bundleID, memberID *uuid.UUID) error {
	return r.DB.WithContext(ctx).
		Where("bundle_id = ? AND member_id = ?", bundleID, memberID).
		Delete(&database.InstalledPackageMember{}).Error
}

// CountBundlesForMember counts how many bundles still link a member, excluding an
// optional bundle being removed, so uninstall can decide whether the member is
// still needed. No content is read; this counts membership rows only.
func (r *Registry) CountBundlesForMember(ctx context.Context, memberID, excludeBundleID *uuid.UUID) (int64, error) {
	q := r.DB.WithContext(ctx).Model(&database.InstalledPackageMember{}).Where("member_id = ?", memberID)
	if excludeBundleID != nil {
		q = q.Where("bundle_id <> ?", excludeBundleID)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// InsertInstalledResource links a created entity to its installed package
func (r *Registry) InsertInstalledResource(ctx context.Context, row *database.InstalledResource) (*uuid.UUID, error) {
	id := uuid.New()
	row.ID = &id
	row.CreatedAt = now()
	row.UpdatedAt = now()
	if err := r.DB.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return &id, nil
}

// GetInstalledResourcesByPackage returns the resources an install created, in
// the order they were recorded.
func (r *Registry) GetInstalledResourcesByPackage(
	ctx context.Context, installedPackageID *uuid.UUID,
) ([]*database.InstalledResource, error) {
	var rows []*database.InstalledResource
	err := r.DB.WithContext(ctx).
		Where("installed_package_id = ?", installedPackageID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// GetAllInstalledResources returns every provenance row across all installed
// packages, so a caller can tell which entities came from a package.
func (r *Registry) GetAllInstalledResources(ctx context.Context) ([]*database.InstalledResource, error) {
	var rows []*database.InstalledResource
	err := r.DB.WithContext(ctx).Find(&rows).Error
	return rows, err
}

// CountInstalledResourcesByResource counts provenance rows pointing at the same
// resource entity, excluding one installed package. Used on uninstall to keep a
// shared asset that another installed package still references.
func (r *Registry) CountInstalledResourcesByResource(
	ctx context.Context, resourceID, excludePackageID *uuid.UUID,
) (int64, error) {
	var n int64
	err := r.DB.WithContext(ctx).Model(&database.InstalledResource{}).
		Where("resource_id = ? AND installed_package_id <> ?", resourceID, excludePackageID).
		Count(&n).Error
	return n, err
}

// GetInstalledResourcesByResource returns every provenance row pointing at one
// entity, across all installed packages.
func (r *Registry) GetInstalledResourcesByResource(
	ctx context.Context, resourceType string, resourceID *uuid.UUID,
) ([]*database.InstalledResource, error) {
	var rows []*database.InstalledResource
	err := r.DB.WithContext(ctx).
		Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).
		Find(&rows).Error
	return rows, err
}

// UpdateInstalledResource records a new content hash and bound variables for a
// resource (used after an update applies).
func (r *Registry) UpdateInstalledResource(ctx context.Context, id *uuid.UUID, contentHash, boundVars string) error {
	return r.DB.WithContext(ctx).Model(&database.InstalledResource{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"installed_content_hash": contentHash,
			"bound_variables":        boundVars,
			"updated_at":             now(),
		}).Error
}

// DeleteInstalledResource removes one provenance row
func (r *Registry) DeleteInstalledResource(ctx context.Context, id *uuid.UUID) error {
	return r.DB.WithContext(ctx).Where("id = ?", id).Delete(&database.InstalledResource{}).Error
}

// DeleteInstalledResourcesByPackage removes all provenance rows for an install
func (r *Registry) DeleteInstalledResourcesByPackage(ctx context.Context, installedPackageID *uuid.UUID) error {
	return r.DB.WithContext(ctx).
		Where("installed_package_id = ?", installedPackageID).
		Delete(&database.InstalledResource{}).Error
}

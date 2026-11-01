package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// pickAllowDeny is the pick kind of a variable the operator fills by choosing
// one of their own global allow/deny lists. An asset names the variable as a
// whole @@NAME@@ entry in its allowDeny list, and nothing else, so the asset has
// exactly that one list and its kind is the operator's choice.
const pickAllowDeny = "allowDeny"

// aListKind names the kind of an allow deny list, with its article.
func aListKind(allowed bool) string {
	if allowed {
		return "an allow list"
	}
	return "a block list"
}

// reusedList is an installed list package an install or update reuses instead
// of installing it, with the version fetched for it now and the package that
// needs it.
type reusedList struct {
	installed *database.InstalledPackage
	pkg       *registry.VerifiedPackage
	neededBy  string
	// outdated is set when the installed version is older than pkg.
	outdated error
}

// checkReusedDep checks an installed package that the package neededBy depends
// on and that is reused at its installed version. Only a list package is
// checked: it returns the reused list package with the version fetched for it,
// or nil for any other type. One installed at a version older than the one
// fetched may lack lists, or have lists of another kind, than the dependent
// expects. That is not refused here but recorded as outdated, and an asset
// that uses one of its lists is refused (see listRef), so a list package no
// written asset uses never blocks an install or update.
func (s *ContentRegistry) checkReusedDep(
	ctx context.Context,
	ex *database.InstalledPackage,
	neededBy string,
	fetch func(id string) (*registry.VerifiedPackage, error),
) (*reusedList, error) {
	if ex.PackageType != "allowdeny" {
		return nil, nil
	}
	dp, err := fetch(ex.PackageID)
	if errors.Is(err, errNotDownloaded) || (err == nil && manifestOnly(dp)) {
		return nil, errs.NewValidationError(errNotDownloaded)
	}
	if err != nil {
		return nil, registryError("could not fetch referenced package "+ex.PackageID, err)
	}
	rl := &reusedList{installed: ex, pkg: dp, neededBy: neededBy}
	if isDowngrade(dp.Manifest.Version, ex.Version) {
		rl.outdated = errs.NewValidationError(fmt.Errorf(
			"%s needs %s version %s, but version %s is installed. Update %s first, then try again",
			neededBy, ex.PackageID, dp.Manifest.Version, ex.Version, ex.PackageID,
		))
	}
	return rl, nil
}

// listRef is a list name an asset takes from a list package its package
// depends on: that package, the kind it declares the list as, and whether the
// install or update installs the package, so the list does not exist yet.
// outdated is set for an installed package older than the version fetched for
// it, and refuses every asset that uses one of its lists.
type listRef struct {
	pkgID    string
	allowed  bool
	planned  bool
	outdated error
}

// listScope is where the list names of one package's assets resolve. A name a
// list package it depends on declares resolves through the installed resource
// that package tracks for it, never by the list's name now, so a list renamed
// in Phishing Club still resolves and another list that took its name never
// does. Any other name, such as the list an installer picked, is a global list
// found by name. A nil scope finds every name by name.
type listScope struct {
	registryID *uuid.UUID
	owner      string
	refs       map[string]listRef
}

// ref returns the dependency a name comes from.
func (sc *listScope) ref(name string) (listRef, bool) {
	if sc == nil {
		return listRef{}, false
	}
	r, ok := sc.refs[name]
	return r, ok
}

// listSource is a list package an install or update uses: the version read for
// it with the values its list names are filled with, and whether the operation
// installs it.
type listSource struct {
	target   assetTarget
	planned  bool
	outdated error
}

// listSourcesFor maps the package id of every package an install or update
// takes lists from to its source: the ones it installs or updates, filled with
// the values given now, and the installed list packages it reuses, filled with
// the values they were installed with. The lists of a package it installs or
// updates are created or written before any asset is linked to them.
func (s *ContentRegistry) listSourcesFor(
	ctx context.Context, installs []*registry.VerifiedPackage, vars map[string]string, share bool, reused []reusedList,
) map[string]listSource {
	out := map[string]listSource{}
	for _, p := range installs {
		out[p.Manifest.ID] = listSource{target: assetTarget{pkg: p, vars: effectiveVarsFor(p, vars, share)}, planned: true}
	}
	for _, rl := range reused {
		bound := boundNamesFor(rl.installed.PackageID, rl.pkg, s.mergeBoundVars(ctx, rl.installed.ID, nil))
		out[rl.installed.PackageID] = listSource{target: assetTarget{pkg: rl.pkg, vars: bound}, outdated: rl.outdated}
	}
	return out
}

// listScopes builds the list scope of each target, by package id, from the
// packages in sources it depends on and its own lists. Two of them declaring
// the same name are refused, since the name would not tell which list is
// meant.
func (s *ContentRegistry) listScopes(
	registryID *uuid.UUID, targets []assetTarget, sources map[string]listSource,
) (map[string]*listScope, error) {
	out := map[string]*listScope{}
	declared := map[string]map[string]bool{}
	for _, t := range targets {
		id := t.pkg.Manifest.ID
		sc := &listScope{registryID: registryID, owner: id, refs: map[string]listRef{}}
		from := []string{id}
		for _, d := range t.pkg.Manifest.Dependencies {
			from = append(from, d.ID)
		}
		for _, pid := range from {
			src, ok := sources[pid]
			if !ok {
				continue
			}
			names, ok := declared[pid]
			if !ok {
				var err error
				if names, err = declaredAllowDenies([]assetTarget{src.target}); err != nil {
					return nil, s.contentDocsErr(err)
				}
				declared[pid] = names
			}
			for n, allowed := range names {
				if prev, dup := sc.refs[n]; dup && prev.pkgID != pid {
					return nil, errs.NewValidationError(fmt.Errorf(
						"%s takes the list %q from both %s and %s", id, n, prev.pkgID, pid,
					))
				}
				sc.refs[n] = listRef{pkgID: pid, allowed: allowed, planned: src.planned, outdated: src.outdated}
			}
		}
		out[id] = sc
	}
	return out, nil
}

// scopedList finds the list the list package of ref installed for name,
// through the installed resource it tracks for it, and checks that the list is
// still there, global and of the kind the package declares.
func (s *ContentRegistry) scopedList(ctx context.Context, sc *listScope, ref listRef, name string) (*model.AllowDeny, error) {
	if ref.outdated != nil {
		return nil, ref.outdated
	}
	gone := errs.NewValidationError(fmt.Errorf(
		"%s uses the list %q from %s, but that list is no longer installed. "+
			"Reinstall %s to restore it, then try again",
		sc.owner, name, ref.pkgID, ref.pkgID,
	))
	ip, err := s.RegistryRepository.GetInstalledPackage(ctx, sc.registryID, ref.pkgID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gone
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	rows, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, ip.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	var listID *uuid.UUID
	for _, r := range rows {
		if r.ResourceType == "allowdeny" && r.ResourceID != nil && strings.TrimSpace(r.SourceName) == name {
			listID = r.ResourceID
			break
		}
	}
	if listID == nil || s.AllowDeny == nil || s.AllowDeny.AllowDenyRepository == nil {
		return nil, gone
	}
	l, err := s.AllowDeny.AllowDenyRepository.GetByID(ctx, listID, &repository.AllowDenyOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gone
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if !model.AllowDenyUsableInCompany(l, nil) {
		return nil, gone
	}
	if allowed, _ := l.Allowed.Get(); allowed != ref.allowed {
		return nil, errs.NewValidationError(fmt.Errorf(
			"%s uses the list %q from %s as %s, but the installed list is %s. "+
				"Update %s or fix the list, then try again",
			sc.owner, name, ref.pkgID, aListKind(ref.allowed), aListKind(allowed), ref.pkgID,
		))
	}
	return l, nil
}

// memberDeps is what the packages a bundle install or update installs need of
// their own: the packages they depend on that are not installed yet, the links
// from each to what it depends on, and the reused list packages to check.
type memberDeps struct {
	install []*registry.VerifiedPackage
	links   []memberLink
	lists   []reusedList
}

// planMemberDeps resolves the dependencies of members, the packages a bundle
// installs now. A dependency the plan already installs or reuses (reused,
// which this adds to) is linked, one installed already is reused and linked,
// and the rest are fetched to install. Bait Box only lets a package that is not
// a bundle depend on list packages, which have no dependencies themselves, so
// one level is complete; a deeper one is refused.
func (s *ContentRegistry) planMemberDeps(
	ctx context.Context,
	registryID *uuid.UUID,
	members []*registry.VerifiedPackage,
	reused map[string]*uuid.UUID,
	fetch func(id string) (*registry.VerifiedPackage, error),
) (*memberDeps, error) {
	out := &memberDeps{}
	planned := map[string]bool{}
	for _, m := range members {
		planned[m.Manifest.ID] = true
	}
	for _, m := range members {
		if m.Manifest.Type == "bundle" {
			continue
		}
		for _, d := range m.Manifest.Dependencies {
			out.links = append(out.links, memberLink{parent: m.Manifest.ID, child: d.ID})
			if planned[d.ID] {
				continue
			}
			if _, ok := reused[d.ID]; ok {
				continue
			}
			ex, err := s.RegistryRepository.GetInstalledPackage(ctx, registryID, d.ID)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errs.Wrap(err)
			}
			if err == nil && ex != nil {
				rl, rerr := s.checkReusedDep(ctx, ex, m.Manifest.ID, fetch)
				if rerr != nil {
					return nil, rerr
				}
				if rl != nil {
					out.lists = append(out.lists, *rl)
				}
				reused[d.ID] = ex.ID
				continue
			}
			dp, ferr := fetch(d.ID)
			if errors.Is(ferr, errNotDownloaded) || (ferr == nil && manifestOnly(dp)) {
				return nil, errs.NewValidationError(errNotDownloaded)
			}
			if ferr != nil {
				return nil, registryError("could not fetch referenced package "+d.ID, ferr)
			}
			if len(dp.Manifest.Dependencies) > 0 {
				return nil, errs.NewValidationError(fmt.Errorf(
					"%s depends on %s, which depends on other packages. "+
						"Only the direct dependencies of a bundle member are installed",
					m.Manifest.ID, d.ID,
				))
			}
			planned[d.ID] = true
			out.install = append(out.install, dp)
			if len(out.install) > maxBundleDependencies {
				return nil, errs.NewValidationError(fmt.Errorf("package references too many packages"))
			}
		}
	}
	return out, nil
}

// prefetchMemberDeps downloads what planMemberDeps reads for a member the
// update installs, before the lock is taken: each dependency not installed
// yet, and each installed list package it reuses. A failure is kept in the
// cache and reported when the plan reads it.
func (s *ContentRegistry) prefetchMemberDeps(
	ctx context.Context, registryID *uuid.UUID, member *registry.VerifiedPackage, cache *packageCache,
) {
	if member == nil || member.Manifest == nil || member.Manifest.Type == "bundle" {
		return
	}
	for _, d := range member.Manifest.Dependencies {
		ex, err := s.RegistryRepository.GetInstalledPackage(ctx, registryID, d.ID)
		if err == nil && ex != nil && ex.PackageType != "allowdeny" {
			continue
		}
		_, _ = cache.get(d.ID)
	}
}

// checkListPicks checks every list pick variable of pkgs: it must be required,
// and the list chosen must be an existing global list, found by its name now.
// Any kind is accepted, since an asset with a pick has no other list.
func (s *ContentRegistry) checkListPicks(
	ctx context.Context,
	pkgs []*registry.VerifiedPackage,
	vars map[string]string,
	share bool,
) error {
	for _, p := range pkgs {
		for _, v := range p.Manifest.Variables {
			if v.Pick != pickAllowDeny {
				continue
			}
			if !v.Required {
				return errs.NewValidationError(fmt.Errorf(
					"%s: the variable %q picks a list and must be required", p.Manifest.ID, v.Name,
				))
			}
			name := strings.TrimSpace(vars[varKey(p.Manifest.ID, v.Name, share)])
			if name == "" {
				return errs.NewValidationError(fmt.Errorf(
					"choose an allow/deny list for %q", v.Name,
				))
			}
			l, err := s.globalAllowDeny(ctx, name)
			if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && !model.AllowDenyUsableInCompany(l, nil)) {
				return errs.NewValidationError(fmt.Errorf(
					"the list %q chosen for %q is not a global allow/deny list", name, v.Name,
				))
			}
			if err != nil {
				return errs.Wrap(err)
			}
		}
	}
	return nil
}

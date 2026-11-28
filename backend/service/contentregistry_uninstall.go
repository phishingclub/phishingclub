package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// releaseResourceEntity gives back an entity an install put in place: content
// the install overwrote (replaced is set) is written back, anything the install
// created is deleted.
func (s *ContentRegistry) releaseResourceEntity(
	ctx context.Context, session *model.Session, resourceType string, id *uuid.UUID, replaced *string,
) error {
	if resourceType == "reporttemplate" && replaced != nil {
		return s.restoreReportTemplate(ctx, session, id, *replaced)
	}
	return s.deleteResourceEntity(ctx, session, resourceType, id)
}

// deleteResourceEntity removes one installed entity by its provenance type. Used
// by install rollback, Remove and update undo so every type is handled in one
// place. An entity that no longer exists counts as removed. An asset or
// attachment whose file is already gone has its row removed directly. An
// allow/deny list an asset still uses is kept and counts as removed, since
// deleting it would make that asset reachable by everyone; it stays as the
// user's own list.
func (s *ContentRegistry) deleteResourceEntity(
	ctx context.Context, session *model.Session, resourceType string, id *uuid.UUID,
) error {
	var err error
	switch resourceType {
	case "allowdeny":
		used, uerr := s.listUsedByAssets(ctx, id)
		if uerr != nil {
			return uerr
		}
		if used {
			s.Logger.Infow("allow/deny list kept, assets still use it", "id", id.String())
			return nil
		}
		err = allowDenySpec.deleteByID(s, ctx, session, id)
	case "asset":
		err = s.Asset.DeleteByID(ctx, session, id)
		if errors.Is(err, os.ErrNotExist) {
			err = s.Asset.AssetRepository.DeleteByID(ctx, id)
		}
	case "attachment":
		err = s.Attachment.DeleteByID(ctx, session, id)
		if errors.Is(err, os.ErrNotExist) {
			if err = s.Attachment.EmailRepository.RemoveAttachmentsByAttachmentID(ctx, id); err == nil {
				err = s.Attachment.AttachmentRepository.DeleteByID(ctx, id)
			}
		}
	default:
		// identifiers have no spec: they are global, shared and reusable, so they
		// stay in place.
		if sp, ok := resourceSpecs[resourceType]; ok {
			err = sp.deleteByID(s, ctx, session, id)
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// removeInstalledResource deletes the entity behind one provenance row of the
// install ipID. Identifiers are global shared values and stay. An asset another
// installed package still references stays too, and so does an allow/deny list
// an asset still uses, since deleting it would make that asset reachable by
// everyone. A global report template the install overwrote gets its previous
// content back. It returns the error so the caller can keep the provenance row
// of anything that was not removed. An entity kept here has its provenance row
// dropped by the caller, so it becomes the user's own.
func (s *ContentRegistry) removeInstalledResource(
	ctx context.Context, session *model.Session, ipID *uuid.UUID, r *database.InstalledResource,
) error {
	if r.ResourceID == nil || r.ResourceType == "identifier" {
		return nil
	}
	switch r.ResourceType {
	case "asset":
		shared, err := s.assetOwnedByOtherInstall(ctx, r.ResourceID, ipID)
		if err != nil {
			return err
		}
		if shared {
			return nil
		}
	}
	// an allow/deny list an asset still uses is kept, see deleteResourceEntity.
	return s.releaseResourceEntity(ctx, session, r.ResourceType, r.ResourceID, r.ReplacedContent)
}

// listUsedByAssets reports whether any asset uses the allow/deny list. Removal
// deletes the assets of the package before its lists, so an asset found here
// is one the removal keeps.
func (s *ContentRegistry) listUsedByAssets(ctx context.Context, listID *uuid.UUID) (bool, error) {
	if s.Asset == nil || s.Asset.AssetRepository == nil {
		return false, nil
	}
	assets, err := s.Asset.AssetRepository.GetByAllowDenyID(ctx, listID)
	if err != nil {
		return false, err
	}
	return len(assets) > 0, nil
}

// removeRank is the order a package's resources are deleted in, highest first.
// A resource is deleted before the one it points at: a campaign template before
// what it binds, an api sender before its oauth provider, a nested attachment
// before its email, an asset before the allow/deny lists it uses, so a failed
// asset delete never leaves the asset without its lists. It matches the order
// update removes dropped resources in.
func removeRank(resourceType string) int {
	switch resourceType {
	case "campaigntemplate":
		return 4
	case "apisender":
		return 3
	case "attachment":
		return 2
	case "oauthprovider", "allowdeny":
		return 0
	default:
		return 1
	}
}

// removalOrder returns the resources in the order to delete them: by
// removeRank, and within one rank the last recorded first.
func removalOrder(resources []*database.InstalledResource) []*database.InstalledResource {
	out := slices.Clone(resources)
	slices.Reverse(out)
	sort.SliceStable(out, func(i, j int) bool {
		return removeRank(out[i].ResourceType) > removeRank(out[j].ResourceType)
	})
	return out
}

// rollbackInstall undoes a partial install: it deletes every entity the install
// created and keeps an asset another install still references. The provenance
// row of an entity that fails to delete is kept along with the package row, so
// the leftover shows as installed and the user can remove it later. keepPackage
// keeps the package row even when everything was removed. It returns false when
// any entity was left behind. It runs to the end even when ctx is already
// cancelled, bounded by rollbackTimeout, so an ended request never leaves a
// partial install.
func (s *ContentRegistry) rollbackInstall(
	ctx context.Context, session *model.Session, ipID *uuid.UUID, keepPackage bool,
) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, ipID)
	if err != nil {
		s.Logger.Errorw("install rollback: list resources", "installedPackageID", ipID.String(), "error", err)
		return false
	}
	clean := true
	for _, r := range removalOrder(resources) {
		if derr := s.removeInstalledResource(ctx, session, ipID, r); derr != nil {
			s.Logger.Errorw("install rollback: delete resource",
				"type", r.ResourceType, "name", r.SourceName, "error", derr)
			clean = false
			continue
		}
		if derr := s.RegistryRepository.DeleteInstalledResource(ctx, r.ID); derr != nil {
			clean = false
		}
	}
	if !clean || keepPackage {
		return clean
	}
	if derr := s.RegistryRepository.DeleteInstalledPackage(ctx, ipID); derr != nil {
		return false
	}
	return true
}

// Remove deletes the entities an install created and its provenance. Entities are
// removed through the entity services, so their campaign and template relations
// are cleaned up the same way a normal delete does.
func (s *ContentRegistry) Remove(
	ctx context.Context, session *model.Session, installedPackageID *uuid.UUID,
) (err error) {
	// check access before waiting on the lock, so a caller without access never
	// waits behind another install, update or uninstall.
	ae, err := s.authorize(session, "Registry.Remove")
	if err != nil {
		return err
	}
	unlock, err := s.lockOps(ctx)
	if err != nil {
		ae.Details["installedPackageId"] = installedPackageID.String()
		s.auditFailure(ae, &err)
		return err
	}
	defer unlock()
	// a package another installed package links is still needed by it, for
	// example a list package whose lists a page package uses. Removing it would
	// take filters off that package's files, so the user removes or updates the
	// package that uses it first.
	if users, uerr := s.packageUsers(ctx, installedPackageID); uerr != nil || len(users) > 0 {
		ae.Details["installedPackageId"] = installedPackageID.String()
		if uerr != nil {
			err = errs.Wrap(uerr)
		} else {
			err = packageInUseErr(users)
		}
		s.auditFailure(ae, &err)
		return err
	}
	// a campaign template a step of an open program uses blocks the uninstall
	// before anything is deleted, so a bundle is never left half removed
	if perr := s.checkRemovalNotUsedByOpenProgram(ctx, installedPackageID); perr != nil {
		ae.Details["installedPackageId"] = installedPackageID.String()
		err = perr
		s.auditFailure(ae, &err)
		return err
	}
	// once deleting starts it runs to the end even if the request ends, so an
	// uninstall is never cut off halfway.
	return s.remove(context.WithoutCancel(ctx), session, installedPackageID)
}

// checkRemovalNotUsedByOpenProgram refuses an uninstall that would delete a
// campaign template a step of an open program uses. It walks the package and
// the referenced packages removed with it. A closed program does not block.
func (s *ContentRegistry) checkRemovalNotUsedByOpenProgram(ctx context.Context, installedPackageID *uuid.UUID) error {
	repo := s.programRepository()
	if repo == nil {
		return nil
	}
	members, err := s.removalSet(ctx, []*uuid.UUID{installedPackageID}, nil, nil, nil)
	if err != nil {
		return err
	}
	packageIDs := []*uuid.UUID{installedPackageID}
	for _, m := range members {
		packageIDs = append(packageIDs, m.ID)
	}
	for _, pid := range packageIDs {
		resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, pid)
		if err != nil {
			return errs.Wrap(err)
		}
		for _, r := range resources {
			if r.ResourceType != "campaigntemplate" || r.ResourceID == nil {
				continue
			}
			uses, err := repo.GetOpenUsingTemplates(ctx, []*uuid.UUID{r.ResourceID})
			if err != nil {
				return errs.Wrap(err)
			}
			if len(uses) == 0 {
				continue
			}
			quoted := make([]string, len(uses))
			for i, u := range uses {
				quoted[i] = fmt.Sprintf("%q", u.Name)
			}
			label := "program"
			if len(uses) > 1 {
				label = "programs"
			}
			return errs.NewValidationError(fmt.Errorf(
				"cannot uninstall: campaign template %q is used by %s %s. "+
					"Remove the template from the program or close the program first, then uninstall again",
				s.campaignTemplateName(ctx, *r.ResourceID), label, strings.Join(quoted, ", "),
			))
		}
	}
	return nil
}

// packageUsers returns the package ids of the installed packages that link the
// installed package id as a member, sorted.
func (s *ContentRegistry) packageUsers(ctx context.Context, id *uuid.UUID) ([]string, error) {
	parents, err := s.RegistryRepository.GetPackageParents(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range parents {
		pp, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, p)
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			// a link left by a package that is gone; it uses nothing.
			continue
		}
		if gerr != nil {
			return nil, gerr
		}
		if !slices.Contains(out, pp.PackageID) {
			out = append(out, pp.PackageID)
		}
	}
	sort.Strings(out)
	return out, nil
}

// packageInUseErr refuses removing a package other installed packages use.
func packageInUseErr(users []string) error {
	verb := "uses"
	if len(users) > 1 {
		verb = "use"
	}
	return errs.NewValidationError(fmt.Errorf(
		"%s %s this package; uninstall or update %s first",
		strings.Join(users, ", "), verb, pronounFor(len(users)),
	))
}

// pronounFor is "it" for one and "them" for more.
func pronounFor(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// remove is Remove without the lock. The caller holds the lock. It calls itself for
// each referenced package a bundle removes.
func (s *ContentRegistry) remove(
	ctx context.Context, session *model.Session, installedPackageID *uuid.UUID,
) (err error) {
	ae, err := s.authorize(session, "Registry.Remove")
	if err != nil {
		return err
	}
	defer s.auditFailure(ae, &err)
	ae.Details["installedPackageId"] = installedPackageID.String()
	// read the installed row now, while it still exists, so the audit names the
	// package and the Bait Box it came from.
	if ip, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, installedPackageID); gerr == nil {
		ae.Details["packageId"] = ip.PackageID
		ae.Details["registryId"] = ip.RegistryID.String()
	}
	resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, installedPackageID)
	if err != nil {
		return errs.Wrap(err)
	}
	// a reporting inbox signing in with an OAuth provider of this package, or of a
	// referenced package removed with it, blocks the removal before anything is
	// deleted, so a bundle is never left half removed
	if err := s.checkRemovalNotUsedByReportInbox(ctx, installedPackageID); err != nil {
		return err
	}
	// the API senders this package itself installed are deleted in this same call,
	// so they must not count as external users that block removing the package's own
	// OAuth provider (a self contained package shipping both would never uninstall).
	var ownSenderIDs []uuid.UUID
	for _, r := range resources {
		if r.ResourceType == "apisender" && r.ResourceID != nil {
			ownSenderIDs = append(ownSenderIDs, *r.ResourceID)
		}
	}
	// an OAuth provider cannot be deleted while an API sender still points at it (a
	// foreign key), so refuse the uninstall with a clear message before deleting
	// anything, rather than failing the delete silently and orphaning the provider.
	for _, r := range resources {
		if r.ResourceType != "oauthprovider" || r.ResourceID == nil {
			continue
		}
		names, cerr := s.APISenderRepository.NamesByOAuthProviderIDExcluding(ctx, *r.ResourceID, ownSenderIDs)
		if cerr != nil {
			return errs.Wrap(cerr)
		}
		if len(names) == 0 {
			continue
		}
		label, tail := "API sender", "it"
		if len(names) > 1 {
			label, tail = "API senders", "them"
		}
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = fmt.Sprintf("%q", n)
		}
		return errs.NewValidationError(fmt.Errorf(
			"cannot uninstall: this OAuth provider is still used by %s %s. "+
				"Remove or repoint %s first, then uninstall again",
			label, strings.Join(quoted, ", "), tail,
		))
	}
	// delete each entity, then its provenance row. identifiers and an asset another
	// installed package still references stay (removeInstalledResource). an entity
	// that fails to delete keeps its row, so the package stays installed and a later
	// uninstall retries exactly what is left. removalOrder deletes a resource
	// before the one it points at, so a foreign key never blocks a delete.
	var failed []string
	for _, r := range removalOrder(resources) {
		if derr := s.removeInstalledResource(ctx, session, installedPackageID, r); derr != nil {
			s.Logger.Errorw("uninstall: delete resource", "type", r.ResourceType, "name", r.SourceName, "error", derr)
			failed = append(failed, fmt.Sprintf("%s %q: %v", r.ResourceType, r.SourceName, derr))
			continue
		}
		if derr := s.RegistryRepository.DeleteInstalledResource(ctx, r.ID); derr != nil {
			failed = append(failed, fmt.Sprintf("%s %q: %v", r.ResourceType, r.SourceName, derr))
		}
	}
	if len(failed) > 0 {
		return removeIncompleteErr(failed)
	}
	// a bundle also pulled in the packages it references. remove each member that no
	// other bundle still links and that the user did not install standalone, then drop
	// its membership row. a member that fails to remove keeps its row and the bundle
	// stays installed, so a later uninstall retries it. members are counted by row,
	// never content scanned.
	members, err := s.RegistryRepository.GetPackageMembers(ctx, installedPackageID)
	if err != nil {
		return errs.Wrap(err)
	}
	// remove members in reverse install order: a package that references another by
	// name (an api sender naming an oauth provider) is removed before the one it
	// references, so the referenced entity is not held by a foreign key when deleted.
	sort.SliceStable(members, func(i, j int) bool {
		return memberRemovePriority(ctx, s, members[i]) < memberRemovePriority(ctx, s, members[j])
	})
	for _, m := range members {
		if m == nil {
			continue
		}
		label := m.String()
		n, cerr := s.RegistryRepository.CountBundlesForMember(ctx, m, installedPackageID)
		if cerr != nil {
			failed = append(failed, fmt.Sprintf("referenced package %s: %v", label, cerr))
			continue
		}
		removeMember := n == 0
		if removeMember {
			mp, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
			switch {
			case errors.Is(gerr, gorm.ErrRecordNotFound):
				removeMember = false
			case gerr != nil:
				failed = append(failed, fmt.Sprintf("referenced package %s: %v", label, gerr))
				continue
			default:
				label = mp.PackageID
				removeMember = mp.InstalledAsDependency
			}
		}
		if removeMember {
			if rerr := s.remove(ctx, session, m); rerr != nil {
				failed = append(failed, fmt.Sprintf("referenced package %s: %v", label, rerr))
				continue
			}
		}
		if lerr := s.RegistryRepository.DeletePackageMember(ctx, installedPackageID, m); lerr != nil {
			failed = append(failed, fmt.Sprintf("referenced package %s: %v", label, lerr))
		}
	}
	if len(failed) > 0 {
		return removeIncompleteErr(failed)
	}
	if err := s.RegistryRepository.DeleteInstalledPackage(ctx, installedPackageID); err != nil {
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// removeIncompleteErr reports the items an uninstall could not remove. They stay
// installed so the uninstall can be retried.
func removeIncompleteErr(failed []string) error {
	return errs.NewValidationError(fmt.Errorf(
		"uninstall incomplete, these items could not be removed and stay installed: %s",
		strings.Join(failed, "; "),
	))
}

// uninstallPreviewCampaignCap caps how many campaigns each preview group names.
// The count fields always hold the full number.
const uninstallPreviewCampaignCap = 10

// uninstallPreviewFetchTimeout bounds the one registry download a preview may do,
// reading the smtp headers the installed version declared.
const uninstallPreviewFetchTimeout = 15 * time.Second

// UninstallPreviewCampaign is an active campaign an uninstall would affect.
// CloseAt is set for a campaign that already has a close time.
type UninstallPreviewCampaign struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CloseAt string `json:"closeAt,omitempty"`
}

// UninstallPreviewChanged is an active campaign that keeps running but loses
// something the uninstall deletes. Losses names each lost item as plain text.
// OpenToEveryone is set when the campaign loses its last allow list, so it
// starts serving every visitor.
type UninstallPreviewChanged struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Losses         []string `json:"losses"`
	OpenToEveryone bool     `json:"openToEveryone"`
}

// UninstallPreviewAsset is an asset that stays but loses allow or block lists
// the uninstall deletes. Losses names each lost list as plain text.
// OpenToEveryone is set when the asset loses its last allow list, so every
// visitor can get it. Unfiltered is set when no list is left on it at all.
type UninstallPreviewAsset struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	DomainName     string   `json:"domainName,omitempty"`
	Losses         []string `json:"losses"`
	OpenToEveryone bool     `json:"openToEveryone"`
	Unfiltered     bool     `json:"unfiltered"`
}

// UninstallPreviewKeptList is an allow or block list the uninstall keeps because
// assets it does not delete still use it. Deleting it would make those assets
// reachable by everyone, so it stays as the user's own list.
type UninstallPreviewKeptList struct {
	Name    string   `json:"name"`
	Allowed bool     `json:"allowed"`
	Assets  []string `json:"assets"`
}

// UninstallPreviewItem is an installed entity the user edited after install.
// Uninstall deletes it, so those edits are lost. Name is the entity's current
// name, SourceName the name the package gave it.
type UninstallPreviewItem struct {
	ResourceType string `json:"resourceType"`
	Name         string `json:"name"`
	SourceName   string `json:"sourceName"`
	PackageID    string `json:"packageId"`
}

// UninstallPreview is what uninstalling one installed package would affect.
//
// Campaigns are the active campaigns the uninstall closes, also the ones that
// already have a later close time.
// Changed are active campaigns that lose a filter, deny page, webhook or script.
// UserAssetCount counts the assets on domains the uninstall deletes that the
// package did not install, which are deleted with the domain.
// ChangedAssets are assets that stay but lose an allow or block list.
// Members lists the referenced packages a bundle uninstall also removes, which
// leaves out any still used by another bundle or installed by the user directly.
// KeptLists are lists of the removed packages that stay because assets still
// use them. Blocked are entities the uninstall can not delete, they stay
// installed and the uninstall ends incomplete. UsedBy names the installed
// packages that use this one; while any does, the uninstall is refused.
type UninstallPreview struct {
	Campaigns         []UninstallPreviewCampaign `json:"campaigns"`
	CampaignCount     int                        `json:"campaignCount"`
	Changed           []UninstallPreviewChanged  `json:"changed"`
	ChangedCount      int                        `json:"changedCount"`
	UserAssetCount    int                        `json:"userAssetCount"`
	ChangedAssets     []UninstallPreviewAsset    `json:"changedAssets"`
	ChangedAssetCount int                        `json:"changedAssetCount"`
	Edited            []UninstallPreviewItem     `json:"edited"`
	Members           []string                   `json:"members"`
	KeptLists         []UninstallPreviewKeptList `json:"keptLists"`
	Blocked           []UninstallPreviewBlocked  `json:"blocked"`
	UsedBy            []string                   `json:"usedBy"`
	// Programs are the open programs the uninstall affects. A program with a
	// step whose campaign template the uninstall deletes blocks the uninstall.
	Programs []UninstallPreviewProgram `json:"programs"`
	// ProgramCount is the number of affected open programs, Programs holds up
	// to uninstallPreviewCampaignCap of them. ProgramsBlock is set when any of
	// them blocks the uninstall, listed or not.
	ProgramCount  int  `json:"programCount"`
	ProgramsBlock bool `json:"programsBlock"`
}

// UninstallPreviewBlocked is an installed entity the uninstall can not delete,
// a page that an open campaign or open program uses as deny or evasion page.
// It stays installed with its package, and nothing that uses it loses it.
// UsedBy names some of the campaigns and programs as "campaign X" or
// "program X", UsedByCount counts all of them.
type UninstallPreviewBlocked struct {
	ResourceType string   `json:"resourceType"`
	Name         string   `json:"name"`
	PackageID    string   `json:"packageId"`
	UsedBy       []string `json:"usedBy"`
	UsedByCount  int      `json:"usedByCount"`
}

// UninstallPreviewProgram is an open program an uninstall would affect.
// Blocks is set when the uninstall deletes a campaign template one of its
// steps uses, the uninstall is refused until the step is removed or the
// program is closed.
type UninstallPreviewProgram struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Losses []string `json:"losses"`
	Blocks bool     `json:"blocks"`
}

// campaignLoss is what one campaign loses from the deletes of an uninstall.
type campaignLoss struct {
	campaign *model.Campaign
	losses   []string
	// removedAllow holds the allow lists the uninstall deletes from it
	removedAllow map[uuid.UUID]bool
	// removedFilter holds every allow or block list the uninstall deletes from it
	removedFilter map[uuid.UUID]bool
	// denyPageRemoved is set when its deny page is deleted
	denyPageRemoved bool
}

// UninstallPreview reports, without changing anything, which active campaigns
// uninstalling the package would close or change and which of its entities were
// edited locally. It walks the same packages Remove would: the package itself
// and each referenced package no other bundle links and the user did not install
// directly.
func (s *ContentRegistry) UninstallPreview(
	ctx context.Context, session *model.Session, installedPackageID *uuid.UUID,
) (*UninstallPreview, error) {
	if _, err := s.authorize(session, "Registry.UninstallPreview"); err != nil {
		return nil, err
	}
	root, err := s.RegistryRepository.GetInstalledPackageByID(ctx, installedPackageID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	members, err := s.removalSet(ctx, []*uuid.UUID{root.ID}, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	pkgs := append([]*database.InstalledPackage{root}, members...)
	if pkgs, err = s.removalReached(ctx, pkgs); err != nil {
		return nil, err
	}
	var clients []*registry.Client
	defer func() {
		for _, c := range clients {
			c.CloseIdle()
		}
	}()
	headersFor := func(p *database.InstalledPackage) func(string) ([]*model.SMTPHeader, bool) {
		return s.previewSMTPHeaders(ctx, p, &clients)
	}
	out, _, err := s.previewRemoval(ctx, pkgs, nil, headersFor, nil, nil)
	if err != nil {
		return nil, err
	}
	if out.UsedBy, err = s.packageUsers(ctx, installedPackageID); err != nil {
		return nil, errs.Wrap(err)
	}
	if out.UsedBy == nil {
		out.UsedBy = []string{}
	}
	return out, nil
}

// previewRemoval reports what removing pkgs would affect, the first being the
// package removed and the rest the referenced packages removed with it. others
// are packages the same removal takes too that are reported on their own:
// their assets and domains count as gone, nothing else of them is reported.
// declaredFor returns the smtp header lookup of a package. A campaign template
// in skipTemplates is left out, as if its campaigns do not use the content. It
// also returns the campaign templates the removal closes campaigns of.
// plannedLists is as for previewAssetLosses.
func (s *ContentRegistry) previewRemoval(
	ctx context.Context, pkgs []*database.InstalledPackage,
	others []*database.InstalledPackage,
	declaredFor func(p *database.InstalledPackage) func(string) ([]*model.SMTPHeader, bool),
	skipTemplates map[uuid.UUID]bool,
	plannedLists map[uuid.UUID]*plannedAssetLists,
) (*UninstallPreview, []uuid.UUID, error) {
	out := &UninstallPreview{
		Campaigns:     []UninstallPreviewCampaign{},
		Changed:       []UninstallPreviewChanged{},
		ChangedAssets: []UninstallPreviewAsset{},
		Edited:        []UninstallPreviewItem{},
		Members:       []string{},
		KeptLists:     []UninstallPreviewKeptList{},
		Blocked:       []UninstallPreviewBlocked{},
		Programs:      []UninstallPreviewProgram{},
	}
	for _, p := range pkgs[1:] {
		out.Members = append(out.Members, p.PackageID)
	}

	var templateIDs []*uuid.UUID
	seenTemplate := map[uuid.UUID]bool{}
	addTemplates := func(ids []uuid.UUID) {
		for _, id := range ids {
			if skipTemplates[id] {
				continue
			}
			if !seenTemplate[id] {
				seenTemplate[id] = true
				tid := id
				templateIDs = append(templateIDs, &tid)
			}
		}
	}
	// language rows that lose content. Only the campaigns and programs that
	// can reach the language are affected, see closeCampaignsUsingContent
	var languageUses []repository.TemplateLanguageUse
	var domainIDs []uuid.UUID
	// templates the removal deletes itself. their campaigns close now, also when
	// they already have a later close time.
	var deletedTemplates []*uuid.UUID
	trackedAssets := map[uuid.UUID]bool{}
	removedLists := map[uuid.UUID]bool{}
	// lists are judged once every asset and domain the removal deletes is known.
	var listRows []*database.InstalledResource
	removing := map[uuid.UUID]bool{}
	for _, p := range pkgs {
		removing[*p.ID] = true
	}
	// the domains of others delete their assets too, but are reported with them.
	var otherDomains []uuid.UUID
	for _, p := range others {
		removing[*p.ID] = true
		resources, rerr := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, p.ID)
		if rerr != nil {
			return nil, nil, errs.Wrap(rerr)
		}
		for _, r := range resources {
			if r.ResourceID == nil {
				continue
			}
			switch r.ResourceType {
			case "asset":
				trackedAssets[*r.ResourceID] = true
			case "domain":
				otherDomains = append(otherDomains, *r.ResourceID)
			case "proxy":
				pds, perr := s.proxyDomainsOf(ctx, r.ResourceID)
				if perr != nil {
					return nil, nil, errs.Wrap(perr)
				}
				for _, pd := range pds {
					otherDomains = append(otherDomains, pd.ID)
				}
			}
		}
	}
	losses := map[uuid.UUID]*campaignLoss{}
	var lossOrder []uuid.UUID
	addLoss := func(c *model.Campaign, text string) *campaignLoss {
		cid, err := c.ID.Get()
		if err != nil {
			return nil
		}
		l, ok := losses[cid]
		if !ok {
			l = &campaignLoss{campaign: c, removedAllow: map[uuid.UUID]bool{}, removedFilter: map[uuid.UUID]bool{}}
			losses[cid] = l
			lossOrder = append(lossOrder, cid)
		}
		if !slices.Contains(l.losses, text) {
			l.losses = append(l.losses, text)
		}
		return l
	}

	programs := map[uuid.UUID]*UninstallPreviewProgram{}
	var programOrder []uuid.UUID
	addProgram := func(p repository.ProgramUse, text string, blocks bool) {
		v, ok := programs[p.ID]
		if !ok {
			v = &UninstallPreviewProgram{ID: p.ID.String(), Name: p.Name, Losses: []string{}}
			programs[p.ID] = v
			programOrder = append(programOrder, p.ID)
		}
		if !slices.Contains(v.Losses, text) {
			v.Losses = append(v.Losses, text)
		}
		v.Blocks = v.Blocks || blocks
	}
	// a deleted list, page or webhook is taken off the programs using it
	addProgramLosses := func(r *database.InstalledResource) error {
		what := ""
		switch r.ResourceType {
		case "allowdeny":
			what = "allow or block list"
		case "page":
			what = "page"
		case "webhook":
			what = "webhook"
		default:
			return nil
		}
		uses, err := s.openProgramsUsing(ctx, r.ResourceType, r.ResourceID)
		if err != nil {
			return err
		}
		name := s.resourceName(ctx, r)
		for _, u := range uses {
			addProgram(u, fmt.Sprintf("%s %q removed", what, name), false)
		}
		return nil
	}

	for _, p := range pkgs {
		resources, rerr := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, p.ID)
		if rerr != nil {
			return nil, nil, errs.Wrap(rerr)
		}
		declared := declaredFor(p)
		for _, r := range resources {
			if r.ResourceID == nil {
				continue
			}
			// a page an open campaign or program filters with is refused by the
			// page delete, so it stays and nothing using it changes
			users, total, berr := s.pageDeleteBlockers(ctx, r)
			if berr != nil {
				return nil, nil, berr
			}
			if total > 0 {
				out.Blocked = append(out.Blocked, UninstallPreviewBlocked{
					ResourceType: r.ResourceType, Name: s.resourceName(ctx, r), PackageID: p.PackageID,
					UsedBy: users, UsedByCount: int(total),
				})
				continue
			}
			if r.ResourceType == "asset" {
				trackedAssets[*r.ResourceID] = true
			}
			if r.ResourceType == "allowdeny" {
				listRows = append(listRows, r)
			}
			if edited, name := s.resourceEdited(ctx, r, declared); edited {
				if name == "" {
					name = r.SourceName
				}
				out.Edited = append(out.Edited, UninstallPreviewItem{
					ResourceType: r.ResourceType, Name: name, SourceName: r.SourceName, PackageID: p.PackageID,
				})
			}
			ids, uses, terr := s.templatesAffectedByDelete(ctx, r.ResourceType, r.ResourceID)
			if terr != nil {
				return nil, nil, errs.Wrap(terr)
			}
			addTemplates(ids)
			for _, use := range uses {
				if !skipTemplates[use.TemplateID] {
					languageUses = append(languageUses, use)
				}
			}
			if r.ResourceType == "campaigntemplate" && r.ResourceID != nil && !skipTemplates[*r.ResourceID] {
				deletedTemplates = append(deletedTemplates, r.ResourceID)
			}
			switch r.ResourceType {
			case "domain":
				domainIDs = append(domainIDs, *r.ResourceID)
			case "proxy":
				// deleting a proxy also deletes its proxy domains, which closes the
				// campaigns whose template uses one of them and deletes their assets.
				pds, perr := s.proxyDomainsOf(ctx, r.ResourceID)
				if perr != nil {
					return nil, nil, errs.Wrap(perr)
				}
				for _, pd := range pds {
					id := pd.ID
					dids, derr := s.templatesClosedByDelete(ctx, "domain", &id)
					if derr != nil {
						return nil, nil, errs.Wrap(derr)
					}
					addTemplates(dids)
					domainIDs = append(domainIDs, id)
				}
			}
			if r.ResourceType == "allowdeny" {
				continue
			}
			if lerr := s.collectCampaignLosses(ctx, r, addLoss); lerr != nil {
				return nil, nil, errs.Wrap(lerr)
			}
			if perr := addProgramLosses(r); perr != nil {
				return nil, nil, errs.Wrap(perr)
			}
		}
	}
	// a list an asset the removal keeps still uses stays, as remove keeps it, so
	// neither the asset nor a campaign loses it.
	gone := s.assetRemovedFunc(ctx, trackedAssets, removing, append(slices.Clone(domainIDs), otherDomains...))
	for _, r := range listRows {
		kept, kerr := s.keptListUsers(ctx, r.ResourceID, gone, plannedLists)
		if kerr != nil {
			return nil, nil, errs.Wrap(kerr)
		}
		if len(kept) > 0 {
			out.KeptLists = append(out.KeptLists, s.previewKeptList(ctx, r, kept))
			continue
		}
		removedLists[*r.ResourceID] = true
		if lerr := s.collectCampaignLosses(ctx, r, addLoss); lerr != nil {
			return nil, nil, errs.Wrap(lerr)
		}
		if perr := addProgramLosses(r); perr != nil {
			return nil, nil, errs.Wrap(perr)
		}
	}

	// programs with a step on a template the removal deletes block it, a step
	// whose template loses content keeps running without those sends
	if s.programRepository() != nil {
		deleted := map[uuid.UUID]bool{}
		for _, id := range deletedTemplates {
			deleted[*id] = true
		}
		for _, id := range templateIDs {
			uses, perr := s.programRepository().GetOpenUsingTemplates(ctx, []*uuid.UUID{id})
			if perr != nil {
				return nil, nil, errs.Wrap(perr)
			}
			name := s.campaignTemplateName(ctx, *id)
			for _, u := range uses {
				if deleted[*id] {
					addProgram(u, fmt.Sprintf("template %q deleted", name), true)
				} else {
					addProgram(u, fmt.Sprintf("template %q loses content, its open sends close", name), false)
				}
			}
		}
		for _, id := range deletedTemplates {
			if seenTemplate[*id] {
				continue
			}
			uses, perr := s.programRepository().GetOpenUsingTemplates(ctx, []*uuid.UUID{id})
			if perr != nil {
				return nil, nil, errs.Wrap(perr)
			}
			name := s.campaignTemplateName(ctx, *id)
			for _, u := range uses {
				addProgram(u, fmt.Sprintf("template %q deleted", name), true)
			}
		}
		// a program that picks a language losing content holds only the sends
		// in that language
		for _, use := range languageUses {
			if seenTemplate[use.TemplateID] {
				continue
			}
			templateID := use.TemplateID
			uses, perr := s.programRepository().GetOpenUsingTemplateLanguages(ctx, &templateID, []string{use.Language})
			if perr != nil {
				return nil, nil, errs.Wrap(perr)
			}
			name := s.campaignTemplateName(ctx, templateID)
			for _, u := range uses {
				addProgram(u, fmt.Sprintf(
					"template %q loses its %s content, its sends in that language wait",
					name, data.LanguageName(use.Language),
				), false)
			}
		}
	}
	// the blocking programs are listed first, so the cap never hides them all
	for _, blocking := range []bool{true, false} {
		for _, id := range programOrder {
			p := programs[id]
			if p.Blocks != blocking {
				continue
			}
			out.ProgramCount++
			out.ProgramsBlock = out.ProgramsBlock || p.Blocks
			if len(out.Programs) < uninstallPreviewCampaignCap {
				out.Programs = append(out.Programs, *p)
			}
		}
	}

	campaignRepo := s.campaignRepository()
	closing := map[uuid.UUID]bool{}
	now := time.Now()
	if len(templateIDs) > 0 && campaignRepo != nil {
		campaigns, err := campaignRepo.GetByTemplateIDs(ctx, templateIDs)
		if err != nil {
			return nil, nil, errs.Wrap(err)
		}
		// a program's sends are reported under Programs
		for _, c := range withoutProgramCampaigns(campaigns) {
			if !campaignStillRunning(c, now) {
				continue
			}
			// removing a template or its content closes its campaigns now,
			// also one that already has a later close time
			v := previewCampaign(c)
			if cid, err := c.ID.Get(); err == nil {
				closing[cid] = true
			}
			out.CampaignCount++
			if len(out.Campaigns) < uninstallPreviewCampaignCap {
				out.Campaigns = append(out.Campaigns, v)
			}
		}
	}

	// a campaign on a template whose language row loses content closes when
	// it can reach that language, as the removal does
	if len(languageUses) > 0 && campaignRepo != nil {
		for _, use := range languageUses {
			if seenTemplate[use.TemplateID] {
				continue
			}
			templateID := use.TemplateID
			campaigns, err := campaignRepo.GetByTemplateIDs(ctx, []*uuid.UUID{&templateID})
			if err != nil {
				return nil, nil, errs.Wrap(err)
			}
			byID := map[uuid.UUID]*model.Campaign{}
			candidates := []uuid.UUID{}
			for _, c := range withoutProgramCampaigns(campaigns) {
				cid, err := c.ID.Get()
				if err != nil || closing[cid] || !campaignStillRunning(c, now) {
					continue
				}
				byID[cid] = c
				candidates = append(candidates, cid)
			}
			reach, err := campaignRepo.FilterCampaignsUsingTemplateLanguage(ctx, candidates, templateID, use.Language)
			if err != nil {
				return nil, nil, errs.Wrap(err)
			}
			for _, cid := range reach {
				c, ok := byID[cid]
				if !ok || closing[cid] {
					continue
				}
				closing[cid] = true
				out.CampaignCount++
				if len(out.Campaigns) < uninstallPreviewCampaignCap {
					out.Campaigns = append(out.Campaigns, previewCampaign(c))
				}
			}
		}
	}

	for _, cid := range lossOrder {
		l := losses[cid]
		if closing[cid] || !campaignStillRunning(l.campaign, now) {
			continue
		}
		open := false
		listsLeft := 0
		if len(l.removedFilter) > 0 && campaignRepo != nil {
			open = len(l.removedAllow) > 0
			lists, err := campaignRepo.GetAllDenyByCampaignID(ctx, &cid)
			if err != nil {
				return nil, nil, errs.Wrap(err)
			}
			for _, ad := range lists {
				id, ierr := ad.ID.Get()
				if ierr != nil || l.removedFilter[id] {
					continue
				}
				listsLeft++
				if allowed, aerr := ad.Allowed.Get(); aerr == nil && allowed {
					open = false
				}
			}
		}
		texts := l.losses
		// a list delete clears the deny page once no list and no evasion page
		// needs it anymore
		if len(l.removedFilter) > 0 && listsLeft == 0 && !l.denyPageRemoved {
			_, denyErr := l.campaign.DenyPageID.Get()
			_, evasionErr := l.campaign.EvasionPageID.Get()
			if denyErr == nil && evasionErr != nil {
				texts = append(texts, "deny page cleared")
			}
		}
		if open {
			texts = append([]string{"IP filter removed: campaign will be reachable by everyone"}, texts...)
		}
		out.ChangedCount++
		if len(out.Changed) >= uninstallPreviewCampaignCap {
			continue
		}
		v := previewCampaign(l.campaign)
		out.Changed = append(out.Changed, UninstallPreviewChanged{
			ID: v.ID, Name: v.Name, Losses: texts, OpenToEveryone: open,
		})
	}

	// deleting a domain deletes every asset on it, including files the user
	// uploaded after install.
	if s.Asset != nil && s.Asset.AssetRepository != nil {
		seenDomain := map[uuid.UUID]bool{}
		for _, did := range domainIDs {
			if seenDomain[did] {
				continue
			}
			seenDomain[did] = true
			id := did
			assets, err := s.Asset.AssetRepository.GetAllByDomainID(ctx, &id)
			if err != nil {
				return nil, nil, errs.Wrap(err)
			}
			for _, a := range assets {
				if aid, err := a.ID.Get(); err == nil && !trackedAssets[aid] {
					out.UserAssetCount++
				}
			}
		}
	}
	err := s.previewAssetLosses(ctx, out, removedLists, gone, plannedLists)
	if err != nil {
		return nil, nil, err
	}
	closed := make([]uuid.UUID, 0, len(templateIDs))
	for _, id := range templateIDs {
		closed = append(closed, *id)
	}
	for _, use := range languageUses {
		if !seenTemplate[use.TemplateID] && !slices.Contains(closed, use.TemplateID) {
			closed = append(closed, use.TemplateID)
		}
	}
	return out, closed, nil
}

// pageDeleteBlockers returns, for an installed page, the open campaigns and
// programs that use it as deny or evasion page and how many there are. The
// page delete refuses such a page, so a removal keeps it installed. Any other
// resource has none.
func (s *ContentRegistry) pageDeleteBlockers(
	ctx context.Context, r *database.InstalledResource,
) ([]string, int64, error) {
	if r.ResourceType != "page" || r.ResourceID == nil || s.Page == nil || s.Page.CampaignRepository == nil {
		return nil, 0, nil
	}
	return s.Page.OpenFilterPageUsers(ctx, r.ResourceID)
}

// removalReached returns the packages of pkgs that remove gets to, pkgs being
// the package removed first and then the referenced packages removed with it.
// remove stops at a package with a page it can not delete: that package stays
// installed, so the packages it references stay too. Such a package is still
// reached when every package linking it is removed, and its other content is
// deleted.
func (s *ContentRegistry) removalReached(
	ctx context.Context, pkgs []*database.InstalledPackage,
) ([]*database.InstalledPackage, error) {
	blocked := map[uuid.UUID]bool{}
	for _, p := range pkgs {
		resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, p.ID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, r := range resources {
			_, total, err := s.pageDeleteBlockers(ctx, r)
			if err != nil {
				return nil, err
			}
			if total > 0 {
				blocked[*p.ID] = true
				break
			}
		}
	}
	root := pkgs[0]
	if len(blocked) == 0 {
		return pkgs, nil
	}
	if blocked[*root.ID] {
		return pkgs[:1], nil
	}
	taken, err := s.removalSet(ctx, []*uuid.UUID{root.ID}, nil, nil, func(mp *database.InstalledPackage) bool {
		return blocked[*mp.ID]
	})
	if err != nil {
		return nil, err
	}
	removed := map[uuid.UUID]bool{*root.ID: true}
	for _, p := range taken {
		removed[*p.ID] = true
	}
	out := []*database.InstalledPackage{root}
	for _, p := range pkgs[1:] {
		if removed[*p.ID] {
			out = append(out, p)
			continue
		}
		if !blocked[*p.ID] {
			continue
		}
		parents, err := s.RegistryRepository.GetPackageParents(ctx, p.ID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		reached := true
		for _, pp := range parents {
			if pp != nil && !removed[*pp] {
				reached = false
				break
			}
		}
		if reached {
			out = append(out, p)
		}
	}
	return out, nil
}

// assetRemovedFunc returns a check for whether a removal deletes an asset: one
// on a domain the removal deletes, and one tracked by a removed package and by
// no other install. removing holds the ids of the removed packages.
func (s *ContentRegistry) assetRemovedFunc(
	ctx context.Context,
	trackedAssets map[uuid.UUID]bool,
	removing map[uuid.UUID]bool,
	domainIDs []uuid.UUID,
) func(a *model.Asset) (bool, error) {
	removedDomain := map[uuid.UUID]bool{}
	for _, d := range domainIDs {
		removedDomain[d] = true
	}
	return func(a *model.Asset) (bool, error) {
		if did, derr := a.DomainID.Get(); derr == nil && removedDomain[did] {
			return true, nil
		}
		id, err := a.ID.Get()
		if err != nil || !trackedAssets[id] {
			return false, nil
		}
		rows, err := s.RegistryRepository.GetInstalledResourcesByResource(ctx, "asset", &id)
		if err != nil {
			return false, err
		}
		for _, r := range rows {
			if r.InstalledPackageID != nil && !removing[*r.InstalledPackageID] {
				return false, nil
			}
		}
		return true, nil
	}
}

// keptListUsers returns the labels of the assets that keep a list in place
// after a removal: assets that use it, that the removal does not delete (gone),
// and that an update does not take it off first (plannedLists, as for
// previewAssetLosses). An empty result means the removal deletes the list.
func (s *ContentRegistry) keptListUsers(
	ctx context.Context,
	listID *uuid.UUID,
	gone func(a *model.Asset) (bool, error),
	plannedLists map[uuid.UUID]*plannedAssetLists,
) ([]string, error) {
	if s.Asset == nil || s.Asset.AssetRepository == nil {
		return nil, nil
	}
	assets, err := s.Asset.AssetRepository.GetByAllowDenyID(ctx, listID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range assets {
		removed, rerr := gone(a)
		if rerr != nil {
			return nil, rerr
		}
		if removed {
			continue
		}
		if aid, ierr := a.ID.Get(); ierr == nil {
			if planned, replaced := plannedLists[aid]; replaced && !planned.ids[*listID] {
				continue
			}
		}
		out = append(out, assetLabel(a))
	}
	return out, nil
}

// previewKeptList describes a list the removal keeps for the assets in users.
func (s *ContentRegistry) previewKeptList(
	ctx context.Context, r *database.InstalledResource, users []string,
) UninstallPreviewKeptList {
	v := UninstallPreviewKeptList{Name: r.SourceName, Assets: users}
	if s.AllowDeny != nil && s.AllowDeny.AllowDenyRepository != nil {
		if l, err := s.AllowDeny.AllowDenyRepository.GetByID(ctx, r.ResourceID, &repository.AllowDenyOption{}); err == nil {
			if n := optStr(l.Name); n != "" {
				v.Name = n
			}
			v.Allowed, _ = l.Allowed.Get()
		}
	}
	if len(v.Assets) > uninstallPreviewCampaignCap {
		v.Assets = v.Assets[:uninstallPreviewCampaignCap]
	}
	return v
}

// previewAssetLosses adds to out each asset that stays after the removal but
// loses one of removedLists. An asset the removal deletes (gone) is left out.
// plannedLists maps an asset whose lists an update replaces before the removal
// to its new lists: a list it uses now that is not among them is taken off by
// the update and is not counted as lost.
func (s *ContentRegistry) previewAssetLosses(
	ctx context.Context,
	out *UninstallPreview,
	removedLists map[uuid.UUID]bool,
	gone func(a *model.Asset) (bool, error),
	plannedLists map[uuid.UUID]*plannedAssetLists,
) error {
	if len(removedLists) == 0 || s.Asset == nil || s.Asset.AssetRepository == nil {
		return nil
	}
	repo := s.Asset.AssetRepository
	listIDs := make([]uuid.UUID, 0, len(removedLists))
	for id := range removedLists {
		listIDs = append(listIDs, id)
	}
	sort.Slice(listIDs, func(i, j int) bool { return listIDs[i].String() < listIDs[j].String() })
	assets := map[uuid.UUID]*model.Asset{}
	var order []uuid.UUID
	for _, lid := range listIDs {
		id := lid
		linked, err := repo.GetByAllowDenyID(ctx, &id)
		if err != nil {
			return errs.Wrap(err)
		}
		for _, a := range linked {
			aid, ierr := a.ID.Get()
			if ierr != nil {
				continue
			}
			if _, ok := assets[aid]; ok {
				continue
			}
			removed, gerr := gone(a)
			if gerr != nil {
				return errs.Wrap(gerr)
			}
			if removed {
				continue
			}
			assets[aid] = a
			order = append(order, aid)
		}
	}
	for _, aid := range order {
		a := assets[aid]
		id := aid
		lists, err := repo.GetAllowDenyByAssetID(ctx, &id)
		if err != nil {
			return errs.Wrap(err)
		}
		v := UninstallPreviewAsset{
			ID: aid.String(), Name: optStr(a.Name), Path: optStr(a.Path),
			DomainName: optStr(a.DomainName), Losses: []string{},
		}
		planned, replaced := plannedLists[aid]
		lostAllow, leftAllow, left := false, false, 0
		onAsset := map[uuid.UUID]bool{}
		for _, l := range lists {
			lid, ierr := l.ID.Get()
			if ierr != nil {
				continue
			}
			onAsset[lid] = true
			// the update takes this list off the asset before the removal.
			if replaced && !planned.ids[lid] {
				continue
			}
			allowed, _ := l.Allowed.Get()
			if removedLists[lid] {
				kind := "block list"
				if allowed {
					kind = "allow list"
					lostAllow = true
				}
				v.Losses = append(v.Losses, fmt.Sprintf("%s %q removed", kind, optStr(l.Name)))
				continue
			}
			left++
			if allowed {
				leftAllow = true
			}
		}
		if replaced {
			if len(v.Losses) == 0 {
				continue
			}
			// a list the update adds to the asset stays after the removal, unless
			// the removal deletes it too.
			for lid := range planned.ids {
				if onAsset[lid] || removedLists[lid] {
					continue
				}
				left++
				id := lid
				if s.AllowDeny == nil || s.AllowDeny.AllowDenyRepository == nil {
					continue
				}
				l, gerr := s.AllowDeny.AllowDenyRepository.GetByID(ctx, &id, &repository.AllowDenyOption{})
				if gerr != nil {
					continue
				}
				if allowed, _ := l.Allowed.Get(); allowed {
					leftAllow = true
				}
			}
			for _, allowed := range planned.created {
				left++
				if allowed {
					leftAllow = true
				}
			}
		}
		v.OpenToEveryone = lostAllow && !leftAllow
		v.Unfiltered = left == 0
		if v.OpenToEveryone {
			v.Losses = append([]string{"Filter removed: asset will be reachable by everyone"}, v.Losses...)
		}
		out.ChangedAssetCount++
		if len(out.ChangedAssets) < uninstallPreviewCampaignCap {
			out.ChangedAssets = append(out.ChangedAssets, v)
		}
	}
	return nil
}

// assetLabel names an asset by its path, with its domain when it has one.
func assetLabel(a *model.Asset) string {
	p := optStr(a.Path)
	if d := optStr(a.DomainName); d != "" {
		return d + "/" + p
	}
	return p
}

// previewCampaign is the id and name of a campaign for the preview.
func previewCampaign(c *model.Campaign) UninstallPreviewCampaign {
	v := UninstallPreviewCampaign{}
	if id, err := c.ID.Get(); err == nil {
		v.ID = id.String()
	}
	if n, err := c.Name.Get(); err == nil {
		v.Name = n.String()
	}
	return v
}

// withoutProgramCampaigns leaves out the hidden campaigns programs send with. A
// preview names the program instead.
func withoutProgramCampaigns(campaigns []*model.Campaign) []*model.Campaign {
	out := make([]*model.Campaign, 0, len(campaigns))
	for _, c := range campaigns {
		if !c.IsProgramManaged() {
			out = append(out, c)
		}
	}
	return out
}

// programRepository is the program repository the entity services use, or nil
// when it is not wired.
func (s *ContentRegistry) programRepository() *repository.Program {
	if s.CampaignTemplate == nil {
		return nil
	}
	return s.CampaignTemplate.ProgramRepository
}

// openProgramsUsing returns the open programs that use an entity: a campaign
// template as a step, an allow or deny list, a deny or evasion page, or a
// webhook. Closed programs never count.
func (s *ContentRegistry) openProgramsUsing(
	ctx context.Context, resourceType string, id *uuid.UUID,
) ([]repository.ProgramUse, error) {
	repo := s.programRepository()
	if repo == nil || id == nil {
		return nil, nil
	}
	switch resourceType {
	case "campaigntemplate":
		return repo.GetOpenUsingTemplates(ctx, []*uuid.UUID{id})
	case "allowdeny":
		return repo.GetOpenUsingAllowDeny(ctx, id)
	case "page":
		return repo.GetOpenUsingPage(ctx, id)
	case "webhook":
		return repo.GetOpenUsingWebhook(ctx, id)
	}
	return nil, nil
}

// campaignRepository is the campaign repository the entity services use, or nil
// when it is not wired.
func (s *ContentRegistry) campaignRepository() *repository.Campaign {
	if s.CampaignTemplate == nil {
		return nil
	}
	return s.CampaignTemplate.CampaignRepository
}

// proxyDomainsOf returns the proxy domains deleting a proxy also deletes. A
// proxy that no longer exists has none.
func (s *ContentRegistry) proxyDomainsOf(ctx context.Context, proxyID *uuid.UUID) ([]ProxyDomain, error) {
	if s.Proxy == nil || s.ProxyRepository == nil {
		return nil, nil
	}
	px, err := s.ProxyRepository.GetByID(ctx, proxyID, &repository.ProxyOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pds, err := s.Proxy.ProxyDomains(ctx, px)
	if err != nil {
		// a config that does not parse deletes no domains either
		return nil, nil
	}
	return pds, nil
}

// collectCampaignLosses records what campaigns lose, other than being closed,
// when the entity service deletes this entity: an allow or block list, a deny
// page (which takes the lists and the evasion page that need it along), an
// evasion page, a webhook or a script. A deny page that a list delete leaves
// unused is reported once all losses are known. The hidden campaigns of a
// program are left out, the program is reported instead.
func (s *ContentRegistry) collectCampaignLosses(
	ctx context.Context,
	r *database.InstalledResource,
	addLoss func(c *model.Campaign, text string) *campaignLoss,
) error {
	repo := s.campaignRepository()
	if repo == nil {
		return nil
	}
	listLabel := func(allowed bool, name string) string {
		if allowed {
			return fmt.Sprintf("allow list %q removed", name)
		}
		return fmt.Sprintf("block list %q removed", name)
	}
	switch r.ResourceType {
	case "allowdeny":
		allowed := false
		name := r.SourceName
		if s.AllowDeny != nil && s.AllowDeny.AllowDenyRepository != nil {
			ad, err := s.AllowDeny.AllowDenyRepository.GetByID(ctx, r.ResourceID, &repository.AllowDenyOption{})
			if err == nil {
				allowed, _ = ad.Allowed.Get()
				if n := optStr(ad.Name); n != "" {
					name = n
				}
			}
		}
		campaigns, err := repo.GetByAllowDenyID(ctx, r.ResourceID)
		if err != nil {
			return err
		}
		for _, c := range withoutProgramCampaigns(campaigns) {
			l := addLoss(c, listLabel(allowed, name))
			if l == nil {
				continue
			}
			l.removedFilter[*r.ResourceID] = true
			if allowed {
				l.removedAllow[*r.ResourceID] = true
			}
		}
	case "page":
		name := s.resourceName(ctx, r)
		campaigns, err := repo.GetByDenyPageID(ctx, r.ResourceID)
		if err != nil {
			return err
		}
		for _, c := range withoutProgramCampaigns(campaigns) {
			l := addLoss(c, fmt.Sprintf("deny page %q removed", name))
			if l == nil {
				continue
			}
			l.denyPageRemoved = true
			cid, _ := c.ID.Get()
			lists, lerr := repo.GetAllDenyByCampaignID(ctx, &cid)
			if lerr != nil {
				return lerr
			}
			for _, ad := range lists {
				id, ierr := ad.ID.Get()
				if ierr != nil {
					continue
				}
				allowed, _ := ad.Allowed.Get()
				addLoss(c, listLabel(allowed, optStr(ad.Name)))
				l.removedFilter[id] = true
				if allowed {
					l.removedAllow[id] = true
				}
			}
			if _, err := c.EvasionPageID.Get(); err == nil {
				addLoss(c, "evasion page cleared")
			}
		}
		evading, err := repo.GetByEvasionPageID(ctx, r.ResourceID)
		if err != nil {
			return err
		}
		for _, c := range withoutProgramCampaigns(evading) {
			addLoss(c, fmt.Sprintf("evasion page %q removed", name))
		}
	case "webhook", "script":
		get, what := repo.GetByWebhookID, "webhook"
		if r.ResourceType == "script" {
			get, what = repo.GetByScriptID, "script"
		}
		campaigns, err := get(ctx, r.ResourceID)
		if err != nil {
			return err
		}
		name := s.resourceName(ctx, r)
		for _, c := range withoutProgramCampaigns(campaigns) {
			addLoss(c, fmt.Sprintf("%s %q removed", what, name))
		}
	}
	return nil
}

// previewSMTPHeaders returns a lookup of the smtp headers the installed version
// of a package declared, downloaded on first use under a short timeout. The
// lookup reports false when that version cannot be read.
func (s *ContentRegistry) previewSMTPHeaders(
	ctx context.Context, ip *database.InstalledPackage, clients *[]*registry.Client,
) func(source string) ([]*model.SMTPHeader, bool) {
	fetch := func(id string) (*registry.VerifiedPackage, error) {
		reg, err := s.RegistryRepository.GetRegistryByID(ctx, ip.RegistryID)
		if err != nil {
			return nil, err
		}
		base, version, _ := strings.Cut(id, "@")
		ns, nm := splitPackageID(base)
		if ns == "" || nm == "" {
			return nil, fmt.Errorf("invalid package id")
		}
		client := s.clientFor(reg)
		*clients = append(*clients, client)
		fctx, cancel := context.WithTimeout(ctx, uninstallPreviewFetchTimeout)
		defer cancel()
		return client.FetchVerifiedVersion(fctx, ns, nm, version)
	}
	return s.boundSMTPHeaders(ctx, ip, fetch)
}

// boundSMTPHeaders returns a lookup of the smtp headers the installed version of
// a package declared, read through fetch and filled with the values the install
// bound.
func (s *ContentRegistry) boundSMTPHeaders(
	ctx context.Context,
	ip *database.InstalledPackage,
	fetch func(id string) (*registry.VerifiedPackage, error),
) func(source string) ([]*model.SMTPHeader, bool) {
	effective := func(old *registry.VerifiedPackage) map[string]string {
		return effectiveVarsFor(old, s.mergeBoundVars(ctx, ip.ID, nil), installedShare(ip, old.Manifest))
	}
	return installedSMTPHeaders(ip, fetch, effective)
}

// campaignStillRunning reports whether a campaign is neither closed nor past its
// close time, the campaigns a delete of their template or its pieces affects.
func campaignStillRunning(c *model.Campaign, now time.Time) bool {
	if !c.IsActive() {
		return false
	}
	if at, err := c.CloseAt.Get(); err == nil && !at.After(now) {
		return false
	}
	return true
}

// removalSet returns the installed packages a removal takes with it besides
// roots, the packages it removes anyway, using the rule remove follows: a
// package installed only as a dependency goes once every link to it goes, a
// link from a removed package or one linkGone reports. The members of roots
// and candidates are considered, and the members of each package taken. A
// package is taken only once all packages linking it are, and the passes
// repeat until nothing changes, so the result does not depend on the order
// links are read in: remove gets the same result by removing one member after
// another. keep reports a package that stays whatever its links.
func (s *ContentRegistry) removalSet(
	ctx context.Context,
	roots []*uuid.UUID,
	candidates []*uuid.UUID,
	linkGone func(parent, child uuid.UUID) bool,
	keep func(mp *database.InstalledPackage) bool,
) ([]*database.InstalledPackage, error) {
	removed := map[uuid.UUID]bool{}
	queued := map[uuid.UUID]bool{}
	var order []*uuid.UUID
	enqueue := func(ids []*uuid.UUID) {
		for _, id := range ids {
			if id != nil && !queued[*id] {
				queued[*id] = true
				order = append(order, id)
			}
		}
	}
	for _, r := range roots {
		removed[*r] = true
	}
	for _, r := range roots {
		members, err := s.RegistryRepository.GetPackageMembers(ctx, r)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		enqueue(members)
	}
	enqueue(candidates)
	var out []*database.InstalledPackage
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(order); i++ {
			c := order[i]
			if removed[*c] {
				continue
			}
			mp, err := s.RegistryRepository.GetInstalledPackageByID(ctx, c)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return nil, errs.Wrap(err)
			}
			if !mp.InstalledAsDependency || (keep != nil && keep(mp)) {
				continue
			}
			parents, err := s.RegistryRepository.GetPackageParents(ctx, c)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			stays := false
			for _, p := range parents {
				if p == nil || removed[*p] || (linkGone != nil && linkGone(*p, *c)) {
					continue
				}
				stays = true
				break
			}
			if stays {
				continue
			}
			removed[*c] = true
			out = append(out, mp)
			changed = true
			members, err := s.RegistryRepository.GetPackageMembers(ctx, c)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			enqueue(members)
		}
	}
	return out, nil
}

// templatesClosedByDelete returns the campaign templates whose active campaigns
// the entity service closes when it deletes this entity.
func (s *ContentRegistry) templatesClosedByDelete(
	ctx context.Context, resourceType string, id *uuid.UUID,
) ([]uuid.UUID, error) {
	if resourceType == "campaigntemplate" {
		return []uuid.UUID{*id}, nil
	}
	repo := s.CampaignTemplateRepository
	sp, ok := resourceSpecs[resourceType]
	if repo == nil || !ok {
		return nil, nil
	}
	templates, found, err := sp.templatesUsing(repo, ctx, id)
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(templates))
	for _, t := range templates {
		if tid, err := t.ID.Get(); err == nil {
			out = append(out, tid)
		}
	}
	return out, nil
}

// templatesAffectedByDelete splits the templates a delete strips content
// from into the templates that hold an email or page themselves, whose
// campaigns all close, and the language rows that hold it. Other resource
// types give only templates that hold it themselves.
func (s *ContentRegistry) templatesAffectedByDelete(
	ctx context.Context, resourceType string, id *uuid.UUID,
) ([]uuid.UUID, []repository.TemplateLanguageUse, error) {
	if (resourceType != "email" && resourceType != "page") || s.CampaignTemplateRepository == nil {
		ids, err := s.templatesClosedByDelete(ctx, resourceType, id)
		return ids, nil, err
	}
	repo := s.CampaignTemplateRepository
	var templates []*model.CampaignTemplate
	var uses []repository.TemplateLanguageUse
	var err error
	if resourceType == "email" {
		templates, err = repo.GetByEmailID(ctx, id, &repository.CampaignTemplateOption{})
		if err == nil {
			uses, err = repo.GetLanguageUsesByEmailID(ctx, id)
		}
	} else {
		templates, err = repo.GetByPageID(ctx, id, &repository.CampaignTemplateOption{})
		if err == nil {
			uses, err = repo.GetLanguageUsesByPageID(ctx, id)
		}
	}
	if err != nil {
		return nil, nil, err
	}
	direct := []uuid.UUID{}
	for _, t := range templates {
		columns := []nullable.Nullable[uuid.UUID]{t.EmailID}
		if resourceType == "page" {
			columns = []nullable.Nullable[uuid.UUID]{t.BeforeLandingPageID, t.LandingPageID, t.AfterLandingPageID}
		}
		for _, column := range columns {
			if v, err := column.Get(); err == nil && v == *id {
				direct = append(direct, t.ID.MustGet())
				break
			}
		}
	}
	return direct, uses, nil
}

// resourceEdited reports whether an installed entity differs from what the
// install recorded, using the same stored fingerprints an update compares for
// local edit conflicts, and returns the entity's current name. Types an update
// does not check for local edits, and an entity that no longer exists, read as
// not edited. declared looks up the smtp headers the installed version declared.
func (s *ContentRegistry) resourceEdited(
	ctx context.Context,
	r *database.InstalledResource,
	declared func(source string) ([]*model.SMTPHeader, bool),
) (bool, string) {
	if r.ResourceType == "smtp" {
		if s.SMTPConfigurationRepository == nil {
			return false, ""
		}
		cur, err := s.SMTPConfigurationRepository.GetByID(
			ctx, r.ResourceID, &repository.SMTPConfigurationOption{WithHeaders: true},
		)
		if err != nil {
			return false, ""
		}
		name := optStr(cur.Name)
		if smtpFingerprint(cur) == r.InstalledContentHash {
			return false, name
		}
		// a baseline recorded before headers were part of the fingerprint covers the
		// fields alone. When the fields match it, the headers count as unedited only
		// if they equal what the installed version declared, the same rule an update
		// uses. When that version cannot be read they count as edited.
		if smtpFieldsFingerprint(cur) == r.InstalledContentHash && declared != nil {
			if hs, ok := declared(r.SourceName); ok && sameSMTPHeaders(hs, cur.Headers) {
				return false, name
			}
		}
		return true, name
	}
	fp := s.storedHash(ctx, r.ResourceType, r.ResourceID)
	if fp == "" || fp == r.InstalledContentHash {
		return false, ""
	}
	return true, s.resourceName(ctx, r)
}

// resourceName returns the current name of an installed entity, or the name the
// package gave it when the entity has no name or cannot be read.
func (s *ContentRegistry) resourceName(ctx context.Context, r *database.InstalledResource) string {
	name := ""
	if sp, ok := resourceSpecs[r.ResourceType]; ok {
		name = sp.currentName(s, ctx, r.ResourceID)
	}
	if name == "" {
		return r.SourceName
	}
	return name
}

// checkRemovalNotUsedByReportInbox returns a validation error when a reporting
// inbox signs in with an OAuth provider installed by the package or by a
// referenced package that is removed with it
func (s *ContentRegistry) checkRemovalNotUsedByReportInbox(ctx context.Context, installedPackageID *uuid.UUID) error {
	if s.ReportInboxRepository == nil {
		return nil
	}
	members, err := s.removalSet(ctx, []*uuid.UUID{installedPackageID}, nil, nil, nil)
	if err != nil {
		return err
	}
	ids := []*uuid.UUID{installedPackageID}
	for _, m := range members {
		if m != nil && m.ID != nil {
			ids = append(ids, m.ID)
		}
	}
	for _, id := range ids {
		resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, id)
		if err != nil {
			return errs.Wrap(err)
		}
		for _, r := range resources {
			if r.ResourceType != "oauthprovider" || r.ResourceID == nil {
				continue
			}
			if err := reportInboxUsesProvider(ctx, s.ReportInboxRepository, *r.ResourceID, "uninstall"); err != nil {
				return err
			}
		}
	}
	return nil
}

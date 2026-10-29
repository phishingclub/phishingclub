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
// attachment whose file is already gone has its row removed directly.
func (s *ContentRegistry) deleteResourceEntity(
	ctx context.Context, session *model.Session, resourceType string, id *uuid.UUID,
) error {
	var err error
	switch resourceType {
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
// installed package still references stays too. A global report template the
// install overwrote gets its previous content back. It returns the error so
// the caller can keep the provenance row of anything that was not removed.
func (s *ContentRegistry) removeInstalledResource(
	ctx context.Context, session *model.Session, ipID *uuid.UUID, r *database.InstalledResource,
) error {
	if r.ResourceID == nil || r.ResourceType == "identifier" {
		return nil
	}
	if r.ResourceType == "asset" {
		shared, err := s.assetOwnedByOtherInstall(ctx, r.ResourceID, ipID)
		if err != nil {
			return err
		}
		if shared {
			return nil
		}
	}
	return s.releaseResourceEntity(ctx, session, r.ResourceType, r.ResourceID, r.ReplacedContent)
}

// removeRank is the order a package's resources are deleted in, highest first.
// A resource is deleted before the one it points at: a campaign template before
// what it binds, an api sender before its oauth provider, a nested attachment
// before its email. It matches the order update removes dropped resources in.
func removeRank(resourceType string) int {
	switch resourceType {
	case "campaigntemplate":
		return 4
	case "apisender":
		return 3
	case "attachment":
		return 2
	case "oauthprovider":
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
	// once deleting starts it runs to the end even if the request ends, so an
	// uninstall is never cut off halfway.
	return s.remove(context.WithoutCancel(ctx), session, installedPackageID)
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
// Campaigns are the active campaigns the uninstall closes. KeepRunning are
// active campaigns that already have a close time: deleting their content does
// not move that time, so they keep running without it until they close.
// Changed are active campaigns that lose a filter, deny page, webhook or script.
// UserAssetCount counts the assets on domains the uninstall deletes that the
// package did not install, which are deleted with the domain.
// Members lists the referenced packages a bundle uninstall also removes, which
// leaves out any still used by another bundle or installed by the user directly.
type UninstallPreview struct {
	Campaigns        []UninstallPreviewCampaign `json:"campaigns"`
	CampaignCount    int                        `json:"campaignCount"`
	KeepRunning      []UninstallPreviewCampaign `json:"keepRunning"`
	KeepRunningCount int                        `json:"keepRunningCount"`
	Changed          []UninstallPreviewChanged  `json:"changed"`
	ChangedCount     int                        `json:"changedCount"`
	UserAssetCount   int                        `json:"userAssetCount"`
	Edited           []UninstallPreviewItem     `json:"edited"`
	Members          []string                   `json:"members"`
}

// campaignLoss is what one campaign loses from the deletes of an uninstall.
type campaignLoss struct {
	campaign *model.Campaign
	losses   []string
	// removedAllow holds the allow lists the uninstall deletes from it
	removedAllow map[uuid.UUID]bool
	// removedFilter holds every allow or block list the uninstall deletes from it
	removedFilter map[uuid.UUID]bool
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
	pkgs := []*database.InstalledPackage{root}
	seen := map[uuid.UUID]bool{*root.ID: true}
	if err := s.collectRemovedMembers(ctx, root.ID, seen, &pkgs); err != nil {
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
	out, _, err := s.previewRemoval(ctx, pkgs, headersFor, nil)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// previewRemoval reports what removing pkgs would affect, the first being the
// package removed and the rest the referenced packages removed with it.
// declaredFor returns the smtp header lookup of a package. A campaign template
// in skipTemplates is left out, as if its campaigns do not use the content. It
// also returns the campaign templates the removal closes campaigns of.
func (s *ContentRegistry) previewRemoval(
	ctx context.Context, pkgs []*database.InstalledPackage,
	declaredFor func(p *database.InstalledPackage) func(string) ([]*model.SMTPHeader, bool),
	skipTemplates map[uuid.UUID]bool,
) (*UninstallPreview, []uuid.UUID, error) {
	out := &UninstallPreview{
		Campaigns:   []UninstallPreviewCampaign{},
		KeepRunning: []UninstallPreviewCampaign{},
		Changed:     []UninstallPreviewChanged{},
		Edited:      []UninstallPreviewItem{},
		Members:     []string{},
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
	var domainIDs []uuid.UUID
	trackedAssets := map[uuid.UUID]bool{}
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
			if r.ResourceType == "asset" {
				trackedAssets[*r.ResourceID] = true
			}
			if edited, name := s.resourceEdited(ctx, r, declared); edited {
				if name == "" {
					name = r.SourceName
				}
				out.Edited = append(out.Edited, UninstallPreviewItem{
					ResourceType: r.ResourceType, Name: name, SourceName: r.SourceName, PackageID: p.PackageID,
				})
			}
			ids, terr := s.templatesClosedByDelete(ctx, r.ResourceType, r.ResourceID)
			if terr != nil {
				return nil, nil, errs.Wrap(terr)
			}
			addTemplates(ids)
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
			if lerr := s.collectCampaignLosses(ctx, r, addLoss); lerr != nil {
				return nil, nil, errs.Wrap(lerr)
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
		for _, c := range campaigns {
			if !campaignStillRunning(c, now) {
				continue
			}
			v := previewCampaign(c)
			// Campaign.Close leaves a campaign that already has a close time alone,
			// so only one without a close time is closed now.
			if at, err := c.CloseAt.Get(); err == nil {
				v.CloseAt = at.UTC().Format(time.RFC3339)
				out.KeepRunningCount++
				if len(out.KeepRunning) < uninstallPreviewCampaignCap {
					out.KeepRunning = append(out.KeepRunning, v)
				}
				continue
			}
			if cid, err := c.ID.Get(); err == nil {
				closing[cid] = true
			}
			out.CampaignCount++
			if len(out.Campaigns) < uninstallPreviewCampaignCap {
				out.Campaigns = append(out.Campaigns, v)
			}
		}
	}

	for _, cid := range lossOrder {
		l := losses[cid]
		if closing[cid] || !campaignStillRunning(l.campaign, now) {
			continue
		}
		open := false
		if len(l.removedAllow) > 0 && campaignRepo != nil {
			open = true
			lists, err := campaignRepo.GetAllDenyByCampaignID(ctx, &cid)
			if err != nil {
				return nil, nil, errs.Wrap(err)
			}
			for _, ad := range lists {
				id, ierr := ad.ID.Get()
				if ierr != nil || l.removedFilter[id] {
					continue
				}
				if allowed, aerr := ad.Allowed.Get(); aerr == nil && allowed {
					open = false
					break
				}
			}
		}
		texts := l.losses
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
	closed := make([]uuid.UUID, 0, len(templateIDs))
	for _, id := range templateIDs {
		closed = append(closed, *id)
	}
	return out, closed, nil
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
// when the entity service deletes this entity: an allow or block list (which
// also clears the campaign deny page), a deny page, a webhook or a script.
func (s *ContentRegistry) collectCampaignLosses(
	ctx context.Context,
	r *database.InstalledResource,
	addLoss func(c *model.Campaign, text string) *campaignLoss,
) error {
	repo := s.campaignRepository()
	if repo == nil {
		return nil
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
		kind := "block list"
		if allowed {
			kind = "allow list"
		}
		for _, c := range campaigns {
			l := addLoss(c, fmt.Sprintf("%s %q removed", kind, name))
			if l == nil {
				continue
			}
			l.removedFilter[*r.ResourceID] = true
			if allowed {
				l.removedAllow[*r.ResourceID] = true
			}
			if _, err := c.DenyPageID.Get(); err == nil {
				addLoss(c, "deny page cleared")
			}
		}
	case "page", "webhook", "script":
		get, what := repo.GetByDenyPageID, "deny page"
		if r.ResourceType == "webhook" {
			get, what = repo.GetByWebhookID, "webhook"
		} else if r.ResourceType == "script" {
			get, what = repo.GetByScriptID, "script"
		}
		campaigns, err := get(ctx, r.ResourceID)
		if err != nil {
			return err
		}
		name := s.resourceName(ctx, r)
		for _, c := range campaigns {
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

// collectRemovedMembers appends to out each referenced package that removing
// bundleID would also remove, using the same rule as remove: no other bundle
// links it and it was installed only as a dependency. It recurses the same way
// remove does.
func (s *ContentRegistry) collectRemovedMembers(
	ctx context.Context, bundleID *uuid.UUID, seen map[uuid.UUID]bool, out *[]*database.InstalledPackage,
) error {
	members, err := s.RegistryRepository.GetPackageMembers(ctx, bundleID)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, m := range members {
		if m == nil || seen[*m] {
			continue
		}
		n, err := s.RegistryRepository.CountBundlesForMember(ctx, m, bundleID)
		if err != nil {
			return errs.Wrap(err)
		}
		if n > 0 {
			continue
		}
		mp, err := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if !mp.InstalledAsDependency {
			continue
		}
		seen[*m] = true
		*out = append(*out, mp)
		if err := s.collectRemovedMembers(ctx, m, seen, out); err != nil {
			return err
		}
	}
	return nil
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

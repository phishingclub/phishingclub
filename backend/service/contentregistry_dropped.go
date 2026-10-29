package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// memberConflictType is the conflict type of a referenced package a bundle
// update drops while the user edited or uses its content.
const memberConflictType = "member"

// resolveRemove uninstalls a dropped referenced package. The other choice for
// it is resolveKeep, which unlinks it and keeps it installed on its own.
const resolveRemove = "remove"

// memberKey is the resolution key of a dropped referenced package.
func memberKey(packageID string) string { return memberConflictType + ":" + packageID }

// DroppedMemberImpact is what uninstalling a referenced package that a new
// bundle version drops would affect. The preview fields cover the package and
// the packages removed with it. Templates names campaign templates outside the
// bundle that use its content, and TemplateCount holds their full number.
type DroppedMemberImpact struct {
	UninstallPreview
	Templates     []string `json:"templates"`
	TemplateCount int      `json:"templateCount"`
}

// inUse reports whether removing the package would lose an edit or affect
// content used outside the bundle.
func (d *DroppedMemberImpact) inUse() bool {
	return len(d.Edited) > 0 || d.TemplateCount > 0 || d.CampaignCount > 0 ||
		d.KeepRunningCount > 0 || d.ChangedCount > 0 || d.UserAssetCount > 0
}

// templatesOf returns the campaign templates the given packages track.
func (s *ContentRegistry) templatesOf(ctx context.Context, ids []*uuid.UUID, out map[uuid.UUID]bool) error {
	for _, id := range ids {
		if id == nil {
			continue
		}
		rows, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, id)
		if err != nil {
			return errs.Wrap(err)
		}
		for _, r := range rows {
			if r.ResourceType == "campaigntemplate" && r.ResourceID != nil {
				out[*r.ResourceID] = true
			}
		}
	}
	return nil
}

// droppedMemberConflicts returns a conflict for each referenced package the
// update would uninstall (no other bundle links it and it was installed only as
// a dependency) whose content the user edited or uses outside the bundle. A
// package with neither is left out and is removed without asking. fetch reads
// the installed version of a package, for its declared smtp headers.
func (s *ContentRegistry) droppedMemberConflicts(
	ctx context.Context,
	bundle *database.InstalledPackage,
	dropped []*uuid.UUID,
	fetch func(id string) (*registry.VerifiedPackage, error),
) ([]UpdateConflict, error) {
	if len(dropped) == 0 {
		return nil, nil
	}
	// the bundle's own templates are rewired or removed by this update, so their
	// campaigns are not counted against a dropped package.
	own := map[uuid.UUID]bool{}
	if err := s.templatesOf(ctx, []*uuid.UUID{bundle.ID}, own); err != nil {
		return nil, err
	}
	// templates of the bundle and of every package it links are not outside use.
	owned := map[uuid.UUID]bool{}
	for id := range own {
		owned[id] = true
	}
	members, err := s.RegistryRepository.GetPackageMembers(ctx, bundle.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.templatesOf(ctx, members, owned); err != nil {
		return nil, err
	}
	declaredFor := func(p *database.InstalledPackage) func(string) ([]*model.SMTPHeader, bool) {
		return s.boundSMTPHeaders(ctx, p, fetch)
	}

	var out []UpdateConflict
	for _, m := range dropped {
		if m == nil {
			continue
		}
		mp, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			continue
		}
		if gerr != nil {
			return nil, errs.Wrap(gerr)
		}
		n, cerr := s.RegistryRepository.CountBundlesForMember(ctx, m, bundle.ID)
		if cerr != nil {
			return nil, errs.Wrap(cerr)
		}
		if n != 0 || !mp.InstalledAsDependency {
			continue
		}
		pkgs := []*database.InstalledPackage{mp}
		seen := map[uuid.UUID]bool{*bundle.ID: true, *m: true}
		if err := s.collectRemovedMembers(ctx, m, seen, &pkgs); err != nil {
			return nil, err
		}
		removedOwned := map[uuid.UUID]bool{}
		for _, p := range pkgs {
			if err := s.templatesOf(ctx, []*uuid.UUID{p.ID}, removedOwned); err != nil {
				return nil, err
			}
		}
		preview, closed, perr := s.previewRemoval(ctx, pkgs, declaredFor, own)
		if perr != nil {
			return nil, perr
		}
		impact := &DroppedMemberImpact{UninstallPreview: *preview, Templates: []string{}}
		for _, tid := range closed {
			if owned[tid] || removedOwned[tid] {
				continue
			}
			impact.TemplateCount++
			if len(impact.Templates) >= uninstallPreviewCampaignCap {
				continue
			}
			impact.Templates = append(impact.Templates, s.campaignTemplateName(ctx, tid))
		}
		if !impact.inUse() {
			continue
		}
		out = append(out, UpdateConflict{
			ResourceType: memberConflictType,
			SourceName:   mp.PackageID,
			CurrentName:  mp.PackageID,
			Member:       impact,
		})
	}
	return out, nil
}

// campaignTemplateName returns the name of a campaign template, or its id when
// it cannot be read.
func (s *ContentRegistry) campaignTemplateName(ctx context.Context, id uuid.UUID) string {
	if s.CampaignTemplateRepository != nil {
		if ct, err := s.CampaignTemplateRepository.GetByID(ctx, &id, &repository.CampaignTemplateOption{}); err == nil {
			if n := optStr(ct.Name); n != "" {
				return n
			}
		}
	}
	return id.String()
}

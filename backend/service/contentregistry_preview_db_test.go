package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// installLoginPage installs a one page package from a fake registry and returns
// its installed package row and the page id.
func installLoginPage(t *testing.T, h *ownershipFixture) (*database.InstalledPackage, *uuid.UUID) {
	t.Helper()
	if err := h.db.AutoMigrate(&database.InstalledPackageMember{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h.s.CampaignTemplateRepository = &repository.CampaignTemplate{DB: h.db}
	h.s.CampaignTemplate = h.s.Page.CampaignTemplateService
	srv := fakeRegistry(t, map[string]fakeArchive{
		"acme/login": buildFakeArchive(t, "acme/login", "page", nil, map[string]string{
			"content/login/data.yaml":  "pages:\n  - name: Login\n    file: index.html\n",
			"content/login/index.html": "<html>login</html>",
		}),
	})
	regID, err := h.s.RegistryRepository.InsertRegistry(
		h.ctx, &database.Registry{Name: "fake", URL: srv.URL, InsecureTLS: true},
	)
	if err != nil {
		t.Fatalf("insert registry: %v", err)
	}
	if _, err := h.s.Install(h.ctx, h.session, regID, "acme", "login", nil, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	ip, err := h.s.RegistryRepository.GetInstalledPackage(h.ctx, regID, "acme/login")
	if err != nil {
		t.Fatalf("installed row: %v", err)
	}
	pageID, ok := h.pageIDByName("Login")
	if !ok {
		t.Fatalf("page not installed")
	}
	return ip, pageID
}

// insertCampaign adds a campaign row and returns its id.
func insertCampaign(
	t *testing.T,
	h *ownershipFixture,
	name string,
	templateID, denyPageID *uuid.UUID,
	closeAt *time.Time,
) uuid.UUID {
	t.Helper()
	now := time.Now()
	id := uuid.New()
	row := &database.Campaign{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: name,
		CampaignTemplateID: templateID, DenyPageID: denyPageID, CloseAt: closeAt,
	}
	if err := h.db.Create(row).Error; err != nil {
		t.Fatalf("insert campaign: %v", err)
	}
	return id
}

func campaignRow(t *testing.T, h *ownershipFixture, id uuid.UUID) database.Campaign {
	t.Helper()
	var row database.Campaign
	if err := h.db.Where("id = ?", id.String()).First(&row).Error; err != nil {
		t.Fatalf("campaign %s: %v", id, err)
	}
	return row
}

// TestUninstallPreviewMatchesRemove checks the preview splits campaigns the way
// the delete paths treat them. While an open campaign uses the page as its deny
// page, the page delete is refused: the preview lists the page as blocked and
// nothing as closed or changed, and remove keeps the page and changes no
// campaign. Once that campaign is closed, a campaign without a close time and
// one that has a later close time are both closed now, and the closed campaign
// loses its deny page. Remove must then close exactly the campaigns
// the preview listed as closed.
func TestUninstallPreviewMatchesRemove(t *testing.T) {
	h := newOwnershipFixture(t)
	ip, pageID := installLoginPage(t, h)

	now := time.Now()
	tplID := uuid.New()
	identID := uuid.New()
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &tplID, CreatedAt: &now, UpdatedAt: &now, Name: "Tpl", LandingPageID: pageID,
		URLIdentifierID: &identID, StateIdentifierID: &identID,
	}).Error; err != nil {
		t.Fatalf("insert template: %v", err)
	}
	future := now.Add(48 * time.Hour).UTC().Truncate(time.Second)
	openID := insertCampaign(t, h, "Open", &tplID, nil, nil)
	scheduledID := insertCampaign(t, h, "Scheduled", &tplID, nil, &future)
	otherTpl := uuid.New()
	denyOnlyID := insertCampaign(t, h, "DenyOnly", &otherTpl, pageID, nil)

	// the open campaign DenyOnly holds the page
	preview, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Blocked) != 1 || preview.Blocked[0].Name != "Login" ||
		preview.Blocked[0].ResourceType != "page" || preview.Blocked[0].UsedByCount != 1 ||
		len(preview.Blocked[0].UsedBy) != 1 || preview.Blocked[0].UsedBy[0] != "campaign DenyOnly" {
		t.Fatalf("expected the Login page blocked by campaign DenyOnly, got %+v", preview.Blocked)
	}
	if preview.CampaignCount != 0 || preview.ChangedCount != 0 {
		t.Fatalf("a page that stays changes no campaign, got closed %+v, changed %+v",
			preview.Campaigns, preview.Changed)
	}
	err = h.s.Remove(h.ctx, h.session, ip.ID)
	if err == nil || !strings.Contains(err.Error(), "stay installed") || !strings.Contains(err.Error(), "DenyOnly") {
		t.Fatalf("expected remove to keep the page used by DenyOnly, got %v", err)
	}
	if _, ok := h.pageIDByName("Login"); !ok {
		t.Fatalf("the Login page should stay installed")
	}
	if _, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, ip.ID); err != nil {
		t.Fatalf("the package should stay installed: %v", err)
	}
	if row := campaignRow(t, h, denyOnlyID); row.DenyPageID == nil || *row.DenyPageID != *pageID {
		t.Fatalf("DenyOnly should keep its deny page, got %v", row.DenyPageID)
	}
	if row := campaignRow(t, h, openID); row.CloseAt != nil {
		t.Fatalf("Open should not be closed while the page stays, got %v", row.CloseAt)
	}

	// once DenyOnly is closed the page goes and DenyOnly loses it
	if err := h.db.Model(&database.Campaign{}).Where("id = ?", denyOnlyID.String()).
		Update("closed_at", now).Error; err != nil {
		t.Fatalf("close DenyOnly: %v", err)
	}
	preview, err = h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Blocked) != 0 {
		t.Fatalf("a closed campaign does not hold the page, got %+v", preview.Blocked)
	}
	// a campaign with a later close time is closed now too
	if preview.CampaignCount != 2 || len(preview.Campaigns) != 2 {
		t.Fatalf("expected Open and Scheduled to be closed, got %+v", preview.Campaigns)
	}
	if preview.ChangedCount != 0 {
		t.Fatalf("a closed campaign is not listed as changed, got %+v", preview.Changed)
	}

	if err := h.s.Remove(h.ctx, h.session, ip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := h.pageIDByName("Login"); ok {
		t.Fatalf("the Login page should be removed")
	}
	var closed []string
	for _, id := range []uuid.UUID{openID, scheduledID, denyOnlyID} {
		row := campaignRow(t, h, id)
		if row.CloseAt == nil {
			continue
		}
		// a close time set by the remove is not far in the future
		if row.CloseAt.Before(now.Add(time.Hour)) {
			closed = append(closed, row.Name)
		}
	}
	var previewClosed []string
	for _, c := range preview.Campaigns {
		previewClosed = append(previewClosed, c.Name)
	}
	sort.Strings(closed)
	sort.Strings(previewClosed)
	if strings.Join(closed, ",") != strings.Join(previewClosed, ",") {
		t.Fatalf("remove closed %v but the preview listed %v", closed, previewClosed)
	}
	if row := campaignRow(t, h, scheduledID); row.CloseAt == nil || !row.CloseAt.Before(future) {
		t.Fatalf("the later close time of Scheduled should be brought forward, got %v", row.CloseAt)
	}
	if row := campaignRow(t, h, denyOnlyID); row.DenyPageID != nil {
		t.Fatalf("the closed DenyOnly should have lost its deny page")
	}
}

// TestUninstallPreviewBlockedMemberKeepsItsMembers checks a bundle whose
// referenced package has a page an open campaign filters with: remove keeps
// that package and so the package it references, and the preview lists only
// the packages remove gets to. Uninstalling the blocked package itself takes
// none of its members.
func TestUninstallPreviewBlockedMemberKeepsItsMembers(t *testing.T) {
	h := newOwnershipFixture(t)
	member, pageID := installLoginPage(t, h)
	bundleID := h.newInstall("acme/bundle")
	subID := h.newInstall("acme/sub")
	for _, id := range []*uuid.UUID{member.ID, subID} {
		if err := h.s.RegistryRepository.SetInstalledAsDependency(h.ctx, id, true); err != nil {
			t.Fatalf("mark dependency: %v", err)
		}
	}
	if err := h.s.RegistryRepository.InsertPackageMember(h.ctx, bundleID, member.ID); err != nil {
		t.Fatalf("link member: %v", err)
	}
	if err := h.s.RegistryRepository.InsertPackageMember(h.ctx, member.ID, subID); err != nil {
		t.Fatalf("link sub member: %v", err)
	}
	otherTpl := uuid.New()
	insertCampaign(t, h, "DenyOnly", &otherTpl, pageID, nil)

	preview, err := h.s.UninstallPreview(h.ctx, h.session, bundleID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if strings.Join(preview.Members, ",") != "acme/login" {
		t.Fatalf("expected only acme/login to be reached, got %v", preview.Members)
	}
	if len(preview.Blocked) != 1 || preview.Blocked[0].PackageID != "acme/login" {
		t.Fatalf("expected the Login page of acme/login blocked, got %+v", preview.Blocked)
	}
	preview, err = h.s.UninstallPreview(h.ctx, h.session, member.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Members) != 0 || len(preview.Blocked) != 1 {
		t.Fatalf("a blocked package takes no members, got members %v blocked %+v", preview.Members, preview.Blocked)
	}

	if err := h.s.Remove(h.ctx, h.session, bundleID); err == nil {
		t.Fatalf("expected remove to be incomplete")
	}
	for _, id := range []*uuid.UUID{member.ID, subID} {
		if _, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, id); err != nil {
			t.Fatalf("package %s should stay installed: %v", id, err)
		}
	}
}

// TestUninstallPreviewFlagsRemovedAllowList checks a campaign whose only allow
// list the uninstall deletes is flagged as reachable by everyone, while one that
// keeps another allow list is listed without that flag.
func TestUninstallPreviewFlagsRemovedAllowList(t *testing.T) {
	h := newOwnershipFixture(t)
	if err := h.db.AutoMigrate(&database.InstalledPackageMember{}, &database.CampaignAllowDeny{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h.regFixture.withAllowDenyService()
	h.s.CampaignTemplateRepository = &repository.CampaignTemplate{DB: h.db}
	h.s.CampaignTemplate = h.s.Page.CampaignTemplateService

	ipID := h.newInstall("acme/filter")
	officeID, err := h.s.createAllowDeny(h.ctx, h.session, "Office", true, "10.0.0.0/8", "", "", "", "")
	if err != nil {
		t.Fatalf("create allow list: %v", err)
	}
	if err := h.s.recordResource(
		h.ctx, ipID, "allowdeny", "Office", officeID, h.s.storedAllowDenyHash(h.ctx, officeID), "{}",
	); err != nil {
		t.Fatalf("record resource: %v", err)
	}
	userListID, err := h.s.createAllowDeny(h.ctx, h.session, "User list", true, "192.168.0.0/16", "", "", "", "")
	if err != nil {
		t.Fatalf("create user allow list: %v", err)
	}

	tpl := uuid.New()
	onlyID := insertCampaign(t, h, "OnlyOffice", &tpl, nil, nil)
	bothID := insertCampaign(t, h, "Both", &tpl, nil, nil)
	for _, link := range []struct{ c, ad *uuid.UUID }{{&onlyID, officeID}, {&bothID, officeID}, {&bothID, userListID}} {
		if err := h.db.Create(&database.CampaignAllowDeny{CampaignID: link.c, AllowDenyID: link.ad}).Error; err != nil {
			t.Fatalf("link allow list: %v", err)
		}
	}

	preview, err := h.s.UninstallPreview(h.ctx, h.session, ipID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.CampaignCount != 0 {
		t.Fatalf("an allow list delete closes no campaign, got %+v", preview.Campaigns)
	}
	if preview.ChangedCount != 2 {
		t.Fatalf("expected both campaigns to be changed, got %+v", preview.Changed)
	}
	for _, c := range preview.Changed {
		switch c.Name {
		case "OnlyOffice":
			if !c.OpenToEveryone || !strings.Contains(c.Losses[0], "reachable by everyone") {
				t.Fatalf("OnlyOffice should be flagged as reachable by everyone, got %+v", c)
			}
		case "Both":
			if c.OpenToEveryone {
				t.Fatalf("Both keeps another allow list and should not be flagged, got %+v", c)
			}
		default:
			t.Fatalf("unexpected campaign %q", c.Name)
		}
		if !strings.Contains(strings.Join(c.Losses, ";"), `allow list "Office" removed`) {
			t.Fatalf("expected the allow list loss for %s, got %v", c.Name, c.Losses)
		}
	}
}

// TestRollbackInstallRunsOnCancelledContext checks a rollback still removes
// everything when the request context has already ended.
func TestRollbackInstallRunsOnCancelledContext(t *testing.T) {
	h := newOwnershipFixture(t)
	ip, _ := installLoginPage(t, h)

	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if !h.s.rollbackInstall(ctx, h.session, ip.ID, false) {
		t.Fatalf("rollback on a cancelled context did not complete")
	}
	if _, ok := h.pageIDByName("Login"); ok {
		t.Fatalf("the installed page should be removed")
	}
	if _, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, ip.ID); err == nil {
		t.Fatalf("the installed package row should be removed")
	}
}

// TestLockWaitStopsWithContext checks a waiting uninstall gives up with a clear
// error when its request ends while another operation holds the lock.
func TestLockWaitStopsWithContext(t *testing.T) {
	h := newOwnershipFixture(t)
	unlock, err := h.s.lockOps(h.ctx)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(h.ctx, 50*time.Millisecond)
	defer cancel()
	id := uuid.New()
	if err := h.s.Remove(ctx, h.session, &id); !errors.Is(err, ErrRegistryBusy) {
		t.Fatalf("expected ErrRegistryBusy, got %v", err)
	}
}

// TestRemoveUnauthorizedDoesNotWaitOnLock checks a caller without access is
// refused at once, even while another operation holds the lock.
func TestRemoveUnauthorizedDoesNotWaitOnLock(t *testing.T) {
	h := newOwnershipFixture(t)
	unlock, err := h.s.lockOps(h.ctx)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer unlock()
	session := &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nn(uuid.New()),
			Role: &model.Role{ID: uuid.New(), Name: data.RoleCompanyUser},
		},
	}
	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()
	id := uuid.New()
	start := time.Now()
	if err := h.s.Remove(ctx, session, &id); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected ErrAuthorizationFailed, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("unauthorized remove waited on the lock")
	}
}

// TestMemberVarsForCacheMiss checks a member missing from the offline cache
// fails with the same error as every other cache miss.
func TestMemberVarsForCacheMiss(t *testing.T) {
	h := newOwnershipFixture(t)
	id := h.newInstall("acme/member")
	cache := newPackageCache(nil)
	cache.offline = true
	_, err := h.s.memberVarsFor(h.ctx, cache.get, []*uuid.UUID{id})
	if !errors.As(err, &errs.ValidationError{}) || err.Error() != errNotDownloaded.Error() {
		t.Fatalf("expected errNotDownloaded, got %v", err)
	}
}

// TestUpdateAfterConcurrentUninstall checks an update whose package is
// uninstalled while it downloads returns ErrUninstalledDuringUpdate.
func TestUpdateAfterConcurrentUninstall(t *testing.T) {
	h := newOwnershipFixture(t)
	ip, _ := installLoginPage(t, h)

	unlock, err := h.s.lockOps(h.ctx)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := h.s.Update(h.ctx, h.session, ip.ID, nil, nil, nil)
		done <- err
	}()
	// give the update time to download and start waiting on the lock.
	time.Sleep(time.Second)
	if err := h.s.RegistryRepository.DeleteInstalledPackage(h.ctx, ip.ID); err != nil {
		unlock()
		t.Fatalf("delete installed package: %v", err)
	}
	unlock()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUninstalledDuringUpdate) {
			t.Fatalf("expected ErrUninstalledDuringUpdate, got %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("update did not return")
	}
}

// createAllowDeny creates an allow deny list the way an install would, for tests
// that need one owned by a package.
func (s *ContentRegistry) createAllowDeny(
	ctx context.Context,
	session *model.Session,
	name string,
	allowed bool,
	cidrs, countryCodes, asns, ja4, headersJSON string,
) (*uuid.UUID, error) {
	m, err := buildAllowDenyModel(name, allowed, cidrs, countryCodes, asns, ja4, headersJSON)
	if err != nil {
		return nil, err
	}
	return s.AllowDeny.Create(ctx, session, m)
}

// storedAllowDenyHash is the content hash of a stored allow deny list.
func (s *ContentRegistry) storedAllowDenyHash(ctx context.Context, id *uuid.UUID) string {
	return s.storedHash(ctx, "allowdeny", id)
}

// TestUninstallPreviewDeletedTemplateClosesNow checks a campaign whose template
// the removal deletes is listed as closed, also when it already has a later close
// time, and that remove then closes it now
func TestUninstallPreviewDeletedTemplateClosesNow(t *testing.T) {
	h := newOwnershipFixture(t)
	if err := h.db.AutoMigrate(&database.InstalledPackageMember{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h.s.CampaignTemplateRepository = &repository.CampaignTemplate{DB: h.db}
	h.s.CampaignTemplate = h.s.Page.CampaignTemplateService

	now := time.Now()
	tplID := uuid.New()
	identID := uuid.New()
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &tplID, CreatedAt: &now, UpdatedAt: &now, Name: "Pkg tpl",
		URLIdentifierID: &identID, StateIdentifierID: &identID,
	}).Error; err != nil {
		t.Fatalf("insert template: %v", err)
	}
	ipID := h.newInstall("acme/tpl")
	if err := h.s.recordResource(h.ctx, ipID, "campaigntemplate", "Pkg tpl", &tplID, "x", "{}"); err != nil {
		t.Fatalf("record resource: %v", err)
	}
	future := now.Add(72 * time.Hour).UTC().Truncate(time.Second)
	laterID := insertCampaign(t, h, "Later", &tplID, nil, &future)

	preview, err := h.s.UninstallPreview(h.ctx, h.session, ipID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.CampaignCount != 1 || len(preview.Campaigns) != 1 || preview.Campaigns[0].Name != "Later" {
		t.Fatalf("expected Later to be listed as closed, got %+v", preview.Campaigns)
	}
	if err := h.s.Remove(h.ctx, h.session, ipID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if row := campaignRow(t, h, laterID); row.CloseAt == nil || !row.CloseAt.Before(now.Add(time.Hour)) {
		t.Fatalf("Later should close now, close at is %v", row.CloseAt)
	}
}

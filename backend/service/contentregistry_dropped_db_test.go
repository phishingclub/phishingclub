package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
)

// applyWith runs applyUpdate with resolutions.
func (h *regFixture) applyWith(
	ip *database.InstalledPackage,
	pkg *registry.VerifiedPackage,
	fetch func(string) (*registry.VerifiedPackage, error),
	resolutions map[string]string,
) (*InstallResult, *AuditEvent, error) {
	ae := NewAuditEvent("Registry.PackageUpdate", h.session)
	res, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetch, nil, resolutions, nil)
	return res, ae, err
}

// droppedFixture installs a bundle that references ns/a and ns/c, each with one
// page, and returns the bundle row and a new version that references only ns/b.
func droppedFixture(t *testing.T) (
	*regFixture,
	uuid.UUID,
	*database.InstalledPackage,
	*registry.VerifiedPackage,
	func(string) (*registry.VerifiedPackage, error),
) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/unused")
	regID := uuid.New()
	a := typedPackage("ns/a", "page", "1.0.0", pageYAML("A page", "a.html"), map[string]string{"a.html": "<p>a</p>"})
	c := typedPackage("ns/c", "page", "1.0.0", pageYAML("C page", "c.html"), map[string]string{"c.html": "<p>c</p>"})
	b := typedPackage("ns/b", "page", "1.0.0", pageYAML("B page", "b.html"), map[string]string{"b.html": "<p>b</p>"})
	v1 := typedPackage("ns/bundle", "bundle", "1.0.0", "", nil, "ns/a", "ns/c")
	bundle := h.install(&regID, "ns/bundle", nil, fetchFrom(v1, a, c))
	v2 := typedPackage("ns/bundle", "bundle", "2.0.0", "", nil, "ns/b")
	return h, regID, bundle, v2, fetchFrom(v2, b)
}

func memberConflictsOf(t *testing.T, err error) []UpdateConflict {
	t.Helper()
	var uc ErrUpdateConflicts
	if !errors.As(err, &uc) {
		t.Fatalf("expected ErrUpdateConflicts, got %v", err)
	}
	var out []UpdateConflict
	for _, c := range uc.Conflicts {
		if c.ResourceType == memberConflictType {
			out = append(out, c)
		}
	}
	return out
}

// TestUpdateDroppedMemberEditedConflict drops two members, one with an edited
// page. The update stops before any write with a conflict for that member only.
// Keep then unlinks it and leaves it installed on its own with the edit intact,
// while the unedited member is removed without asking.
func TestUpdateDroppedMemberEditedConflict(t *testing.T) {
	h, regID, bundle, v2, fetch := droppedFixture(t)
	aIP := h.installed(&regID, "ns/a")
	pageID, ok := h.pageIDByName("A page")
	if !ok {
		t.Fatal("member page not installed")
	}
	h.editPage(pageID, "<p>mine</p>")

	_, ae, err := h.applyWith(bundle, v2, fetch, nil)
	mc := memberConflictsOf(t, err)
	if len(mc) != 1 || mc[0].SourceName != "ns/a" || mc[0].Key() != "member:ns/a" {
		t.Fatalf("member conflicts = %+v", mc)
	}
	if mc[0].Member == nil || len(mc[0].Member.Edited) != 1 || mc[0].Member.Edited[0].ResourceType != "page" {
		t.Fatalf("conflict does not list the edited page: %+v", mc[0].Member)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("conflict should stop before any write")
	}
	if h.isInstalled(&regID, "ns/b") || !h.isInstalled(&regID, "ns/c") {
		t.Fatal("members changed by a stopped update")
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "1.0.0" {
		t.Fatalf("bundle version advanced to %s", got)
	}

	res, ae, err := h.applyWith(bundle, v2, fetch, map[string]string{"member:ns/a": "keep"})
	if err != nil {
		t.Fatalf("update with keep: %v", err)
	}
	if !h.isInstalled(&regID, "ns/a") {
		t.Fatal("kept member was removed")
	}
	if h.installed(&regID, "ns/a").InstalledAsDependency {
		t.Fatal("kept member still marked as a dependency")
	}
	if h.memberIDs(bundle.ID)[*aIP.ID] {
		t.Fatal("kept member still linked to the bundle")
	}
	if got := h.pageContent(pageID); got != "<p>mine</p>" {
		t.Fatalf("kept page content = %q", got)
	}
	if h.isInstalled(&regID, "ns/c") {
		t.Fatal("unedited dropped member not removed")
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "2.0.0" {
		t.Fatalf("bundle version = %s", got)
	}
	if kept, _ := ae.Details["membersKept"].([]string); len(kept) != 1 || kept[0] != "ns/a" {
		t.Fatalf("audit membersKept = %v", ae.Details["membersKept"])
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "ns/a unlinked") {
		t.Fatalf("result does not report the kept member: %v", res.Skipped)
	}
}

// TestUpdateDroppedMemberRemove resolves an edited dropped member with remove,
// which uninstalls it as before.
func TestUpdateDroppedMemberRemove(t *testing.T) {
	h, regID, bundle, v2, fetch := droppedFixture(t)
	pageID, _ := h.pageIDByName("A page")
	h.editPage(pageID, "<p>mine</p>")
	if _, _, err := h.applyWith(bundle, v2, fetch, map[string]string{"member:ns/a": "remove"}); err != nil {
		t.Fatalf("update with remove: %v", err)
	}
	if h.isInstalled(&regID, "ns/a") {
		t.Fatal("removed member still installed")
	}
	if _, ok := h.pageIDByName("A page"); ok {
		t.Fatal("removed member page left behind")
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "2.0.0" {
		t.Fatalf("bundle version = %s", got)
	}
}

// TestUpdateDroppedMemberInvalidResolution rejects any value but keep or remove
// for a dropped member before anything is written.
func TestUpdateDroppedMemberInvalidResolution(t *testing.T) {
	h, regID, bundle, v2, fetch := droppedFixture(t)
	pageID, _ := h.pageIDByName("A page")
	h.editPage(pageID, "<p>mine</p>")
	for _, v := range []string{"overwrite", "both", "", "bogus"} {
		_, _, err := h.applyWith(bundle, v2, fetch, map[string]string{"member:ns/a": v})
		if !isValidation(err) {
			t.Fatalf("resolution %q: expected a validation error, got %v", v, err)
		}
	}
	if !h.isInstalled(&regID, "ns/a") || !h.isInstalled(&regID, "ns/c") || h.isInstalled(&regID, "ns/b") {
		t.Fatal("members changed by a rejected update")
	}
}

// TestUpdateDroppedMemberInUse raises a conflict for a dropped member whose page
// a campaign template outside the bundle uses and an active campaign uses as
// its deny page, though nothing was edited.
func TestUpdateDroppedMemberInUse(t *testing.T) {
	h, regID, bundle, v2, fetch := droppedFixture(t)
	pageID, _ := h.pageIDByName("A page")
	now := time.Now()
	tid := uuid.New()
	urlID := uuid.New()
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &tid, CreatedAt: &now, UpdatedAt: &now, Name: "Mine", LandingPageID: pageID, URLIdentifierID: &urlID,
	}).Error; err != nil {
		t.Fatalf("template: %v", err)
	}
	cid := uuid.New()
	if err := h.db.Create(&database.Campaign{
		ID: &cid, CreatedAt: &now, UpdatedAt: &now, Name: "Live", DenyPageID: pageID,
	}).Error; err != nil {
		t.Fatalf("campaign: %v", err)
	}

	_, _, err := h.applyWith(bundle, v2, fetch, nil)
	mc := memberConflictsOf(t, err)
	if len(mc) != 1 || mc[0].SourceName != "ns/a" {
		t.Fatalf("member conflicts = %+v", mc)
	}
	m := mc[0].Member
	if len(m.Edited) != 0 || m.TemplateCount != 1 || m.Templates[0] != "Mine" || m.ChangedCount != 1 {
		t.Fatalf("impact = %+v", m)
	}
	if !h.isInstalled(&regID, "ns/a") {
		t.Fatal("member removed by a stopped update")
	}
}

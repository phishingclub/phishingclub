package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
)

// botsPackage is a second list package that declares the block list Bots.
func botsPackage(version string) *registry.VerifiedPackage {
	y := "allowDenies:\n  - name: Bots\n    allowed: false\n    cidrs: \"172.16.0.0/12\"\n"
	return typedPackage("ns/bots", "allowdeny", version, y, nil)
}

// depPagePackage is a page package whose asset a.js uses the lists named and
// that depends on the packages in deps.
func depPagePackage(version string, lists []string, deps ...string) *registry.VerifiedPackage {
	y := "assets:\n  - path: a.js\n    allowDeny: [" + strings.Join(lists, ", ") + "]\n"
	p := typedPackage("ns/page", "page", version, y, nil, deps...)
	p.Files["assets/a.js"] = []byte("a()")
	return p
}

// trackedListID returns the list the installed package tracks for the name.
func (h *ownershipFixture) trackedListID(regID *uuid.UUID, pkgID, name string) uuid.UUID {
	h.t.Helper()
	ip := h.installed(regID, pkgID)
	for _, r := range h.resourcesOf(ip.ID, "allowdeny") {
		if r.SourceName == name {
			return *r.ResourceID
		}
	}
	h.t.Fatalf("%s tracks no list %q", pkgID, name)
	return uuid.UUID{}
}

// assetListIDs returns the ids of the lists the asset at rel uses.
func (h *ownershipFixture) assetListIDs(rel string) map[uuid.UUID]bool {
	h.t.Helper()
	a, ok := h.assetAt(rel)
	if !ok {
		h.t.Fatalf("no asset at %s", rel)
	}
	lists, err := h.s.Asset.AssetRepository.GetAllowDenyByAssetID(h.ctx, a.id)
	if err != nil {
		h.t.Fatalf("asset lists: %v", err)
	}
	out := map[uuid.UUID]bool{}
	for _, l := range lists {
		out[l.ID.MustGet()] = true
	}
	return out
}

// renameList renames the list with the id, as the admin UI does.
func (h *ownershipFixture) renameList(id uuid.UUID, name string) {
	h.t.Helper()
	if err := h.db.Model(&database.AllowDeny{}).Where("id = ?", id.String()).Update("name", name).Error; err != nil {
		h.t.Fatalf("rename list: %v", err)
	}
}

// TestRenamedListResolvesOnInstall: a list from a list package that was
// renamed in Phishing Club still resolves for a page that depends on the
// package, through the tracked resource, and a user list that took the old
// name is never linked instead.
func TestRenamedListResolvesOnInstall(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", false)))
	tracked := h.trackedListID(regID, "ns/lists", "Scanners")
	h.renameList(tracked, "Scanners renamed")
	decoy := h.globalList("Scanners", false, nil)

	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	h.install(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.0.0", false)))
	ids := h.assetListIDs("a.js")
	if len(ids) != 1 || !ids[tracked] || ids[*decoy] {
		t.Fatalf("asset lists = %v, want only the tracked list %s", ids, tracked)
	}
	// the renamed list reads as the name the package gave it, so it is no
	// local edit.
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Scanners" {
		t.Fatalf("asset list names = %v", a.lists)
	}
}

// TestRenamedListUpdateAddsListPackage: after a list was renamed, an update of
// the page to a version that also depends on a second list package installs
// that package and links both lists, without a conflict.
func TestRenamedListUpdateAddsListPackage(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	v1 := depPagePackage("1.0.0", []string{"Scanners"}, "ns/lists")
	pip := h.install(regID, "ns/page", nil, fetchFrom(v1, listsPackage("1.0.0", false)))
	scanners := h.trackedListID(regID, "ns/lists", "Scanners")
	h.renameList(scanners, "Allow EUsc")

	v2 := depPagePackage("2.0.0", []string{"Scanners", "Bots"}, "ns/lists", "ns/bots")
	if _, _, err := h.applyWith(pip, v2, fetchFrom(v2, listsPackage("1.0.0", false), botsPackage("1.0.0")), nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !h.isInstalled(regID, "ns/bots") {
		t.Fatal("new list package not installed")
	}
	bots := h.trackedListID(regID, "ns/bots", "Bots")
	ids := h.assetListIDs("a.js")
	if len(ids) != 2 || !ids[scanners] || !ids[bots] {
		t.Fatalf("asset lists = %v", ids)
	}
	if got := h.installed(regID, "ns/page").Version; got != "2.0.0" {
		t.Fatalf("page version = %s", got)
	}
	// the asset matches its baseline, so the rename is no local edit.
	for _, r := range h.resourcesOf(pip.ID, "asset") {
		if got := h.s.storedHash(h.ctx, "asset", r.ResourceID); got != r.InstalledContentHash {
			t.Fatal("asset reads as edited after the rename")
		}
	}
}

// TestTrackedListKindChecked: the kind check reads the tracked list, not a
// list that has the declared name now.
func TestTrackedListKindChecked(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", false)))
	tracked := h.trackedListID(regID, "ns/lists", "Scanners")
	h.renameList(tracked, "Flipped")
	if err := h.db.Model(&database.AllowDeny{}).Where("id = ?", tracked.String()).Update("allowed", true).Error; err != nil {
		t.Fatalf("flip list: %v", err)
	}
	h.globalList("Scanners", false, nil)

	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	err := h.tryInstall(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.0.0", false)))
	want := `uses the list "Scanners" from ns/lists as a block list, but the installed list is an allow list`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
	if h.isInstalled(regID, "ns/page") || h.count(&database.Asset{}) != 0 {
		t.Fatal("refused install wrote something")
	}
}

// TestTrackedListDeletedRefused: a list the list package installed that was
// deleted since is refused with a clear message, even when a user list has
// its name.
func TestTrackedListDeletedRefused(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", false)))
	tracked := h.trackedListID(regID, "ns/lists", "Scanners")
	if err := h.db.Where("id = ?", tracked.String()).Delete(&database.AllowDeny{}).Error; err != nil {
		t.Fatalf("delete list: %v", err)
	}
	h.globalList("Scanners", false, nil)

	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	err := h.tryInstall(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.0.0", false)))
	want := `ns/page uses the list "Scanners" from ns/lists, but that list is no longer installed`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
	if h.count(&database.Asset{}) != 0 {
		t.Fatal("refused install wrote an asset")
	}
}

// diamondBundle is a bundle that references a page and the list package the
// page depends on.
func diamondBundle(version string, deps ...string) *registry.VerifiedPackage {
	return typedPackage("ns/kit", "bundle", version, "", nil, deps...)
}

// linkCampaignToList adds a running campaign that uses the list.
func (h *ownershipFixture) linkCampaignToList(name string, listID uuid.UUID) {
	h.t.Helper()
	tpl := uuid.New()
	cid := insertCampaign(h.t, h, name, &tpl, nil, nil)
	if err := h.db.Create(&database.CampaignAllowDeny{CampaignID: &cid, AllowDenyID: &listID}).Error; err != nil {
		h.t.Fatalf("link campaign: %v", err)
	}
}

// TestDiamondBundlePreview: a bundle that links a page and the list package
// the page also links removes the list package too, and the preview says so,
// with the campaign that loses the list.
func TestDiamondBundlePreview(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	kip := h.install(regID, "ns/kit", nil,
		fetchFrom(diamondBundle("1.0.0", "ns/lists", "ns/page"), page, listsPackage("1.0.0", false)))
	h.linkCampaignToList("Uses scanners", h.trackedListID(regID, "ns/lists", "Scanners"))

	prev, err := h.s.UninstallPreview(h.ctx, h.session, kip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	members := strings.Join(prev.Members, ",")
	if !strings.Contains(members, "ns/page") || !strings.Contains(members, "ns/lists") {
		t.Fatalf("members = %v", prev.Members)
	}
	if prev.ChangedCount != 1 || prev.Changed[0].Name != "Uses scanners" ||
		!strings.Contains(strings.Join(prev.Changed[0].Losses, ";"), `block list "Scanners" removed`) {
		t.Fatalf("changed = %+v", prev.Changed)
	}
	if err := h.s.Remove(h.ctx, h.session, kip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if h.isInstalled(regID, "ns/lists") || h.isInstalled(regID, "ns/page") {
		t.Fatal("remove kept a member the preview listed")
	}
}

// TestDiamondBundleUpdateConflict: an update that drops both the page and the
// list package asks about the list package a campaign uses, and keep leaves
// it installed on its own.
func TestDiamondBundleUpdateConflict(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	other := typedPackage("ns/other", "page", "1.0.0", pageYAML("Other", "o.html"), map[string]string{"o.html": "<p>o</p>"})
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	kip := h.install(regID, "ns/kit", nil,
		fetchFrom(diamondBundle("1.0.0", "ns/lists", "ns/page", "ns/other"), page, other, listsPackage("1.0.0", false)))
	h.linkCampaignToList("Uses scanners", h.trackedListID(regID, "ns/lists", "Scanners"))

	v2 := diamondBundle("2.0.0", "ns/other")
	_, _, err := h.applyWith(kip, v2, fetchFrom(v2, other), nil)
	mc := memberConflictsOf(t, err)
	if len(mc) != 1 || mc[0].SourceName != "ns/lists" || mc[0].Member == nil || mc[0].Member.ChangedCount != 1 {
		t.Fatalf("member conflicts = %+v", mc)
	}
	if _, _, err := h.applyWith(kip, v2, fetchFrom(v2, other), map[string]string{"member:ns/lists": resolveKeep}); err != nil {
		t.Fatalf("update with keep: %v", err)
	}
	if h.isInstalled(regID, "ns/page") {
		t.Fatal("dropped page still installed")
	}
	if !h.isInstalled(regID, "ns/lists") || h.installed(regID, "ns/lists").InstalledAsDependency {
		t.Fatal("kept list package not installed on its own")
	}
}

// TestBundleUpdateIgnoresUnusedOutdatedList: a bundle update is not blocked by
// a list package it keeps that is older than the latest when nothing the
// update writes uses its lists, and is when a new member uses them.
func TestBundleUpdateIgnoresUnusedOutdatedList(t *testing.T) {
	other := typedPackage("ns/other", "page", "1.0.0", pageYAML("Other", "o.html"), map[string]string{"o.html": "<p>o</p>"})
	t.Run("unused", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		kip := h.install(regID, "ns/kit", nil, fetchFrom(diamondBundle("1.0.0", "ns/lists"), listsPackage("1.0.0", false)))
		v2 := diamondBundle("2.0.0", "ns/lists", "ns/other")
		if _, _, err := h.applyWith(kip, v2, fetchFrom(v2, other, listsPackage("1.1.0", true)), nil); err != nil {
			t.Fatalf("update: %v", err)
		}
		if !h.isInstalled(regID, "ns/other") {
			t.Fatal("new member not installed")
		}
	})
	t.Run("used by a new member", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		kip := h.install(regID, "ns/kit", nil, fetchFrom(diamondBundle("1.0.0", "ns/lists"), listsPackage("1.0.0", false)))
		page := listPagePackage("ns/page", "1.0.0", "Office")
		v2 := diamondBundle("2.0.0", "ns/lists", "ns/page")
		_, _, err := h.applyWith(kip, v2, fetchFrom(v2, page, listsPackage("1.1.0", true)), nil)
		if err == nil || !strings.Contains(err.Error(), "Update ns/lists first") {
			t.Fatalf("err = %v", err)
		}
		if h.isInstalled(regID, "ns/page") {
			t.Fatal("refused update installed the page")
		}
	})
}

// TestRootPickNotOverriddenByMember: the asset of a bundle with its variables
// not shared gets the list its own pick chose, never the value of a member
// variable of the same name.
func TestRootPickNotOverriddenByMember(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.globalList("Picked", true, nil)
	open := "Open"
	member := typedPackage("ns/m", "page", "1.0.0", pageYAML("M", "m.html"), map[string]string{"m.html": "<p>m</p>"})
	member.Manifest.Variables = []registry.Variable{{Name: "L", Type: "text", Default: &open}}
	h.globalList("Open", false, nil)
	root := assetPackage("ns/kit", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: [\"@@L@@\"]\n",
		map[string]string{"a.js": "x"}, "ns/m")
	off := false
	root.Manifest.ShareVariables = &off
	root.Manifest.Variables = []registry.Variable{{Name: "L", Type: "text", Required: true, Pick: pickAllowDeny}}
	h.install(regID, "ns/kit", map[string]string{varKey("ns/kit", "L", false): "Picked"}, fetchFrom(root, member))
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Picked" {
		t.Fatalf("asset lists = %v, want the picked list", a.lists)
	}
}

// TestShareVariablesIgnoredOutsideBundle: a page that sets shareVariables to
// false still fills its variables by their bare names.
func TestShareVariablesIgnoredOutsideBundle(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.globalList("Picked", true, nil)
	p := assetPackage("ns/p", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: [\"@@L@@\"]\n", map[string]string{"a.js": "x"})
	off := false
	p.Manifest.ShareVariables = &off
	p.Manifest.Variables = []registry.Variable{{Name: "L", Type: "text", Required: true, Pick: pickAllowDeny}}
	ip := h.install(regID, "ns/p", map[string]string{"L": "Picked"}, fetchFrom(p))
	if ip.VariablesScoped {
		t.Fatal("page installed with scoped variables")
	}
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Picked" {
		t.Fatalf("asset lists = %v", a.lists)
	}
}

// TestDeleteKeepsListAnAssetUses: deleting a package list through the shared
// delete path, as an update undo or a rollback does, keeps a list an asset
// still uses.
func TestDeleteKeepsListAnAssetUses(t *testing.T) {
	h, _ := newAssetMetaFixture(t)
	listID := h.globalList("Office", true, nil)
	h.linkUserAsset("user/x.html", "Office")
	if err := h.s.releaseResourceEntity(h.ctx, h.session, "allowdeny", listID, nil); err != nil {
		t.Fatalf("release: %v", err)
	}
	if h.listCount("Office") != 1 {
		t.Fatal("a list an asset uses was deleted")
	}
	unused := h.globalList("Unused", true, nil)
	if err := h.s.deleteResourceEntity(h.ctx, h.session, "allowdeny", unused); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if h.listCount("Unused") != 0 {
		t.Fatal("an unused list was kept")
	}
}

// TestAssetListEntryRefused: an asset list entry that is a variable but not a
// list pick, or that names no list, is refused, never dropped.
func TestAssetListEntryRefused(t *testing.T) {
	cases := map[string]struct {
		entries string
		want    string
	}{
		"plain variable": {`["@@SITE@@"]`, "@@SITE@@ is not a variable that lets the installer pick"},
		"unknown token":  {`["@@NOPE@@"]`, "@@NOPE@@ is not a variable that lets the installer pick"},
		"blank":          {`[" "]`, "an allow/deny list entry is empty"},
		"empty value":    {`["@@EMPTY@@x"]`, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, regID := newAssetMetaFixture(t)
			h.globalList("x", false, nil)
			p := assetPackage("ns/p", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: "+c.entries+"\n",
				map[string]string{"a.js": "x"})
			empty := ""
			p.Manifest.Variables = []registry.Variable{
				{Name: "SITE", Type: "text", Required: true},
				{Name: "EMPTY", Type: "text", Default: &empty},
			}
			err := h.tryInstall(regID, "ns/p", map[string]string{"SITE": "Office"}, fetchFrom(p))
			if c.want == "" {
				// a value that only fills part of a name keeps the rest.
				if err != nil {
					t.Fatalf("install: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if h.count(&database.Asset{}) != 0 {
				t.Fatal("refused install wrote an asset")
			}
		})
	}
}

package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
)

// listsPackage is an allow deny package that declares a block list Scanners,
// and an allow list Office when withOffice is set.
func listsPackage(version string, withOffice bool) *registry.VerifiedPackage {
	y := "allowDenies:\n  - name: Scanners\n    allowed: false\n    cidrs: \"10.0.0.0/8\"\n"
	if withOffice {
		y += "  - name: Office\n    allowed: true\n    cidrs: \"192.168.0.0/16\"\n"
	}
	return typedPackage("ns/lists", "allowdeny", version, y, nil)
}

// listPagePackage is a page package whose asset a.js uses the lists named,
// depending on ns/lists.
func listPagePackage(id, version string, lists ...string) *registry.VerifiedPackage {
	y := "assets:\n  - path: a.js\n    allowDeny: [" + strings.Join(lists, ", ") + "]\n"
	p := typedPackage(id, "page", version, y, nil, "ns/lists")
	p.Files["assets/a.js"] = []byte("a()")
	return p
}

// linkUserAsset adds a user asset that uses the list name.
func (h *ownershipFixture) linkUserAsset(rel, list string) *uuid.UUID {
	h.t.Helper()
	id := h.userAsset(rel, "<p>user</p>")
	lid := h.allowDenyByName(list).ID.MustGet()
	if err := h.s.Asset.AssetRepository.ReplaceAllowDeny(h.ctx, id, []*uuid.UUID{&lid}); err != nil {
		h.t.Fatalf("link user asset: %v", err)
	}
	return id
}

func (h *ownershipFixture) listCount(name string) int64 {
	h.t.Helper()
	var n int64
	if err := h.db.Model(&database.AllowDeny{}).Where("name = ?", name).Count(&n).Error; err != nil {
		h.t.Fatalf("count lists: %v", err)
	}
	return n
}

// TestListPackageInUseRefused: a list package a page package depends on can
// not be uninstalled on its own, and the preview names the page. Uninstalling
// the page then removes both.
func TestListPackageInUseRefused(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	pip := h.install(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.0.0", false)))
	lip := h.installed(regID, "ns/lists")
	if !lip.InstalledAsDependency || !h.memberIDs(pip.ID)[*lip.ID] {
		t.Fatal("list package not installed as a member of the page")
	}
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Scanners" {
		t.Fatalf("asset lists = %v", a.lists)
	}
	prev, err := h.s.UninstallPreview(h.ctx, h.session, lip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if strings.Join(prev.UsedBy, ",") != "ns/page" {
		t.Fatalf("usedBy = %v", prev.UsedBy)
	}
	err = h.s.Remove(h.ctx, h.session, lip.ID)
	if err == nil || !strings.Contains(err.Error(), "ns/page uses this package; uninstall or update it first") {
		t.Fatalf("err = %v", err)
	}
	if h.listCount("Scanners") != 1 || !h.isInstalled(regID, "ns/lists") {
		t.Fatal("refused uninstall changed something")
	}
	if err := h.s.Remove(h.ctx, h.session, pip.ID); err != nil {
		t.Fatalf("remove page: %v", err)
	}
	if h.isInstalled(regID, "ns/lists") || h.count(&database.AllowDeny{}) != 0 || h.count(&database.Asset{}) != 0 {
		t.Fatal("page uninstall left the list package, a list or the asset")
	}
	if n := h.count(&database.InstalledPackageMember{}); n != 0 {
		t.Fatalf("%d member rows left", n)
	}
}

// TestListKeptWhileAssetUses: uninstalling a page keeps a list of its list
// package that a user asset still uses, untracked, and the preview says so
// instead of reporting the asset as open to everyone.
func TestListKeptWhileAssetUses(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	pip := h.install(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.0.0", false)))
	userID := h.linkUserAsset("user/x.html", "Scanners")

	prev, err := h.s.UninstallPreview(h.ctx, h.session, pip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.ChangedAssetCount != 0 {
		t.Fatalf("changed assets = %+v", prev.ChangedAssets)
	}
	if len(prev.KeptLists) != 1 || prev.KeptLists[0].Name != "Scanners" || prev.KeptLists[0].Allowed ||
		strings.Join(prev.KeptLists[0].Assets, ",") != "user/x.html" {
		t.Fatalf("kept lists = %+v", prev.KeptLists)
	}
	if strings.Join(prev.Members, ",") != "ns/lists" {
		t.Fatalf("members = %v", prev.Members)
	}
	if err := h.s.Remove(h.ctx, h.session, pip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if h.isInstalled(regID, "ns/lists") {
		t.Fatal("list package still installed")
	}
	if h.listCount("Scanners") != 1 {
		t.Fatal("list used by a user asset was deleted")
	}
	lists, err := h.s.Asset.AssetRepository.GetAllowDenyByAssetID(h.ctx, userID)
	if err != nil || len(lists) != 1 {
		t.Fatalf("user asset lists = %v, %v", lists, err)
	}
	if n := h.count(&database.InstalledResource{}); n != 0 {
		t.Fatalf("%d provenance rows left", n)
	}
}

// TestStandaloneListPackageKeptList: a list package installed on its own keeps
// a list a user asset uses when it is uninstalled.
func TestStandaloneListPackageKeptList(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	lip := h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", true)))
	h.linkUserAsset("user/x.html", "Office")
	prev, err := h.s.UninstallPreview(h.ctx, h.session, lip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.ChangedAssetCount != 0 || len(prev.KeptLists) != 1 || prev.KeptLists[0].Name != "Office" {
		t.Fatalf("preview = %+v", prev)
	}
	if err := h.s.Remove(h.ctx, h.session, lip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if h.listCount("Office") != 1 || h.listCount("Scanners") != 0 {
		t.Fatal("want Office kept and Scanners removed")
	}
}

// TestUpdateDropsListDependency: an update that stops depending on the list
// package removes it, but keeps a list a user asset still uses.
func TestUpdateDropsListDependency(t *testing.T) {
	overwrite := map[string]string{"asset:a.js": resolveOverwrite}
	v2 := func() *registry.VerifiedPackage {
		p := typedPackage("ns/page", "page", "2.0.0", "assets:\n  - path: a.js\n", nil)
		p.Files["assets/a.js"] = []byte("a()")
		return p
	}
	for _, userLinked := range []bool{false, true} {
		h, regID := newAssetMetaFixture(t)
		pip := h.install(regID, "ns/page", nil,
			fetchFrom(listPagePackage("ns/page", "1.0.0", "Scanners"), listsPackage("1.0.0", false)))
		if userLinked {
			h.linkUserAsset("user/x.html", "Scanners")
		}
		next := v2()
		if _, _, err := h.applyWith(pip, next, fetchFrom(next), overwrite); err != nil {
			t.Fatalf("update: %v", err)
		}
		if h.isInstalled(regID, "ns/lists") {
			t.Fatal("dropped list package still installed")
		}
		want := int64(0)
		if userLinked {
			want = 1
		}
		if got := h.listCount("Scanners"); got != want {
			t.Fatalf("user linked %v: %d Scanners lists, want %d", userLinked, got, want)
		}
	}
}

// TestReusedOlderListRefused: a list package installed at an older version
// than the one fetched is not reused silently; the install and the update
// name the package to update first.
func TestReusedOlderListRefused(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", false)))
		page := listPagePackage("ns/page", "1.0.0", "Office")
		err := h.tryInstall(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.1.0", true)))
		want := "ns/page needs ns/lists version 1.1.0, but version 1.0.0 is installed. Update ns/lists first"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v", err)
		}
		if h.isInstalled(regID, "ns/page") || h.count(&database.Asset{}) != 0 {
			t.Fatal("refused install wrote something")
		}
	})
	t.Run("update", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		pip := h.install(regID, "ns/page", nil,
			fetchFrom(listPagePackage("ns/page", "1.0.0", "Scanners"), listsPackage("1.0.0", false)))
		v2 := listPagePackage("ns/page", "2.0.0", "Office")
		_, _, err := h.applyWith(pip, v2, fetchFrom(v2, listsPackage("1.1.0", true)), nil)
		if err == nil || !strings.Contains(err.Error(), "Update ns/lists first") {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestReusedListKindMismatchRefused: a reused list whose installed kind is not
// the kind its package declares is refused before anything is written.
func TestReusedListKindMismatchRefused(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", false)))
	if err := h.db.Model(&database.AllowDeny{}).Where("name = ?", "Scanners").Update("allowed", true).Error; err != nil {
		t.Fatalf("flip list: %v", err)
	}
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	err := h.tryInstall(regID, "ns/page", nil, fetchFrom(page, listsPackage("1.0.0", false)))
	want := `uses the list "Scanners" from ns/lists as a block list, but the installed list is an allow list`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
	if h.isInstalled(regID, "ns/page") {
		t.Fatal("page installed")
	}
}

// TestListPicks: a list pick takes a global list of either kind, must be
// required and must be given.
func TestListPicks(t *testing.T) {
	pkg := func(required bool) *registry.VerifiedPackage {
		p := assetPackage("ns/p", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: [\"@@L@@\"]\n", map[string]string{"a.js": "x"})
		p.Manifest.Variables = []registry.Variable{{Name: "L", Type: "text", Required: required, Pick: pickAllowDeny}}
		return p
	}
	cases := []struct {
		name, value string
		required    bool
		want        string
	}{
		{"allow list", "Office", true, ""},
		{"block list", "Bad", true, ""},
		{"company list", "Corp", true, "not a global allow/deny list"},
		{"unknown list", "Ghost", true, "not a global allow/deny list"},
		{"empty", " ", true, `choose an allow/deny list for "L"`},
		{"optional", "Bad", false, "must be required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, regID := newAssetMetaFixture(t)
			h.globalList("Office", true, nil)
			h.globalList("Bad", false, nil)
			company := uuid.New()
			h.globalList("Corp", false, &company)
			err := h.tryInstall(regID, "ns/p", map[string]string{"L": c.value}, fetchFrom(pkg(c.required)))
			if c.want == "" {
				if err != nil {
					t.Fatalf("install: %v", err)
				}
				if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != c.value {
					t.Fatalf("lists = %v", a.lists)
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

// TestBundleInstallsMemberListDeps: a bundle that references a page but not
// the list package the page depends on installs the list package for the
// page, links it to the page, and its uninstall removes everything.
func TestBundleInstallsMemberListDeps(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	bundle := typedPackage("ns/kit", "bundle", "1.0.0", "", nil, "ns/page")
	bip := h.install(regID, "ns/kit", nil, fetchFrom(bundle, page, listsPackage("1.0.0", false)))
	pip := h.installed(regID, "ns/page")
	lip := h.installed(regID, "ns/lists")
	if !lip.InstalledAsDependency {
		t.Fatal("list package not marked as a dependency")
	}
	if bm := h.memberIDs(bip.ID); !bm[*pip.ID] || bm[*lip.ID] {
		t.Fatalf("bundle members = %v", bm)
	}
	if !h.memberIDs(pip.ID)[*lip.ID] {
		t.Fatal("list package not linked to the page")
	}
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Scanners" {
		t.Fatalf("asset lists = %v", a.lists)
	}
	prev, err := h.s.UninstallPreview(h.ctx, h.session, bip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if strings.Join(prev.Members, ",") != "ns/page,ns/lists" || len(prev.KeptLists) != 0 {
		t.Fatalf("preview = %+v", prev)
	}
	if err := h.s.Remove(h.ctx, h.session, bip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, id := range []string{"ns/kit", "ns/page", "ns/lists"} {
		if h.isInstalled(regID, id) {
			t.Fatalf("%s still installed", id)
		}
	}
	if h.count(&database.AllowDeny{}) != 0 || h.count(&database.Asset{}) != 0 ||
		h.count(&database.InstalledPackageMember{}) != 0 {
		t.Fatal("bundle uninstall left lists, assets or member rows")
	}
}

// TestBundleReusesInstalledMemberListDep: a list package the user installed
// is reused for the page and stays when the bundle is uninstalled.
func TestBundleReusesInstalledMemberListDep(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	lip := h.install(regID, "ns/lists", nil, fetchFrom(listsPackage("1.0.0", false)))
	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	bundle := typedPackage("ns/kit", "bundle", "1.0.0", "", nil, "ns/page")
	bip := h.install(regID, "ns/kit", nil, fetchFrom(bundle, page, listsPackage("1.0.0", false)))
	pip := h.installed(regID, "ns/page")
	if !h.memberIDs(pip.ID)[*lip.ID] {
		t.Fatal("installed list package not linked to the page")
	}
	if err := h.s.Remove(h.ctx, h.session, bip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !h.isInstalled(regID, "ns/lists") || h.listCount("Scanners") != 1 || h.isInstalled(regID, "ns/page") {
		t.Fatal("want the user installed list package kept and the page removed")
	}
}

// TestBundleUpdateMemberListDeps: a bundle update that adds a page installs
// the list package it depends on for it, and one that drops the page removes
// both.
func TestBundleUpdateMemberListDeps(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	other := typedPackage("ns/other", "page", "1.0.0", pageYAML("Other", "o.html"), map[string]string{"o.html": "<p>o</p>"})
	v1 := typedPackage("ns/kit", "bundle", "1.0.0", "", nil, "ns/other")
	bip := h.install(regID, "ns/kit", nil, fetchFrom(v1, other))

	page := listPagePackage("ns/page", "1.0.0", "Scanners")
	v2 := typedPackage("ns/kit", "bundle", "2.0.0", "", nil, "ns/other", "ns/page")
	if _, _, err := h.applyWith(bip, v2, fetchFrom(v2, other, page, listsPackage("1.0.0", false)), nil); err != nil {
		t.Fatalf("update add: %v", err)
	}
	pip := h.installed(regID, "ns/page")
	lip := h.installed(regID, "ns/lists")
	if bm := h.memberIDs(bip.ID); !bm[*pip.ID] || bm[*lip.ID] {
		t.Fatalf("bundle members = %v", bm)
	}
	if !h.memberIDs(pip.ID)[*lip.ID] {
		t.Fatal("list package not linked to the page")
	}
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Scanners" {
		t.Fatalf("asset lists = %v", a.lists)
	}

	bip = h.installed(regID, "ns/kit")
	v3 := typedPackage("ns/kit", "bundle", "3.0.0", "", nil, "ns/other")
	if _, _, err := h.applyWith(bip, v3, fetchFrom(v3, other), nil); err != nil {
		t.Fatalf("update drop: %v", err)
	}
	if h.isInstalled(regID, "ns/page") || h.isInstalled(regID, "ns/lists") {
		t.Fatal("dropped page or its list package still installed")
	}
	if h.count(&database.AllowDeny{}) != 0 {
		t.Fatal("list left after the page was dropped")
	}
}

// TestListPickNotMixed: an asset uses either list names or exactly one list
// pick; both together, or two picks, are refused before any write.
func TestListPickNotMixed(t *testing.T) {
	cases := map[string]struct {
		entries string
		want    string
	}{
		"pick and name": {`["@@L@@", Bad]`, "not both"},
		"two picks":     {`["@@L@@", "@@M@@"]`, "only one list the installer picks"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, regID := newAssetMetaFixture(t)
			h.globalList("Bad", false, nil)
			h.globalList("Worse", false, nil)
			p := assetPackage("ns/p", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: "+c.entries+"\n", map[string]string{"a.js": "x"})
			p.Manifest.Variables = []registry.Variable{
				{Name: "L", Type: "text", Required: true, Pick: pickAllowDeny},
				{Name: "M", Type: "text", Required: true, Pick: pickAllowDeny},
			}
			err := h.tryInstall(regID, "ns/p", map[string]string{"L": "Bad", "M": "Worse"}, fetchFrom(p))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if h.count(&database.Asset{}) != 0 {
				t.Fatal("refused install wrote an asset")
			}
		})
	}
}

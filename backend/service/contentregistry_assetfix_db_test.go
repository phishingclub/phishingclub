package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// assetFileExists reports whether the shared asset file at rel is on disk.
func (h *ownershipFixture) assetFileExists(rel string) bool {
	h.t.Helper()
	_, err := os.Stat(filepath.Join(h.assetRoot, data.ASSET_GLOBAL_FOLDER, rel))
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		h.t.Fatalf("stat %s: %v", rel, err)
	}
	return false
}

const officeListYAML = "allowDenies:\n  - name: Office\n    allowed: true\n    cidrs: \"10.0.0.0/8\"\n"

// TestAssetFixSharedFilteredPathOneInstall installs a bundle whose two members
// and its own content ship the same asset with the same list, all taking the
// list from one list package. The second and third package compare with what
// the first created, whose lists are linked only later, so the install works
// and the one asset gets its list.
func TestAssetFixSharedFilteredPathOneInstall(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	files := map[string]string{"lib/x.js": "x()"}
	assetYAML := "assets:\n  - path: lib/x.js\n    allowDeny: [Office]\n"
	lists := typedPackage("ns/lists", "allowdeny", "1.0.0", officeListYAML, nil)
	m1 := typedPackage("ns/m1", "page", "1.0.0", assetYAML, nil, "ns/lists")
	m2 := typedPackage("ns/m2", "page", "1.0.0", assetYAML, nil, "ns/lists")
	bundle := assetPackage("ns/bundle", "1.0.0", assetYAML, files, "ns/m1", "ns/m2", "ns/lists")
	for _, p := range []*registry.VerifiedPackage{m1, m2} {
		p.Files["assets/lib/x.js"] = []byte(files["lib/x.js"])
	}
	h.install(regID, "ns/bundle", nil, fetchFrom(bundle, m1, m2, lists))

	if n := h.count(&database.Asset{}); n != 1 {
		t.Fatalf("assets = %d, want one shared asset", n)
	}
	a, _ := h.assetAt("lib/x.js")
	if strings.Join(a.lists, ",") != "Office" {
		t.Fatalf("lists = %v", a.lists)
	}
	if h.assetFile("lib/x.js") != "x()" {
		t.Fatal("asset file not written")
	}
	rows, err := h.s.RegistryRepository.GetInstalledResourcesByResource(h.ctx, "asset", a.id)
	if err != nil || len(rows) != 3 {
		t.Fatalf("provenance rows = %d (%v), want 3", len(rows), err)
	}
}

// TestAssetFixSharedFilteredPathUpdate updates a bundle to a version that adds
// a member shipping an asset with a list and ships the same asset itself.
func TestAssetFixSharedFilteredPathUpdate(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	v1 := assetPackage("ns/bundle", "1.0.0", officeListYAML, nil)
	ip := h.install(regID, "ns/bundle", nil, fetchFrom(v1))

	files := map[string]string{"lib/x.js": "x()"}
	assetYAML := "assets:\n  - path: lib/x.js\n    allowDeny: [Office]\n"
	m1 := assetPackage("ns/m1", "1.0.0", assetYAML, files)
	v2 := assetPackage("ns/bundle", "2.0.0", officeListYAML+assetYAML, files, "ns/m1")
	if _, _, err := h.applyWith(ip, v2, fetchFrom(v2, m1), nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	a, ok := h.assetAt("lib/x.js")
	if !ok || strings.Join(a.lists, ",") != "Office" || h.assetFile("lib/x.js") != "x()" {
		t.Fatalf("asset after update = %+v", a)
	}
}

// TestAssetFixCreatedAssetCompared checks that an asset created earlier in the
// same operation is shared only with the same bytes and settings.
func TestAssetFixCreatedAssetCompared(t *testing.T) {
	h, _ := newAssetMetaFixture(t)
	h.globalList("Office", true, nil)
	h.globalList("Partners", true, nil)
	ip := h.newInstall("ns/a")
	result := &InstallResult{}
	decl := &assetDecl{path: "a.js", name: "a.js", settings: assetSettings{allowDeny: []string{"Office"}}}
	id, created, err := h.s.installAsset(h.ctx, h.session, ip, "a.js", []byte("a"), decl, result)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	ip2 := h.newInstall("ns/b")
	same, created, err := h.s.installAsset(h.ctx, h.session, ip2, "a.js", []byte("a"), decl, result)
	if err != nil || created || *same != *id {
		t.Fatalf("same asset: %v %v %v", same, created, err)
	}
	other := &assetDecl{path: "a.js", name: "a.js", settings: assetSettings{allowDeny: []string{"Partners"}}}
	if _, _, err := h.s.installAsset(h.ctx, h.session, ip2, "a.js", []byte("a"), other, result); err == nil {
		t.Fatal("other lists shared an asset created in the same operation")
	}
	if _, _, err := h.s.installAsset(h.ctx, h.session, ip2, "a.js", []byte("b"), decl, result); err == nil {
		t.Fatal("other bytes shared an asset created in the same operation")
	}
}

// TestAssetFixFilteredFileAfterLinks checks that the file of a created asset
// with lists is written only once the lists are linked, so it is never served
// without them.
func TestAssetFixFilteredFileAfterLinks(t *testing.T) {
	h, _ := newAssetMetaFixture(t)
	p := assetPackage("ns/f", "1.0.0", officeListYAML+"assets:\n  - path: f.js\n    allowDeny: [Office]\n",
		map[string]string{"f.js": "f()", "open.js": "o()"})
	ip := h.newInstall("ns/f")
	result := &InstallResult{}
	if err := h.s.installEntities(h.ctx, h.session, p, nil, ip, "{}", nil, result); err != nil {
		t.Fatalf("install entities: %v", err)
	}
	if h.assetFileExists("f.js") {
		t.Fatal("filtered asset file written before its lists are linked")
	}
	if !h.assetFileExists("open.js") {
		t.Fatal("asset without lists not written")
	}
	if err := h.s.linkPendingAssets(h.ctx, h.session, result, nil); err != nil {
		t.Fatalf("link: %v", err)
	}
	a, _ := h.assetAt("f.js")
	if strings.Join(a.lists, ",") != "Office" || h.assetFile("f.js") != "f()" {
		t.Fatalf("asset after link = %+v", a)
	}
}

// TestAssetFixUpdateWritesBytesAfterLists checks that an update writes new
// bytes, and the file of a new asset with lists, only after the lists are
// linked.
func TestAssetFixUpdateWritesBytesAfterLists(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	v1 := assetPackage("ns/u", "1.0.0", "pages: []\n", map[string]string{"a.js": "old"})
	ip := h.install(regID, "ns/u", nil, fetchFrom(v1))

	yaml := officeListYAML + "assets:\n  - path: a.js\n    allowDeny: [Office]\n  - path: b.js\n    allowDeny: [Office]\n"
	v2 := assetPackage("ns/u", "2.0.0", yaml, map[string]string{"a.js": "new", "b.js": "b()"})
	resources, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ip.ID)
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	plan, err := h.s.planAssetUpdate(h.ctx, ip.ID, v2, nil, resources, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	result := &InstallResult{}
	undo := &updateUndo{}
	if err := h.s.reconcileAssets(h.ctx, h.session, ip.ID, "{}", result, undo, plan, nil); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if h.assetFile("a.js") != "old" {
		t.Fatal("new bytes written before the lists are linked")
	}
	if h.assetFileExists("b.js") {
		t.Fatal("new filtered asset written before its lists are linked")
	}
	h.globalList("Office", true, nil)
	if err := h.s.applyAssetSettings(h.ctx, h.session, plan, nil, result, undo); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, rel := range []string{"a.js", "b.js"} {
		if a, _ := h.assetAt(rel); strings.Join(a.lists, ",") != "Office" {
			t.Fatalf("%s lists = %v", rel, a.lists)
		}
	}
	if h.assetFile("a.js") != "new" || h.assetFile("b.js") != "b()" {
		t.Fatal("files not written after linking")
	}
	// the rollback deletes the new asset before anything else.
	undo.run()
	if h.assetFileExists("b.js") {
		t.Fatal("rollback left the new asset file")
	}
	if h.assetFile("a.js") != "old" {
		t.Fatal("rollback did not write the old bytes back")
	}
}

// TestAssetFixRemovalOrder checks that an asset is deleted before the lists of
// the same package, so a failed asset delete leaves its lists in place.
func TestAssetFixRemovalOrder(t *testing.T) {
	rows := []*database.InstalledResource{
		{ResourceType: "asset", SourceName: "a.js"},
		{ResourceType: "allowdeny", SourceName: "Office"},
	}
	got := removalOrder(rows)
	if got[0].ResourceType != "asset" || got[1].ResourceType != "allowdeny" {
		t.Fatalf("order = %s, %s", got[0].ResourceType, got[1].ResourceType)
	}
}

// TestAssetFixFilterLossConflict checks that an update that takes lists off an
// asset asks first, with what is lost, also when nobody edited the asset.
func TestAssetFixFilterLossConflict(t *testing.T) {
	t.Run("allow to deny", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		ip := h.installMeta(regID)
		yaml := strings.Replace(metaYAML, "      - Office\n", "      - Scanners\n", 1)
		yaml = strings.Replace(yaml, "allowDenies:\n",
			"allowDenies:\n  - name: Scanners\n    allowed: false\n    cidrs: \"192.0.2.0/24\"\n", 1)
		v2 := metaPackage("1.1.0", yaml, "<svg/>")
		_, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil)
		var uc ErrUpdateConflicts
		if !errors.As(err, &uc) || len(uc.Conflicts) != 1 {
			t.Fatalf("want one conflict, got %v", err)
		}
		c := uc.Conflicts[0]
		if c.Key() != "asset:acme/logo.svg" || c.FilterLoss == nil || !c.FilterLoss.OpenToEveryone ||
			c.FilterLoss.Unfiltered || strings.Join(c.FilterLoss.RemovedLists, ",") != "Office" || c.Note == "" {
			t.Fatalf("conflict = %+v loss %+v", c, c.FilterLoss)
		}
		raw, _ := json.Marshal(c)
		t.Logf("conflict JSON: %s", raw)
		res, _, err := h.applyWith(ip, v2, fetchFrom(v2), map[string]string{"asset:acme/logo.svg": resolveKeep})
		if err != nil {
			t.Fatalf("keep: %v", err)
		}
		if a, _ := h.assetAt("acme/logo.svg"); strings.Join(a.lists, ",") != "Office" {
			t.Fatalf("keep changed the lists: %v", a.lists)
		}
		if !strings.Contains(strings.Join(res.Skipped, "|"), "acme/logo.svg settings kept") {
			t.Fatalf("skipped = %v", res.Skipped)
		}
		ip = h.installed(regID, "ns/meta")
		if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), map[string]string{"asset:acme/logo.svg": resolveOverwrite}); err != nil {
			t.Fatalf("overwrite: %v", err)
		}
		if a, _ := h.assetAt("acme/logo.svg"); strings.Join(a.lists, ",") != "Scanners" {
			t.Fatalf("overwrite lists = %v", a.lists)
		}
	})
	t.Run("dropped", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		ip := h.installMeta(regID)
		v2 := metaPackage("1.1.0", officeListYAML+"assets:\n  - path: acme/logo.svg\n    name: Logo\n", "<svg/>")
		_, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil)
		var uc ErrUpdateConflicts
		if !errors.As(err, &uc) || len(uc.Conflicts) != 1 || uc.Conflicts[0].FilterLoss == nil ||
			!uc.Conflicts[0].FilterLoss.Unfiltered || !uc.Conflicts[0].FilterLoss.OpenToEveryone {
			t.Fatalf("want an unfiltered conflict, got %v", err)
		}
	})
	t.Run("operator list", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		ip := h.installMeta(regID)
		extra := h.globalList("Extra", true, nil)
		office := h.allowDenyByName("Office").ID.MustGet()
		a, _ := h.assetAt("acme/logo.svg")
		if err := h.s.Asset.AssetRepository.ReplaceAllowDeny(h.ctx, a.id, []*uuid.UUID{&office, extra}); err != nil {
			t.Fatalf("local list: %v", err)
		}
		v2 := metaPackage("1.1.0", strings.Replace(metaYAML, "value: \"@@SITE@@\"", "value: \"v2 @@SITE@@\"", 1), "<svg/>")
		_, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil)
		var uc ErrUpdateConflicts
		if !errors.As(err, &uc) || len(uc.Conflicts) != 1 {
			t.Fatalf("want one conflict, got %v", err)
		}
		loss := uc.Conflicts[0].FilterLoss
		if loss == nil || strings.Join(loss.RemovedLists, ",") != "Extra" || loss.OpenToEveryone || loss.Unfiltered {
			t.Fatalf("loss = %+v", loss)
		}
	})
	t.Run("no loss", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		ip := h.installMeta(regID)
		v2 := metaPackage("1.1.0", strings.Replace(metaYAML, "value: \"@@SITE@@\"", "value: \"v2 @@SITE@@\"", 1), "<svg/>")
		if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil); err != nil {
			t.Fatalf("update without a list change asked: %v", err)
		}
	})
}

// TestAssetFixChangedAssetSharedByNewMember checks that an update that changes
// an asset of the bundle and adds a member shipping the same path is refused
// before any write.
func TestAssetFixChangedAssetSharedByNewMember(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	files := map[string]string{"lib/x.js": "x()"}
	v1 := assetPackage("ns/bundle", "1.0.0",
		"assets:\n  - path: lib/x.js\n    headers:\n      - key: X-A\n        value: \"1\"\n", files)
	ip := h.install(regID, "ns/bundle", nil, fetchFrom(v1))

	v2yaml := "assets:\n  - path: lib/x.js\n    headers:\n      - key: X-A\n        value: \"2\"\n"
	m1 := assetPackage("ns/m1", "1.0.0", v2yaml, files)
	v2 := assetPackage("ns/bundle", "2.0.0", v2yaml, files, "ns/m1")
	_, _, err := h.applyWith(ip, v2, fetchFrom(v2, m1), nil)
	if err == nil || !strings.Contains(err.Error(), "changes in this version") {
		t.Fatalf("err = %v, want the shared change refusal", err)
	}
	if h.isInstalled(regID, "ns/m1") {
		t.Fatal("member written before the refusal")
	}
	if a, _ := h.assetAt("lib/x.js"); strings.Join(a.headers, "|") != "X-A: 1" {
		t.Fatalf("asset changed: %+v", a)
	}
}

// TestAssetFixDroppedMemberListReplaced checks that a bundle asset whose new
// declaration no longer names the list of a dropped member does not count as
// a loss of that member, so the member is removed without asking.
func TestAssetFixDroppedMemberListReplaced(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	h.globalList("Global", true, nil)
	member := assetPackage("ns/m", "1.0.0",
		"allowDenies:\n  - name: Member list\n    allowed: true\n    cidrs: \"10.0.0.0/8\"\n", nil)
	v1 := assetPackage("ns/bundle", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: [Member list]\n",
		map[string]string{"a.js": "a()"}, "ns/m")
	ip := h.install(regID, "ns/bundle", nil, fetchFrom(v1, member))

	v2 := assetPackage("ns/bundle", "2.0.0", "assets:\n  - path: a.js\n    allowDeny: [Global]\n",
		map[string]string{"a.js": "a()"})
	if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Global" {
		t.Fatalf("lists = %v", a.lists)
	}
	if h.isInstalled(regID, "ns/m") {
		t.Fatal("dropped member not removed")
	}
}

// TestAssetFixFilteredCacheControl checks the Cache-Control rule for a package
// asset with lists.
func TestAssetFixFilteredCacheControl(t *testing.T) {
	cases := map[string]struct {
		value  string
		lists  bool
		refuse bool
	}{
		"public":          {"public, max-age=60", true, true},
		"no directive":    {"max-age=60", true, true},
		"s-maxage":        {"private, s-maxage=60", true, true},
		"private":         {"private, max-age=60", true, false},
		"no-store":        {"no-store", true, false},
		"public unfilter": {"public, max-age=60", false, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, regID := newAssetMetaFixture(t)
			yaml := officeListYAML + "assets:\n  - path: a.js\n    headers:\n      - key: cache-control\n        value: \"" +
				c.value + "\"\n"
			if c.lists {
				yaml += "    allowDeny: [Office]\n"
			}
			p := assetPackage("ns/c", "1.0.0", yaml, map[string]string{"a.js": "a"})
			err := h.tryInstall(regID, "ns/c", nil, fetchFrom(p))
			if c.refuse && (err == nil || !strings.Contains(err.Error(), "Cache-Control")) {
				t.Fatalf("err = %v, want a Cache-Control refusal", err)
			}
			if !c.refuse && err != nil {
				t.Fatalf("install: %v", err)
			}
		})
	}
}

// TestAssetFixHeaderCap checks the 50 header limit for a package asset and
// for a header added by hand.
func TestAssetFixHeaderCap(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	var b strings.Builder
	b.WriteString("assets:\n  - path: a.js\n    headers:\n")
	for i := 0; i < model.MaxAssetHeaders+1; i++ {
		fmt.Fprintf(&b, "      - key: X-H%d\n        value: v\n", i)
	}
	p := assetPackage("ns/h", "1.0.0", b.String(), map[string]string{"a.js": "a"})
	if err := h.tryInstall(regID, "ns/h", nil, fetchFrom(p)); err == nil || !strings.Contains(err.Error(), "at most 50") {
		t.Fatalf("err = %v, want the header limit", err)
	}

	id := h.userAsset("user.js", "u")
	header := func(i int) *model.AssetHeader {
		return &model.AssetHeader{
			Key:   nullable.NewNullableWithValue(*vo.NewString127Must(fmt.Sprintf("X-H%d", i))),
			Value: nullable.NewNullableWithValue(*vo.NewString2048Must("v")),
		}
	}
	for i := 0; i < model.MaxAssetHeaders; i++ {
		if _, err := h.s.Asset.AddHeader(h.ctx, h.session, id, header(i)); err != nil {
			t.Fatalf("add header %d: %v", i, err)
		}
	}
	if _, err := h.s.Asset.AddHeader(h.ctx, h.session, id, header(99)); err == nil {
		t.Fatal("header 51 added")
	}
}

// TestAssetFixListNameTrimmed checks that a list name with spaces around it is
// recorded the same way an asset names it.
func TestAssetFixListNameTrimmed(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	yaml := "allowDenies:\n  - name: \" Office \"\n    allowed: true\n    cidrs: \"10.0.0.0/8\"\n" +
		"assets:\n  - path: a.js\n    allowDeny: [\" Office \"]\n"
	p := assetPackage("ns/t", "1.0.0", yaml, map[string]string{"a.js": "a"})
	ip := h.install(regID, "ns/t", nil, fetchFrom(p))
	rows := h.resourcesOf(ip.ID, "allowdeny")
	if len(rows) != 1 || rows[0].SourceName != "Office" {
		t.Fatalf("list rows = %+v", rows)
	}
	if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Office" {
		t.Fatalf("lists = %v", a.lists)
	}
}

// TestAssetFixHeaderTimes checks that ReplaceHeaders writes times in the form
// AddHeader writes them, so a header added later sorts last and the order of
// the replaced headers holds.
func TestAssetFixHeaderTimes(t *testing.T) {
	h, _ := newAssetMetaFixture(t)
	id := h.userAsset("user.js", "u")
	repo := h.s.Asset.AssetRepository
	var hs []*model.AssetHeader
	for i := 0; i < model.MaxAssetHeaders; i++ {
		hs = append(hs, &model.AssetHeader{
			Key:   nullable.NewNullableWithValue(*vo.NewString127Must(fmt.Sprintf("X-%02d", i))),
			Value: nullable.NewNullableWithValue(*vo.NewString2048Must("v")),
		})
	}
	if err := repo.ReplaceHeaders(h.ctx, id, hs); err != nil {
		t.Fatalf("replace: %v", err)
	}
	add := &model.AssetHeader{
		AssetID: nullable.NewNullableWithValue(*id),
		Key:     nullable.NewNullableWithValue(*vo.NewString127Must("X-Last")),
		Value:   nullable.NewNullableWithValue(*vo.NewString2048Must("v")),
	}
	if _, err := repo.AddHeader(h.ctx, add); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := repo.GetHeadersByAssetID(h.ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for i, hd := range got[:model.MaxAssetHeaders] {
		if want := fmt.Sprintf("X-%02d", i); hd.Key.MustGet().String() != want {
			t.Fatalf("header %d = %s, want %s", i, hd.Key.MustGet().String(), want)
		}
	}
	if got[len(got)-1].Key.MustGet().String() != "X-Last" {
		t.Fatal("a header added later does not sort last")
	}
	var times []string
	if err := h.db.Model(&database.AssetHeader{}).Where("asset_id = ?", id).Pluck("created_at", &times).Error; err != nil {
		t.Fatalf("pluck: %v", err)
	}
	for _, s := range times {
		parsed, perr := time.Parse(time.RFC3339Nano, s)
		if perr != nil || utils.RFC3339UTC(parsed) != s {
			t.Fatalf("stored time %q is not in the form AddHeader writes", s)
		}
	}
}

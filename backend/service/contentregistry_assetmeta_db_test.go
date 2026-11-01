package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
)

// newAssetMetaFixture wires assets, allow deny lists and the update deps, so a
// package with an assets block runs through install, update and uninstall.
func newAssetMetaFixture(t *testing.T) (*ownershipFixture, *uuid.UUID) {
	t.Helper()
	h := newOwnershipFixture(t)
	h.withUpdateDeps("ns/unused")
	h.withAllowDenyService()
	if err := h.db.AutoMigrate(&database.CampaignAllowDeny{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	regID := uuid.New()
	return h, &regID
}

// tryInstall runs the install plan and write steps and returns the error.
func (h *ownershipFixture) tryInstall(
	regID *uuid.UUID, id string, vars map[string]string, fetch func(string) (*registry.VerifiedPackage, error),
) error {
	ns, name := splitPackageID(id)
	plan, err := h.s.planInstall(h.ctx, regID, ns, name, vars, nil, fetch)
	if err != nil {
		return err
	}
	ae := NewAuditEvent("Registry.Install", h.session)
	_, err = h.s.applyInstall(h.ctx, h.session, ae, regID, plan, vars)
	return err
}

// assetPackage is a package with one content folder and asset files.
func assetPackage(id, version, dataYAML string, assets map[string]string, deps ...string) *registry.VerifiedPackage {
	p := typedPackage(id, "page", version, dataYAML, nil, deps...)
	if len(deps) > 0 {
		p.Manifest.Type = "bundle"
	}
	for k, v := range assets {
		p.Files["assets/"+k] = []byte(v)
	}
	return p
}

// globalList stores an allow deny list the way the admin UI does. companyID
// nil makes a global list.
func (h *ownershipFixture) globalList(name string, allowed bool, companyID *uuid.UUID) *uuid.UUID {
	h.t.Helper()
	m, err := buildAllowDenyModel(name, allowed, "10.9.0.0/16", "", "", "", "")
	if err != nil {
		h.t.Fatalf("build list: %v", err)
	}
	if companyID != nil {
		m.CompanyID = nullable.NewNullableWithValue(*companyID)
	}
	id, err := h.s.AllowDeny.AllowDenyRepository.Insert(h.ctx, m)
	if err != nil {
		h.t.Fatalf("insert list: %v", err)
	}
	return id
}

type storedAsset struct {
	id          *uuid.UUID
	name        string
	description string
	contentType string
	headers     []string
	lists       []string
}

// assetAt reads the global asset at rel with its headers and lists. ok is
// false when there is none.
func (h *ownershipFixture) assetAt(rel string) (storedAsset, bool) {
	h.t.Helper()
	a, err := h.s.Asset.AssetRepository.GetGlobalByPath(h.ctx, rel)
	if err != nil {
		h.t.Fatalf("get asset %s: %v", rel, err)
	}
	if a == nil {
		return storedAsset{}, false
	}
	id := a.ID.MustGet()
	st, full, err := h.s.storedAssetSettings(h.ctx, &id)
	if err != nil {
		h.t.Fatalf("asset settings %s: %v", rel, err)
	}
	out := storedAsset{
		id: &id, name: optStr(full.Name), description: optStr(full.Description),
		contentType: st.contentType, lists: st.allowDeny,
	}
	for _, hd := range st.headers {
		out.headers = append(out.headers, hd.key+": "+hd.value)
	}
	return out, true
}

func (h *ownershipFixture) count(m any) int64 {
	h.t.Helper()
	var n int64
	if err := h.db.Model(m).Count(&n).Error; err != nil {
		h.t.Fatalf("count: %v", err)
	}
	return n
}

const metaYAML = `
allowDenies:
  - name: Office
    allowed: true
    cidrs: "10.0.0.0/8"
assets:
  - path: acme/logo.svg
    name: Logo
    description: Company logo
    contentType: image/svg+xml
    headers:
      - key: Cache-Control
        value: no-store
      - key: X-Site
        value: "@@SITE@@"
    allowDeny:
      - Office
`

func metaPackage(version, dataYAML, logo string) *registry.VerifiedPackage {
	p := assetPackage("ns/meta", version, dataYAML, map[string]string{"acme/logo.svg": logo, "acme/plain.css": "a{}"})
	p.Manifest.Variables = []registry.Variable{{Name: "SITE", Type: "text", Required: true}}
	return p
}

// installMeta installs metaPackage v1 and returns its installed row.
func (h *ownershipFixture) installMeta(regID *uuid.UUID) *database.InstalledPackage {
	h.t.Helper()
	p := metaPackage("1.0.0", metaYAML, "<svg/>")
	return h.install(regID, "ns/meta", map[string]string{"SITE": "hq"}, fetchFrom(p))
}

// TestAssetMetaInstall installs a package whose assets block sets the name,
// description, content type, headers in order and a list the package declares,
// with a variable substituted. An asset without an entry keeps the defaults.
// The recorded baseline matches the stored fingerprint, so nothing reads as
// edited.
func TestAssetMetaInstall(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)

	a, ok := h.assetAt("acme/logo.svg")
	if !ok {
		t.Fatal("asset not installed")
	}
	if a.name != "Logo" || a.description != "Company logo" || a.contentType != "image/svg+xml" {
		t.Fatalf("asset fields = %+v", a)
	}
	if got := strings.Join(a.headers, "|"); got != "Cache-Control: no-store|X-Site: hq" {
		t.Fatalf("headers = %q", got)
	}
	if got := strings.Join(a.lists, ","); got != "Office" {
		t.Fatalf("lists = %q", got)
	}
	plain, ok := h.assetAt("acme/plain.css")
	if !ok || plain.name != "plain.css" || plain.contentType != "" || len(plain.headers) != 0 || len(plain.lists) != 0 {
		t.Fatalf("plain asset = %+v", plain)
	}
	for _, r := range h.resourcesOf(ip.ID, "asset") {
		if got := h.s.storedHash(h.ctx, "asset", r.ResourceID); got != r.InstalledContentHash {
			t.Fatalf("asset %s baseline %q != stored %q", r.SourceName, r.InstalledContentHash, got)
		}
	}
	prev, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(prev.Edited) != 0 {
		t.Fatalf("fresh install reads as edited: %+v", prev.Edited)
	}
}

// TestAssetMetaInvalidRefusedBeforeWrites checks that a bad assets block fails
// the install before anything is written.
func TestAssetMetaInvalidRefusedBeforeWrites(t *testing.T) {
	cases := map[string]struct {
		yaml string
		want string
	}{
		"content type": {"assets:\n  - path: a.svg\n    contentType: not a type\n", "content type"},
		"blocked header": {
			"assets:\n  - path: a.svg\n    headers:\n      - key: Content-Length\n        value: \"1\"\n", "Content-Length",
		},
		"header value": {
			"assets:\n  - path: a.svg\n    headers:\n      - key: X-A\n        value: \"a\\nb\"\n", "invalid characters",
		},
		"missing file": {"assets:\n  - path: none.svg\n", "not in the assets folder"},
		"duplicate":    {"assets:\n  - path: a.svg\n  - path: a.svg\n", "more than once"},
		"long name":    {"assets:\n  - path: a.svg\n    name: " + strings.Repeat("n", 200) + "\n", "invalid name"},
		"bad path":     {"assets:\n  - path: ../a.svg\n", "path"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, regID := newAssetMetaFixture(t)
			yaml := "pages:\n  - name: P\n    file: p.html\n" + c.yaml
			p := assetPackage("ns/bad", "1.0.0", yaml, map[string]string{"a.svg": "<svg/>"})
			p.Files["content/main/p.html"] = []byte("<p>p</p>")
			err := h.tryInstall(regID, "ns/bad", nil, fetchFrom(p))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
			if n := h.count(&database.Asset{}); n != 0 {
				t.Fatalf("%d assets written", n)
			}
			if n := h.count(&database.Page{}); n != 0 {
				t.Fatalf("%d pages written", n)
			}
			if h.isInstalled(regID, "ns/bad") {
				t.Fatal("package recorded as installed")
			}
		})
	}
}

// TestAssetMetaSharedPathSettings checks that a second package shipping the
// same bytes at a path another install owns is refused when its settings
// differ and shares the asset when they match.
func TestAssetMetaSharedPathSettings(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	files := map[string]string{"lib/app.js": "ok()"}
	a := assetPackage("ns/a", "1.0.0", "assets:\n  - path: lib/app.js\n    contentType: text/javascript\n", files)
	h.install(regID, "ns/a", nil, fetchFrom(a))

	b := assetPackage("ns/b", "1.0.0", "assets:\n  - path: lib/app.js\n    contentType: text/plain\n", files)
	err := h.tryInstall(regID, "ns/b", nil, fetchFrom(b))
	if err == nil || !strings.Contains(err.Error(), "lib/app.js") || !strings.Contains(err.Error(), "ns/a") {
		t.Fatalf("differing settings at a shared path: err = %v", err)
	}
	// no entry at all differs too, since the owner set a content type.
	c := assetPackage("ns/c", "1.0.0", "pages: []\n", files)
	if err := h.tryInstall(regID, "ns/c", nil, fetchFrom(c)); err == nil {
		t.Fatal("default settings at a path shipped with a content type were shared")
	}
	d := assetPackage("ns/d", "1.0.0", "assets:\n  - path: lib/app.js\n    contentType: text/javascript\n", files)
	h.install(regID, "ns/d", nil, fetchFrom(d))
	if n := h.count(&database.Asset{}); n != 1 {
		t.Fatalf("assets = %d, want one shared asset", n)
	}
}

// TestAssetMetaUpdateApply checks that settings changed upstream on an asset
// nobody edited are applied in place, and the baseline follows.
func TestAssetMetaUpdateApply(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)
	v2yaml := strings.Replace(metaYAML, "value: no-store", "value: private, max-age=60", 1)
	v2yaml = strings.Replace(v2yaml, "contentType: image/svg+xml", "contentType: image/svg+xml; charset=utf-8", 1)
	v2 := metaPackage("1.1.0", v2yaml, "<svg/>")
	res, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	a, _ := h.assetAt("acme/logo.svg")
	if a.contentType != "image/svg+xml; charset=utf-8" || strings.Join(a.headers, "|") != "Cache-Control: private, max-age=60|X-Site: hq" {
		t.Fatalf("asset after update = %+v", a)
	}
	if got := strings.Join(a.lists, ","); got != "Office" {
		t.Fatalf("lists after update = %q", got)
	}
	if !containsString(res.Assets, "acme/logo.svg") {
		t.Fatalf("result assets = %v", res.Assets)
	}
	for _, r := range h.resourcesOf(ip.ID, "asset") {
		if got := h.s.storedHash(h.ctx, "asset", r.ResourceID); got != r.InstalledContentHash {
			t.Fatalf("asset %s baseline does not follow the update", r.SourceName)
		}
	}
}

// TestAssetMetaUpdateConflict checks that settings edited locally and changed
// upstream wait for keep or overwrite, that keep leaves the local settings,
// that both is refused and that overwrite applies the new version. New bytes
// alone never ask.
func TestAssetMetaUpdateConflict(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)
	a, _ := h.assetAt("acme/logo.svg")
	if err := h.s.Asset.AssetRepository.UpdateByID(h.ctx, a.id, assetContentTypeModel("text/plain")); err != nil {
		t.Fatalf("local edit: %v", err)
	}
	prev, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(prev.Edited) != 1 || prev.Edited[0].ResourceType != "asset" {
		t.Fatalf("local asset edit not detected: %+v", prev.Edited)
	}

	// new bytes only: written without a question, local settings stay.
	v11 := metaPackage("1.1.0", metaYAML, "<svg id='b'/>")
	if _, _, err := h.applyWith(ip, v11, fetchFrom(v11), nil); err != nil {
		t.Fatalf("bytes only update: %v", err)
	}
	if got := h.assetFile("acme/logo.svg"); got != "<svg id='b'/>" {
		t.Fatalf("bytes = %q", got)
	}
	if a, _ = h.assetAt("acme/logo.svg"); a.contentType != "text/plain" {
		t.Fatalf("local content type lost on a bytes only update: %q", a.contentType)
	}

	ip = h.installed(regID, "ns/meta")
	v2 := metaPackage("1.2.0", strings.Replace(metaYAML, "value: no-store", "value: private, max-age=60", 1), "<svg id='b'/>")
	_, _, err = h.applyWith(ip, v2, fetchFrom(v2), nil)
	var uc ErrUpdateConflicts
	if !errors.As(err, &uc) || len(uc.Conflicts) != 1 || uc.Conflicts[0].Key() != "asset:acme/logo.svg" {
		t.Fatalf("want one asset conflict, got %v", err)
	}
	if uc.Conflicts[0].CurrentName != "Logo" {
		t.Fatalf("conflict current name = %q", uc.Conflicts[0].CurrentName)
	}
	if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), map[string]string{"asset:acme/logo.svg": resolveBoth}); err == nil {
		t.Fatal("keep both accepted for an asset")
	}
	res, _, err := h.applyWith(ip, v2, fetchFrom(v2), map[string]string{"asset:acme/logo.svg": resolveKeep})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if !containsString(res.Skipped, "asset acme/logo.svg "+keptLocalEdit+")") {
		t.Fatalf("skipped = %v", res.Skipped)
	}
	if a, _ = h.assetAt("acme/logo.svg"); a.contentType != "text/plain" || a.headers[0] != "Cache-Control: no-store" {
		t.Fatalf("keep changed the asset: %+v", a)
	}

	ip = h.installed(regID, "ns/meta")
	if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), map[string]string{"asset:acme/logo.svg": resolveOverwrite}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	a, _ = h.assetAt("acme/logo.svg")
	if a.contentType != "image/svg+xml" || a.headers[0] != "Cache-Control: private, max-age=60" {
		t.Fatalf("overwrite did not apply: %+v", a)
	}
	for _, r := range h.resourcesOf(ip.ID, "asset") {
		if got := h.s.storedHash(h.ctx, "asset", r.ResourceID); got != r.InstalledContentHash {
			t.Fatalf("asset %s baseline does not follow the overwrite", r.SourceName)
		}
	}
}

// TestAssetMetaListSources checks the places a list name can come from: an
// existing global list, a list package a bundle member depends on, and an
// installer pick. A list another member declares is not a source.
func TestAssetMetaListSources(t *testing.T) {
	t.Run("existing global", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		h.globalList("Scanners", false, nil)
		p := assetPackage("ns/g", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: [Scanners]\n", map[string]string{"a.js": "x"})
		h.install(regID, "ns/g", nil, fetchFrom(p))
		if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Scanners" {
			t.Fatalf("lists = %v", a.lists)
		}
	})
	t.Run("bundle", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		listYAML := "allowDenies:\n  - name: Bundle list\n    allowed: true\n    cidrs: \"10.0.0.0/8\"\n"
		lists := typedPackage("ns/lists", "allowdeny", "1.0.0", listYAML, nil)
		member := typedPackage("ns/member", "page", "1.0.0",
			"assets:\n  - path: m.js\n    allowDeny: [Bundle list]\n", nil, "ns/lists")
		member.Files["assets/m.js"] = []byte("x")
		bundle := assetPackage("ns/bundle", "1.0.0", "", nil, "ns/member", "ns/lists")
		h.install(regID, "ns/bundle", nil, fetchFrom(bundle, member, lists))
		if a, _ := h.assetAt("m.js"); strings.Join(a.lists, ",") != "Bundle list" {
			t.Fatalf("lists = %v", a.lists)
		}
	})
	t.Run("other member", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		listYAML := "allowDenies:\n  - name: Bundle list\n    allowed: true\n    cidrs: \"10.0.0.0/8\"\n"
		lists := typedPackage("ns/lists", "allowdeny", "1.0.0", listYAML, nil)
		member := assetPackage("ns/member", "1.0.0", "assets:\n  - path: m.js\n    allowDeny: [Bundle list]\n", map[string]string{"m.js": "x"})
		bundle := assetPackage("ns/bundle", "1.0.0", "", nil, "ns/member", "ns/lists")
		err := h.tryInstall(regID, "ns/bundle", nil, fetchFrom(bundle, member, lists))
		if err == nil || !strings.Contains(err.Error(), "Bundle list") {
			t.Fatalf("err = %v", err)
		}
		if h.count(&database.Asset{}) != 0 {
			t.Fatal("refused install wrote an asset")
		}
	})
	t.Run("pick", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		h.globalList("Picked", true, nil)
		p := assetPackage("ns/p", "1.0.0", "assets:\n  - path: a.js\n    allowDeny: [\"@@LIST@@\"]\n", map[string]string{"a.js": "x"})
		p.Manifest.Variables = []registry.Variable{{Name: "LIST", Type: "text", Required: true, Pick: pickAllowDeny}}
		if err := h.tryInstall(regID, "ns/p", nil, fetchFrom(p)); err == nil {
			t.Fatal("install without the pick value succeeded")
		}
		h.install(regID, "ns/p", map[string]string{"LIST": "Picked"}, fetchFrom(p))
		if a, _ := h.assetAt("a.js"); strings.Join(a.lists, ",") != "Picked" {
			t.Fatalf("lists = %v", a.lists)
		}
	})
}

// TestAssetMetaListRefused checks that a name that resolves to nothing, to a
// company list only, or lists of both kinds fail before any write.
func TestAssetMetaListRefused(t *testing.T) {
	cases := map[string]struct {
		lists string
		want  string
	}{
		"unresolved": {"[Ghost]", `"Ghost"`},
		"company":    {"[Corp]", `"Corp"`},
		"mixed":      {"[Office, Scanners]", "both allow lists and block lists"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, regID := newAssetMetaFixture(t)
			company := uuid.New()
			h.globalList("Corp", false, &company)
			h.globalList("Scanners", false, nil)
			yaml := "allowDenies:\n  - name: Office\n    allowed: true\n    cidrs: \"10.0.0.0/8\"\n" +
				"assets:\n  - path: a.js\n    allowDeny: " + c.lists + "\n"
			p := assetPackage("ns/r", "1.0.0", yaml, map[string]string{"a.js": "x"})
			err := h.tryInstall(regID, "ns/r", nil, fetchFrom(p))
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "a.js") {
				t.Fatalf("err = %v, want it to mention %s and the asset", err, c.want)
			}
			if n := h.count(&database.Asset{}); n != 0 {
				t.Fatalf("%d assets written", n)
			}
			if n := h.count(&database.AllowDeny{}); n != 2 {
				t.Fatalf("lists = %d, want only the two made by the test", n)
			}
		})
	}
}

// TestAssetMetaRollback fails an install after its assets got headers and
// lists, and checks the rollback leaves no asset row, header, link, list or
// file behind.
func TestAssetMetaRollback(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	yaml := metaYAML + "campaignTemplates:\n  - name: Kit\n    landingPage: Missing page\n"
	p := metaPackage("1.0.0", yaml, "<svg/>")
	err := h.tryInstall(regID, "ns/meta", map[string]string{"SITE": "hq"}, fetchFrom(p))
	if err == nil {
		t.Fatal("install with a missing page succeeded")
	}
	for name, m := range map[string]any{
		"assets": &database.Asset{}, "headers": &database.AssetHeader{},
		"links": &database.AssetAllowDeny{}, "lists": &database.AllowDeny{},
	} {
		if n := h.count(m); n != 0 {
			t.Fatalf("%d %s left after rollback", n, name)
		}
	}
	if _, err := os.Stat(filepath.Join(h.assetRoot, data.ASSET_GLOBAL_FOLDER, "acme/logo.svg")); !os.IsNotExist(err) {
		t.Fatalf("asset file left after rollback: %v", err)
	}
	if h.isInstalled(regID, "ns/meta") {
		t.Fatal("package left installed")
	}
}

// TestAssetMetaUninstallPreview checks that the uninstall keeps a list a user
// asset uses, so the asset never loses its filter, and the preview names the
// kept list instead of an asset open to everyone.
func TestAssetMetaUninstallPreview(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)
	userID := h.userAsset("user/page.html", "<p>user</p>")
	office := h.allowDenyByName("Office").ID.MustGet()
	if err := h.s.Asset.AssetRepository.ReplaceAllowDeny(h.ctx, userID, []*uuid.UUID{&office}); err != nil {
		t.Fatalf("link user asset: %v", err)
	}
	prev, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.ChangedAssetCount != 0 {
		t.Fatalf("changed assets = %+v", prev.ChangedAssets)
	}
	if len(prev.KeptLists) != 1 || prev.KeptLists[0].Name != "Office" || !prev.KeptLists[0].Allowed ||
		strings.Join(prev.KeptLists[0].Assets, ",") != "user/page.html" {
		t.Fatalf("kept lists = %+v", prev.KeptLists)
	}
	if err := h.s.Remove(h.ctx, h.session, ip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	lists, err := h.s.Asset.AssetRepository.GetAllowDenyByAssetID(h.ctx, userID)
	if err != nil || len(lists) != 1 || optStr(lists[0].Name) != "Office" {
		t.Fatalf("user asset lists after uninstall = %v, %v", lists, err)
	}
}

// TestAssetMetaDroppedList checks the update removal of a list the new version
// drops: a list a user asset still uses is kept and untracked, and a list only
// the package asset used is removed once the asset no longer uses it.
func TestAssetMetaDroppedList(t *testing.T) {
	v2yaml := "assets:\n  - path: acme/logo.svg\n    name: Logo\n"
	// the new version leaves the asset with no lists, which asks first.
	overwriteLogo := map[string]string{"asset:acme/logo.svg": resolveOverwrite}
	t.Run("user linked", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		ip := h.installMeta(regID)
		userID := h.userAsset("user/page.html", "<p>user</p>")
		office := h.allowDenyByName("Office").ID.MustGet()
		if err := h.s.Asset.AssetRepository.ReplaceAllowDeny(h.ctx, userID, []*uuid.UUID{&office}); err != nil {
			t.Fatalf("link user asset: %v", err)
		}
		v2 := metaPackage("2.0.0", v2yaml, "<svg/>")
		res, _, err := h.applyWith(ip, v2, fetchFrom(v2), overwriteLogo)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		h.allowDenyByName("Office")
		if len(h.resourcesOf(ip.ID, "allowdeny")) != 0 {
			t.Fatal("kept list is still tracked")
		}
		if !strings.Contains(strings.Join(res.Skipped, "|"), `allowdeny Office kept (no longer in package, still used by asset "user/page.html")`) {
			t.Fatalf("skipped = %v", res.Skipped)
		}
		if a, _ := h.assetAt("acme/logo.svg"); len(a.lists) != 0 {
			t.Fatalf("package asset still uses the dropped list: %v", a.lists)
		}
	})
	t.Run("package only", func(t *testing.T) {
		h, regID := newAssetMetaFixture(t)
		ip := h.installMeta(regID)
		v2 := metaPackage("2.0.0", v2yaml, "<svg/>")
		if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), overwriteLogo); err != nil {
			t.Fatalf("update: %v", err)
		}
		if n := h.count(&database.AllowDeny{}); n != 0 {
			t.Fatalf("dropped list not removed: %d left", n)
		}
		a, _ := h.assetAt("acme/logo.svg")
		if len(a.lists) != 0 || len(a.headers) != 0 || a.contentType != "" {
			t.Fatalf("asset after update = %+v", a)
		}
	})
}

// TestAssetMetaUpdateNewAssetAndList checks that an update adding a list and a
// new asset that uses it links the asset once the list exists.
func TestAssetMetaUpdateNewAssetAndList(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)
	v2yaml := metaYAML + "  - path: acme/new.js\n    allowDeny: [Partners]\n"
	v2yaml = strings.Replace(v2yaml, "allowDenies:\n", "allowDenies:\n  - name: Partners\n    allowed: true\n    cidrs: \"172.16.0.0/12\"\n", 1)
	v2 := metaPackage("1.1.0", v2yaml, "<svg/>")
	v2.Files["assets/acme/new.js"] = []byte("n()")
	if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if a, ok := h.assetAt("acme/new.js"); !ok || strings.Join(a.lists, ",") != "Partners" {
		t.Fatalf("new asset = %+v", a)
	}
}

// TestAssetMetaSettingsUndo checks that a failed update writes the settings an
// asset had back: content type, headers in order and lists.
func TestAssetMetaSettingsUndo(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)
	h.globalList("Other", true, nil)
	v2yaml := strings.Replace(metaYAML, "value: no-store", "value: private, max-age=60", 1)
	v2yaml = strings.Replace(v2yaml, "      - Office\n", "      - Other\n", 1)
	v2 := metaPackage("1.1.0", v2yaml, "<svg/>")
	resources, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ip.ID)
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	plan, err := h.s.planAssetUpdate(h.ctx, ip.ID, v2, map[string]string{"SITE": "hq"}, resources, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	before, _ := h.assetAt("acme/logo.svg")
	undo := &updateUndo{}
	if err := h.s.applyAssetSettings(h.ctx, h.session, plan, nil, &InstallResult{}, undo); err != nil {
		t.Fatalf("apply: %v", err)
	}
	mid, _ := h.assetAt("acme/logo.svg")
	if mid.headers[0] != "Cache-Control: private, max-age=60" || strings.Join(mid.lists, ",") != "Other" {
		t.Fatalf("settings not applied: %+v", mid)
	}
	undo.run()
	if undo.failures != 0 {
		t.Fatalf("undo failures: %d", undo.failures)
	}
	after, _ := h.assetAt("acme/logo.svg")
	if strings.Join(after.headers, "|") != strings.Join(before.headers, "|") ||
		strings.Join(after.lists, ",") != strings.Join(before.lists, ",") || after.contentType != before.contentType {
		t.Fatalf("undo left %+v, want %+v", after, before)
	}
}

// TestAssetMetaListKeptBothFollowsID checks the "both" resolution on a list an
// asset uses: the local copy is renamed and the asset keeps using it, the same
// way a campaign keeps the list it points at. The asset then reads as edited,
// since the package names the list it does not use.
func TestAssetMetaListKeptBothFollowsID(t *testing.T) {
	h, regID := newAssetMetaFixture(t)
	ip := h.installMeta(regID)
	office := h.allowDenyByName("Office")
	edited, err := buildAllowDenyModel("Office", true, "10.0.0.0/8\n192.168.0.0/16", "", "", "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := h.s.AllowDeny.AllowDenyRepository.Update(h.ctx, office.ID.MustGet(), edited); err != nil {
		t.Fatalf("local list edit: %v", err)
	}
	v2 := metaPackage("1.1.0", strings.Replace(metaYAML, `cidrs: "10.0.0.0/8"`, `cidrs: "10.1.0.0/16"`, 1), "<svg/>")
	if _, _, err := h.applyWith(ip, v2, fetchFrom(v2), map[string]string{"allowdeny:Office": resolveBoth}); err != nil {
		t.Fatalf("update: %v", err)
	}
	a, _ := h.assetAt("acme/logo.svg")
	if len(a.lists) != 1 || !strings.HasPrefix(a.lists[0], "Office (local") {
		t.Fatalf("asset lists = %v, want the renamed local copy", a.lists)
	}
	r := h.resourcesOf(ip.ID, "asset")
	for _, row := range r {
		if row.SourceName == "acme/logo.svg" && h.s.storedHash(h.ctx, "asset", row.ResourceID) == row.InstalledContentHash {
			t.Fatal("asset on the renamed list does not read as edited")
		}
	}
}

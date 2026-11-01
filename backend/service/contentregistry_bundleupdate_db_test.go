package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
)

// typedPackage is yamlPackage with a package type and the packages it references.
func typedPackage(
	id, pkgType, version, dataYAML string, files map[string]string, deps ...string,
) *registry.VerifiedPackage {
	p := yamlPackage(id, version, dataYAML, files)
	p.Manifest.Type = pkgType
	for _, d := range deps {
		p.Manifest.Dependencies = append(p.Manifest.Dependencies, registry.Dependency{ID: d})
	}
	return p
}

// fetchFrom answers a fetch from a fixed set of packages.
func fetchFrom(pkgs ...*registry.VerifiedPackage) func(string) (*registry.VerifiedPackage, error) {
	byID := map[string]*registry.VerifiedPackage{}
	for _, p := range pkgs {
		byID[p.Manifest.ID] = p
	}
	return func(id string) (*registry.VerifiedPackage, error) {
		if p, ok := byID[id]; ok {
			return p, nil
		}
		return nil, errors.New("not found")
	}
}

// install runs the install plan and write steps Install takes, without the lock.
func (h *regFixture) install(
	regID *uuid.UUID,
	id string,
	vars map[string]string,
	fetch func(string) (*registry.VerifiedPackage, error),
) *database.InstalledPackage {
	h.t.Helper()
	ns, name := splitPackageID(id)
	plan, err := h.s.planInstall(h.ctx, regID, ns, name, vars, nil, fetch)
	if err != nil {
		h.t.Fatalf("plan install %s: %v", id, err)
	}
	ae := NewAuditEvent("Registry.Install", h.session)
	if _, err := h.s.applyInstall(h.ctx, h.session, ae, regID, plan, vars); err != nil {
		h.t.Fatalf("install %s: %v", id, err)
	}
	return h.installed(regID, id)
}

func (h *regFixture) installed(regID *uuid.UUID, id string) *database.InstalledPackage {
	h.t.Helper()
	ip, err := h.s.RegistryRepository.GetInstalledPackage(h.ctx, regID, id)
	if err != nil {
		h.t.Fatalf("installed %s: %v", id, err)
	}
	return ip
}

func (h *regFixture) isInstalled(regID *uuid.UUID, id string) bool {
	_, err := h.s.RegistryRepository.GetInstalledPackage(h.ctx, regID, id)
	return err == nil
}

func (h *regFixture) memberIDs(bundleID *uuid.UUID) map[uuid.UUID]bool {
	h.t.Helper()
	ms, err := h.s.RegistryRepository.GetPackageMembers(h.ctx, bundleID)
	if err != nil {
		h.t.Fatalf("members: %v", err)
	}
	out := map[uuid.UUID]bool{}
	for _, m := range ms {
		out[*m] = true
	}
	return out
}

func pageYAML(name, file string) string {
	return "\npages:\n  - name: " + name + "\n    file: " + file + "\n"
}

func kitYAML(landing string) string {
	return "\ncampaignTemplates:\n  - name: Kit\n    landingPage: " + landing + "\n"
}

// TestUpdateScopedMemberKeepsInstallValues installs a bundle that does not share
// variables, so its member stores its value under a package scoped key. Updating
// the member on its own must read that value, not the author default the
// member's own manifest would pick.
func TestUpdateScopedMemberKeepsInstallValues(t *testing.T) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/unused")
	regID := uuid.New()
	def := "Hello default"
	greetVar := []registry.Variable{{Name: "GREETING", Type: "text", Default: &def}}
	member := typedPackage(
		"ns/member", "page", "1.0.0", pageYAML("Greet", "greet.html"),
		map[string]string{"greet.html": "<p>@@GREETING@@</p>"},
	)
	member.Manifest.Variables = greetVar
	off := false
	bundle := typedPackage("ns/bundle", "bundle", "1.0.0", "", nil, "ns/member")
	bundle.Manifest.ShareVariables = &off
	vars := map[string]string{varKey("ns/member", "GREETING", false): "Hi user"}

	h.install(&regID, "ns/bundle", vars, fetchFrom(bundle, member))
	mip := h.installed(&regID, "ns/member")
	if !mip.VariablesScoped || !h.installed(&regID, "ns/bundle").VariablesScoped {
		t.Fatal("install did not record the scoped share mode")
	}
	pageID, ok := h.pageIDByName("Greet")
	if !ok || h.pageContent(pageID) != "<p>Hi user</p>" {
		t.Fatalf("install did not apply the supplied value")
	}
	if installedShare(mip, member.Manifest) {
		t.Fatal("installedShare ignores the recorded scoped mode")
	}

	v2 := typedPackage(
		"ns/member", "page", "1.1.0", pageYAML("Greet", "greet.html"),
		map[string]string{"greet.html": "<p>@@GREETING@@ v2</p>"},
	)
	v2.Manifest.Variables = greetVar
	if _, _, err := h.apply(mip, v2, nil); err != nil {
		t.Fatalf("member update: %v", err)
	}
	if got := h.pageContent(pageID); got != "<p>Hi user v2</p>" {
		t.Fatalf("member update used %q, want the install value", got)
	}

	// a row from before the column existed keeps the manifest mode.
	if !installedShare(&database.InstalledPackage{}, v2.Manifest) {
		t.Fatal("an unscoped row should follow the manifest")
	}
}

// TestUpdateBundleAddsAndDropsMembers updates a bundle to a version that
// references a new package and drops two. The new one is installed as a
// dependency and the bundle's campaign template is rewired to its page. The
// dropped one only this bundle used is uninstalled; the one another bundle also
// links is only unlinked.
func TestUpdateBundleAddsAndDropsMembers(t *testing.T) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/unused")
	regID := uuid.New()
	a := typedPackage("ns/a", "page", "1.0.0", pageYAML("A page", "a.html"), map[string]string{"a.html": "<p>a</p>"})
	c := typedPackage("ns/c", "page", "1.0.0", pageYAML("C page", "c.html"), map[string]string{"c.html": "<p>c</p>"})
	b := typedPackage("ns/b", "page", "1.0.0", pageYAML("B page", "b.html"), map[string]string{"b.html": "<p>b</p>"})
	v1 := typedPackage("ns/bundle", "bundle", "1.0.0", kitYAML("A page"), nil, "ns/a", "ns/c")
	other := typedPackage("ns/other", "bundle", "1.0.0", "", nil, "ns/c")

	bundle := h.install(&regID, "ns/bundle", nil, fetchFrom(v1, a, c))
	otherIP := h.install(&regID, "ns/other", nil, fetchFrom(other, c))
	aIP, cIP := h.installed(&regID, "ns/a"), h.installed(&regID, "ns/c")
	if !h.memberIDs(otherIP.ID)[*cIP.ID] {
		t.Fatal("other bundle did not link the shared member")
	}

	v2 := typedPackage("ns/bundle", "bundle", "2.0.0", kitYAML("B page"), nil, "ns/b")
	res, _, err := h.apply(bundle, v2, fetchFrom(v2, b))
	if err != nil {
		t.Fatalf("bundle update: %v", err)
	}

	if !h.isInstalled(&regID, "ns/b") {
		t.Fatal("new member not installed")
	}
	bIP := h.installed(&regID, "ns/b")
	if !bIP.InstalledAsDependency {
		t.Fatal("new member not marked as a dependency")
	}
	members := h.memberIDs(bundle.ID)
	if !members[*bIP.ID] || members[*aIP.ID] || members[*cIP.ID] || len(members) != 1 {
		t.Fatalf("bundle members after update = %v", members)
	}
	bPage, ok := h.pageIDByName("B page")
	if !ok {
		t.Fatal("new member page not created")
	}
	var rows []*database.InstalledResource
	all, _ := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, bundle.ID)
	for _, r := range all {
		if r.ResourceType == "campaigntemplate" {
			rows = append(rows, r)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("campaign template rows = %d", len(rows))
	}
	ct, err := h.s.CampaignTemplateRepository.GetByID(h.ctx, rows[0].ResourceID, &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	if lp, lerr := ct.LandingPageID.Get(); lerr != nil || lp.String() != bPage.String() {
		t.Fatal("template not rewired to the new member page")
	}

	if h.isInstalled(&regID, "ns/a") {
		t.Fatal("dropped member only this bundle used is still installed")
	}
	if _, ok := h.pageIDByName("A page"); ok {
		t.Fatal("dropped member page left behind")
	}
	if !h.isInstalled(&regID, "ns/c") {
		t.Fatal("dropped member another bundle links was removed")
	}
	if _, ok := h.pageIDByName("C page"); !ok {
		t.Fatal("shared member page removed")
	}
	if !h.memberIDs(otherIP.ID)[*cIP.ID] {
		t.Fatal("the other bundle lost its link")
	}
	joined := strings.Join(res.Skipped, "\n")
	if !strings.Contains(joined, "ns/a removed") || !strings.Contains(joined, "ns/c unlinked") {
		t.Fatalf("result does not report the dropped members: %v", res.Skipped)
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "2.0.0" {
		t.Fatalf("bundle version = %s", got)
	}
}

// TestUpdateBundleNewMemberFailureUndone adds a member whose page name a user
// page already holds. The update fails at write time and every step is undone:
// no member row, no link, no half installed content, bundle version unchanged.
func TestUpdateBundleNewMemberFailureUndone(t *testing.T) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/unused")
	regID := uuid.New()
	a := typedPackage("ns/a", "page", "1.0.0", pageYAML("A page", "a.html"), map[string]string{"a.html": "<p>a</p>"})
	v1 := typedPackage("ns/bundle", "bundle", "1.0.0", kitYAML("A page"), nil, "ns/a")
	bundle := h.install(&regID, "ns/bundle", nil, fetchFrom(v1, a))
	aIP := h.installed(&regID, "ns/a")

	if _, err := h.s.createPage(h.ctx, h.session, "Taken", "<p>user</p>"); err != nil {
		t.Fatalf("user page: %v", err)
	}
	d := typedPackage(
		"ns/d", "page", "1.0.0", pageYAML("D page", "d.html")+"  - name: Taken\n    file: d.html\n",
		map[string]string{"d.html": "<p>d</p>"},
	)
	v2 := typedPackage("ns/bundle", "bundle", "2.0.0", kitYAML("D page"), nil, "ns/d")
	_, ae, err := h.apply(bundle, v2, fetchFrom(v2, d))
	if err == nil {
		t.Fatal("update over a taken page name succeeded")
	}
	if ae.Details["rollback"] != "complete" {
		t.Fatalf("rollback = %v, want complete", ae.Details["rollback"])
	}
	if h.isInstalled(&regID, "ns/d") {
		t.Fatal("failed member left installed")
	}
	if _, ok := h.pageIDByName("D page"); ok {
		t.Fatal("failed member page left behind")
	}
	if ms := h.memberIDs(bundle.ID); len(ms) != 1 || !ms[*aIP.ID] {
		t.Fatalf("bundle members changed: %v", ms)
	}
	if !h.isInstalled(&regID, "ns/a") {
		t.Fatal("kept member removed by a failed update")
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "1.0.0" {
		t.Fatalf("bundle version advanced to %s", got)
	}

	// a new member missing from the download cache fails before any write.
	_, ae, err = h.apply(bundle, v2, fetchFrom(v2))
	if err == nil {
		t.Fatal("update without the new member downloaded succeeded")
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("missing download should fail before any write")
	}
}

// TestPrefetchUpdateCapsAndSlims checks the prefetch keeps existing members as
// manifest only and refuses new members whose payload exceeds the bundle limit.
func TestPrefetchUpdateCapsAndSlims(t *testing.T) {
	h := newRegFixture(t)
	bundle := h.withUpdateDeps("ns/bundle")
	if err := h.db.AutoMigrate(&database.InstalledPackageMember{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	memberID := uuid.New()
	member := h.insertPackage(&memberID, "ns/m")
	if err := h.s.RegistryRepository.InsertPackageMember(h.ctx, bundle.ID, member.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	big := make([]byte, (maxBundleBytes/2)+1)
	pkgs := map[string]*registry.VerifiedPackage{
		"ns/m":    {Manifest: &registry.Manifest{ID: "ns/m"}, Files: map[string][]byte{"content/x": []byte("x")}},
		"ns/big1": {Manifest: &registry.Manifest{ID: "ns/big1"}, Files: map[string][]byte{"content/big": big}},
		"ns/big2": {Manifest: &registry.Manifest{ID: "ns/big2"}, Files: map[string][]byte{"content/big": big}},
	}
	newCache := func() *packageCache {
		return newPackageCache(func(id string) (*registry.VerifiedPackage, error) {
			if p, ok := pkgs[id]; ok {
				return p, nil
			}
			return nil, errors.New("not found")
		})
	}

	small := typedPackage("ns/bundle", "bundle", "2.0.0", "", nil, "ns/m", "ns/big1")
	cache := newCache()
	if err := h.s.prefetchUpdate(h.ctx, bundle, small, cache); err != nil {
		t.Fatalf("prefetch within the limit: %v", err)
	}
	if p, _ := cache.get("ns/m"); !manifestOnly(p) || p.Manifest == nil {
		t.Fatal("existing member kept with its files")
	}
	if p, _ := cache.get("ns/big1"); manifestOnly(p) {
		t.Fatal("new member kept without its files")
	}

	tooBig := typedPackage("ns/bundle", "bundle", "2.0.0", "", nil, "ns/m", "ns/big1", "ns/big2")
	err := h.s.prefetchUpdate(h.ctx, bundle, tooBig, newCache())
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected the size limit error, got %v", err)
	}
}

// TestUpdateStaleAndUnreadableRows checks a provenance row whose page was
// deleted out of band survives an update that fails before writing and is
// replaced when one applies, and that a page that cannot be read fails the
// update with its row kept.
func TestUpdateStaleAndUnreadableRows(t *testing.T) {
	h := newRegFixture(t)
	ip := h.withUpdateDeps("ns/kit")
	pkg, docs := pageDocs("Login", "login.html", "<p>v1</p>")
	if _, err := h.reconcile(pkg, docs, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	pageID, _ := h.pageIDByName("Login")
	if err := h.db.Exec("DELETE FROM pages WHERE id = ?", pageID.String()).Error; err != nil {
		t.Fatalf("delete page: %v", err)
	}

	missingRef := yamlPackage(
		"ns/kit", "1.1.0",
		pageYAML("Login", "login.html")+"campaignTemplates:\n  - name: Kit\n    landingPage: Login\n    email: Nope\n",
		map[string]string{"login.html": "<p>v2</p>"},
	)
	if _, _, err := h.apply(ip, missingRef, nil); err == nil {
		t.Fatal("update with a missing reference succeeded")
	}
	rows := h.pageResources()
	if len(rows) != 1 || rows[0].ResourceID.String() != pageID.String() {
		t.Fatalf("stale row dropped by a failed update: %d rows", len(rows))
	}

	good := yamlPackage(
		"ns/kit", "1.1.0", pageYAML("Login", "login.html"), map[string]string{"login.html": "<p>v2</p>"},
	)
	if _, _, err := h.apply(ip, good, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	newID, ok := h.pageIDByName("Login")
	if !ok {
		t.Fatal("page not recreated")
	}
	rows = h.pageResources()
	if len(rows) != 1 || rows[0].ResourceID.String() != newID.String() {
		t.Fatalf("expected one row for the recreated page, got %d", len(rows))
	}

	// a read error is not a deleted entity: the update fails and the row stays.
	if err := h.db.Exec("ALTER TABLE pages RENAME TO pages_moved").Error; err != nil {
		t.Fatalf("rename table: %v", err)
	}
	v3 := yamlPackage("ns/kit", "1.2.0", pageYAML("Login", "login.html"), map[string]string{"login.html": "<p>v3</p>"})
	if _, _, err := h.apply(ip, v3, nil); err == nil || !strings.Contains(err.Error(), "read installed page") {
		t.Fatalf("expected a read error, got %v", err)
	}
	if err := h.db.Exec("ALTER TABLE pages_moved RENAME TO pages").Error; err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if rows := h.pageResources(); len(rows) != 1 || rows[0].ResourceID.String() != newID.String() {
		t.Fatal("row changed by an update that failed to read")
	}
}

// TestUpdateBundleRefusesTemplateOnDroppedMember updates a bundle to a version
// that drops a member while its campaign template still names the member's page.
// The plan phase refuses it before any write. When another bundle still links
// the member it stays installed, so the same update applies.
func TestUpdateBundleRefusesTemplateOnDroppedMember(t *testing.T) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/unused")
	regID := uuid.New()
	a := typedPackage("ns/a", "page", "1.0.0", pageYAML("A page", "a.html"), map[string]string{"a.html": "<p>a</p>"})
	b := typedPackage("ns/b", "page", "1.0.0", pageYAML("B page", "b.html"), map[string]string{"b.html": "<p>b</p>"})
	v1 := typedPackage("ns/bundle", "bundle", "1.0.0", kitYAML("A page"), nil, "ns/a")
	bundle := h.install(&regID, "ns/bundle", nil, fetchFrom(v1, a))
	aIP := h.installed(&regID, "ns/a")

	v2 := typedPackage("ns/bundle", "bundle", "2.0.0", kitYAML("A page"), nil, "ns/b")
	_, ae, err := h.apply(bundle, v2, fetchFrom(v2, b))
	if err == nil {
		t.Fatal("update naming a dropped member page succeeded")
	}
	if !strings.Contains(err.Error(), `"Kit"`) ||
		!strings.Contains(err.Error(), `"A page"`) ||
		!strings.Contains(err.Error(), "ns/a") {
		t.Fatalf("error does not name the template and entity: %v", err)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("refusal should come before any write")
	}
	if h.isInstalled(&regID, "ns/b") {
		t.Fatal("new member installed by a refused update")
	}
	if ms := h.memberIDs(bundle.ID); len(ms) != 1 || !ms[*aIP.ID] {
		t.Fatalf("bundle members changed: %v", ms)
	}
	if _, ok := h.pageIDByName("A page"); !ok {
		t.Fatal("dropped member page removed by a refused update")
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "1.0.0" {
		t.Fatalf("bundle version advanced to %s", got)
	}

	// another bundle links the member, so the update only unlinks it.
	other := typedPackage("ns/other", "bundle", "1.0.0", "", nil, "ns/a")
	h.install(&regID, "ns/other", nil, fetchFrom(other, a))
	if _, _, err := h.apply(bundle, v2, fetchFrom(v2, b)); err != nil {
		t.Fatalf("update with the member kept by another bundle: %v", err)
	}
	if !h.isInstalled(&regID, "ns/a") {
		t.Fatal("member another bundle links was removed")
	}
	if got := h.installed(&regID, "ns/bundle").Version; got != "2.0.0" {
		t.Fatalf("bundle version = %s", got)
	}
}

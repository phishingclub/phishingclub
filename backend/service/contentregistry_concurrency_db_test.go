package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
)

// fakeArchive is one package version a fake registry serves.
type fakeArchive struct {
	archive []byte
	sha     string
	version string
	hash    string
}

// buildFakeArchive packs files with a manifest that declares them, the way a
// published Bait Box package is laid out.
func buildFakeArchive(t *testing.T, id, pkgType string, deps []string, files map[string]string) fakeArchive {
	t.Helper()
	return buildFakeArchiveVars(t, id, pkgType, deps, nil, files)
}

// buildFakeArchiveVars is buildFakeArchive with declared variables.
func buildFakeArchiveVars(
	t *testing.T, id, pkgType string, deps []string, vars []registry.Variable, files map[string]string,
) fakeArchive {
	t.Helper()
	m := registry.Manifest{Format: "1", ID: id, Type: pkgType, Version: "1.0.0", Variables: vars}
	for _, d := range deps {
		m.Dependencies = append(m.Dependencies, registry.Dependency{ID: d})
	}
	for name, body := range files {
		sum := sha256.Sum256([]byte(body))
		m.Entries = append(m.Entries, registry.Entry{Path: name, SHA256: hex.EncodeToString(sum[:])})
	}
	m.ContentHash = registry.ComputeContentHash(m.Entries)
	mj, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, body []byte) {
		if err := tw.WriteHeader(
			&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg},
		); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	write(registry.ManifestFileName, mj)
	for name, body := range files {
		write(name, []byte(body))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return fakeArchive{archive: buf.Bytes(), sha: hex.EncodeToString(sum[:]), version: m.Version, hash: m.ContentHash}
}

// fakeRegistry serves the given packages over TLS. Downloads are slowed down so
// two installs that run at the same time overlap in the window between checking
// what is installed and writing it.
func fakeRegistry(t *testing.T, pkgs map[string]fakeArchive) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/packages/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(rest, "/")
		if len(parts) < 3 {
			http.NotFound(w, r)
			return
		}
		p, ok := pkgs[parts[0]+"/"+parts[1]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 4 && parts[3] == "download" {
			time.Sleep(50 * time.Millisecond)
			_, _ = w.Write(p.archive)
			return
		}
		_ = json.NewEncoder(w).Encode(
			registry.VersionResponse{Version: p.version, ContentHash: p.hash, ArchiveSHA: p.sha},
		)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestConcurrentBundleInstallsShareDependency installs two bundles that both
// reference the same package at the same time, then removes both at the same
// time. Run one at a time, the second install links the package the first one
// installed instead of inserting it again and failing on the unique index, and
// the second remove sees it is the last user and deletes it.
func TestConcurrentBundleInstallsShareDependency(t *testing.T) {
	h := newOwnershipFixture(t)
	if err := h.db.AutoMigrate(&database.InstalledPackageMember{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pkgs := map[string]fakeArchive{
		"acme/shared": buildFakeArchive(t, "acme/shared", "page", nil, map[string]string{
			"content/login/data.yaml":  "pages:\n  - name: Shared Login\n    file: index.html\n",
			"content/login/index.html": "<html>login</html>",
		}),
		"acme/b1": buildFakeArchive(
			t, "acme/b1", "bundle", []string{"acme/shared"}, map[string]string{"README.md": "one"},
		),
		"acme/b2": buildFakeArchive(
			t, "acme/b2", "bundle", []string{"acme/shared"}, map[string]string{"README.md": "two"},
		),
	}
	srv := fakeRegistry(t, pkgs)
	regID, err := h.s.RegistryRepository.InsertRegistry(
		h.ctx, &database.Registry{Name: "fake", URL: srv.URL, InsecureTLS: true},
	)
	if err != nil {
		t.Fatalf("insert registry: %v", err)
	}

	var wg sync.WaitGroup
	installErrs := make([]error, 2)
	for i, name := range []string{"b1", "b2"} {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			_, installErrs[i] = h.s.Install(h.ctx, h.session, regID, "acme", name, nil, nil)
		}(i, name)
	}
	wg.Wait()
	for i, e := range installErrs {
		if e != nil {
			t.Fatalf("install %d failed: %v", i, e)
		}
	}

	all, err := h.s.RegistryRepository.GetAllInstalledPackages(h.ctx)
	if err != nil {
		t.Fatalf("list installed: %v", err)
	}
	byID := map[string]*database.InstalledPackage{}
	for _, ip := range all {
		byID[ip.PackageID] = ip
	}
	if len(all) != 3 || byID["acme/shared"] == nil || byID["acme/b1"] == nil || byID["acme/b2"] == nil {
		t.Fatalf("expected b1, b2 and one shared install, got %d rows", len(all))
	}
	for _, b := range []string{"acme/b1", "acme/b2"} {
		members, err := h.s.RegistryRepository.GetPackageMembers(h.ctx, byID[b].ID)
		if err != nil {
			t.Fatalf("members of %s: %v", b, err)
		}
		if len(members) != 1 || *members[0] != *byID["acme/shared"].ID {
			t.Fatalf("%s should link the shared install once, got %v", b, members)
		}
	}
	if _, ok := h.pageIDByName("Shared Login"); !ok {
		t.Fatalf("shared page was not created")
	}

	// the preview for one bundle must leave out the shared package, since the
	// other bundle still uses it.
	preview, err := h.s.UninstallPreview(h.ctx, h.session, byID["acme/b1"].ID)
	if err != nil {
		t.Fatalf("uninstall preview: %v", err)
	}
	if len(preview.Members) != 0 {
		t.Fatalf("preview should not remove the shared package while b2 uses it, got %v", preview.Members)
	}

	removeErrs := make([]error, 2)
	for i, b := range []string{"acme/b1", "acme/b2"} {
		wg.Add(1)
		go func(i int, b string) {
			defer wg.Done()
			removeErrs[i] = h.s.Remove(h.ctx, h.session, byID[b].ID)
		}(i, b)
	}
	wg.Wait()
	for i, e := range removeErrs {
		if e != nil {
			t.Fatalf("remove %d failed: %v", i, e)
		}
	}
	left, err := h.s.RegistryRepository.GetAllInstalledPackages(h.ctx)
	if err != nil {
		t.Fatalf("list installed: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("expected nothing installed after removing both bundles, got %d rows", len(left))
	}
	if _, ok := h.pageIDByName("Shared Login"); ok {
		t.Fatalf("shared page should be deleted with the last bundle")
	}
}

// TestUninstallPreviewCampaignsAndEdits checks the uninstall preview names the
// running campaign whose template uses an installed page, skips a closed one,
// and flags the page once it was edited after install.
func TestUninstallPreviewCampaignsAndEdits(t *testing.T) {
	h := newOwnershipFixture(t)
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

	preview, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.CampaignCount != 0 || len(preview.Edited) != 0 {
		t.Fatalf("fresh install should affect nothing, got %+v", preview)
	}

	now := time.Now()
	tplID := uuid.New()
	identID := uuid.New()
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &tplID, CreatedAt: &now, UpdatedAt: &now, Name: "Tpl", LandingPageID: pageID,
		URLIdentifierID: &identID, StateIdentifierID: &identID,
	}).Error; err != nil {
		t.Fatalf("insert template: %v", err)
	}
	past := now.Add(-time.Hour)
	for _, c := range []struct {
		name     string
		closedAt *time.Time
	}{{"Running", nil}, {"Done", &past}} {
		id := uuid.New()
		row := &database.Campaign{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: c.name,
			CampaignTemplateID: &tplID, ClosedAt: c.closedAt,
		}
		if err := h.db.Create(row).Error; err != nil {
			t.Fatalf("insert campaign: %v", err)
		}
	}
	h.editPage(pageID, "<html>edited</html>")

	preview, err = h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.CampaignCount != 1 || len(preview.Campaigns) != 1 || preview.Campaigns[0].Name != "Running" {
		t.Fatalf("expected only the running campaign, got %+v", preview.Campaigns)
	}
	if len(preview.Edited) != 1 || preview.Edited[0].ResourceType != "page" || preview.Edited[0].Name != "Login" {
		t.Fatalf("expected the edited page, got %+v", preview.Edited)
	}
}

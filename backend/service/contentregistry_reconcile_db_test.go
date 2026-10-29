package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// regFixture wires a ContentRegistry over a throwaway sqlite database with the
// real Page entity service, so the generic reconcile driver runs against actual
// stored entities and provenance rows. Only the page type is wired; it is the
// simplest content type and exercises create, in place update, detach/rename and
// delete through the same generic path every other type uses.
type regFixture struct {
	t       *testing.T
	s       *ContentRegistry
	db      *gorm.DB
	ctx     context.Context
	session *model.Session
	ipID    *uuid.UUID
}

func newRegFixture(t *testing.T) *regFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "reconcile.db") + "?_foreign_keys=OFF"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// migrate the tables the page reconcile and its delete path touch. Foreign keys
	// stay off so AutoMigrate order and empty related tables never block an insert.
	if err := db.AutoMigrate(
		&database.Page{},
		&database.Campaign{},
		&database.CampaignTemplate{},
		&database.Registry{},
		&database.InstalledPackage{},
		&database.InstalledResource{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logg := zap.NewNop().Sugar()
	common := Common{Logger: logg}
	pageRepo := &repository.Page{DB: db}
	campaignRepo := &repository.Campaign{DB: db}
	ctRepo := &repository.CampaignTemplate{DB: db}
	regRepo := &repository.Registry{DB: db}
	tmpl := &Template{Common: common}
	ctSvc := &CampaignTemplate{
		Common:                     common,
		CampaignTemplateRepository: ctRepo,
		CampaignRepository:         campaignRepo,
	}
	pageSvc := &Page{
		Common:                  common,
		PageRepository:          pageRepo,
		CampaignRepository:      campaignRepo,
		CampaignTemplateService: ctSvc,
		TemplateService:         tmpl,
	}
	s := &ContentRegistry{
		Common:              common,
		RegistryRepository:  regRepo,
		Page:                pageSvc,
		PageRepository:      pageRepo,
		APISenderRepository: &repository.APISender{DB: db},
	}
	uid := uuid.New()
	ipID := uuid.New()
	session := &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nn(uid),
			Role: &model.Role{ID: uuid.New(), Name: data.RoleSuperAdministrator},
		},
	}
	return &regFixture{t: t, s: s, db: db, ctx: context.Background(), session: session, ipID: &ipID}
}

// pageDocs builds a one page package declaration pointing at an in archive file.
func pageDocs(name, file, content string) (*registry.VerifiedPackage, []contentDoc) {
	pkg := &registry.VerifiedPackage{Files: map[string][]byte{file: []byte(content)}}
	dy := &installDataYAML{}
	dy.Pages = []struct {
		Name string `yaml:"name"`
		File string `yaml:"file"`
	}{{Name: name, File: file}}
	return pkg, []contentDoc{{prefix: "", dy: dy}}
}

// emptyDocs is a package version that declares no pages, modelling a dropped
// upstream resource.
func emptyDocs() (*registry.VerifiedPackage, []contentDoc) {
	return &registry.VerifiedPackage{Files: map[string][]byte{}}, []contentDoc{{prefix: "", dy: &installDataYAML{}}}
}

// reconcile drives the same page sequence Update does: build the reconcile set,
// run the conflict pass, then add/update and remove. It returns the install
// result or the conflict or write error.
func (h *regFixture) reconcile(
	pkg *registry.VerifiedPackage,
	docs []contentDoc,
	resolutions map[string]string,
) (*InstallResult, error) {
	h.t.Helper()
	resources, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, h.ipID)
	if err != nil {
		return nil, err
	}
	undo := &updateUndo{}
	items, currents := h.s.buildPageReconcile(h.ctx, h.session, pkg, docs, map[string]string{}, resources, undo)
	var stale []*database.InstalledResource
	if err := takeStale(currents, &stale); err != nil {
		return nil, err
	}
	conflicts := reconcileConflicts("page", items, currents)
	if unresolved := unresolvedConflicts(conflicts, resolutions); len(unresolved) > 0 {
		return nil, ErrUpdateConflicts{Conflicts: unresolved}
	}
	result := &InstallResult{}
	for _, r := range stale {
		if err := h.s.deleteResourceTracked(h.ctx, r, undo); err != nil {
			undo.run()
			return nil, err
		}
	}
	if aerr := h.s.reconcileAddUpdate(
		h.ctx, h.session, h.ipID, "page", items, currents, resolutions, "{}", result, &result.Pages, undo,
	); aerr != nil {
		undo.run()
		return nil, aerr
	}
	h.s.reconcileRemove(h.ctx, h.ipID, "page", currents, result)
	return result, nil
}

// editPage simulates a local user editing a managed page's body.
func (h *regFixture) editPage(id *uuid.UUID, content string) {
	h.t.Helper()
	pm := &model.Page{}
	c := mustOptional1MB(h.t, content)
	pm.Content = nn(c)
	if err := h.s.Page.UpdateByID(h.ctx, h.session, id, pm); err != nil {
		h.t.Fatalf("edit page: %v", err)
	}
}

func (h *regFixture) pageIDByName(name string) (*uuid.UUID, bool) {
	h.t.Helper()
	id, err := h.s.resolvePageID(h.ctx, name)
	if err != nil {
		return nil, false
	}
	return id, true
}

func (h *regFixture) pageContent(id *uuid.UUID) string {
	h.t.Helper()
	p, err := h.s.PageRepository.GetByID(h.ctx, id, &repository.PageOption{})
	if err != nil {
		h.t.Fatalf("get page content: %v", err)
	}
	return pageContentOf(p)
}

// pageResources returns the page provenance rows for the install.
func (h *regFixture) pageResources() []*database.InstalledResource {
	h.t.Helper()
	all, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, h.ipID)
	if err != nil {
		h.t.Fatalf("list resources: %v", err)
	}
	var out []*database.InstalledResource
	for _, r := range all {
		if r.ResourceType == "page" {
			out = append(out, r)
		}
	}
	return out
}

func mustOptional1MB(t *testing.T, s string) vo.OptionalString1MB {
	t.Helper()
	v, err := vo.NewOptionalString1MB(s)
	if err != nil {
		t.Fatalf("content vo %q: %v", s, err)
	}
	return *v
}

// TestReconcilePageLifecycle walks one page through install, an unchanged update with a
// local edit (kept, no conflict), an upstream only change (applied silently), a
// three way conflict (raised, nothing applied) resolved keep then overwrite, the
// both resolution (local detached, fresh installed), and the dropped upstream
// cases (removed when clean, kept when locally edited).
func TestReconcilePageLifecycle(t *testing.T) {
	const (
		v1  = "<html><body>v1</body></html>"
		v1e = "<html><body>v1 edited</body></html>"
		v2  = "<html><body>v2</body></html>"
		v2e = "<html><body>v2 edited</body></html>"
		v3  = "<html><body>v3</body></html>"
		v3e = "<html><body>v3 edited</body></html>"
		v4  = "<html><body>v4</body></html>"
	)
	h := newRegFixture(t)

	// install: creates the page and one provenance row.
	pkg, docs := pageDocs("Login", "login.html", v1)
	result, err := h.reconcile(pkg, docs, nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(result.Pages) != 1 || result.Pages[0] != "Login" {
		t.Fatalf("install did not report the page: %+v", result.Pages)
	}
	id, ok := h.pageIDByName("Login")
	if !ok {
		t.Fatal("page Login not created")
	}
	if rs := h.pageResources(); len(rs) != 1 {
		t.Fatalf("expected 1 provenance row after install, got %d", len(rs))
	}
	baseline := h.pageResources()[0].InstalledContentHash

	// unchanged update with a local edit: upstream unchanged, so the edit is kept and
	// no conflict is raised.
	h.editPage(id, v1e)
	pkg, docs = pageDocs("Login", "login.html", v1)
	result, err = h.reconcile(pkg, docs, nil)
	if err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	if got := h.pageContent(id); got != v1e {
		t.Fatalf("no-op update did not keep local edit, content=%q", got)
	}
	if h.pageResources()[0].InstalledContentHash != baseline {
		t.Fatal("no-op update changed the recorded baseline")
	}
	if len(result.Pages) != 0 {
		t.Fatalf("no-op update reported a change: %+v", result.Pages)
	}

	// revert the local edit so the next case has an unedited baseline.
	h.editPage(id, v1)

	// upstream only change: applied silently in place, baseline advances.
	pkg, docs = pageDocs("Login", "login.html", v2)
	result, err = h.reconcile(pkg, docs, nil)
	if err != nil {
		t.Fatalf("upstream-only update: %v", err)
	}
	if got := h.pageContent(id); got != v2 {
		t.Fatalf("upstream-only update not applied, content=%q", got)
	}
	if len(result.Pages) != 1 {
		t.Fatalf("upstream-only update not reported: %+v", result.Pages)
	}
	baseV2 := h.pageResources()[0].InstalledContentHash
	if baseV2 == baseline {
		t.Fatal("upstream-only update did not advance the baseline")
	}

	// both changed: conflict raised, nothing applied.
	h.editPage(id, v2e)
	pkg, docs = pageDocs("Login", "login.html", v3)
	_, err = h.reconcile(pkg, docs, nil)
	conflicts, isConflict := err.(ErrUpdateConflicts)
	if !isConflict {
		t.Fatalf("expected ErrUpdateConflicts, got %v", err)
	}
	if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].SourceName != "Login" {
		t.Fatalf("unexpected conflict set: %+v", conflicts.Conflicts)
	}
	if got := h.pageContent(id); got != v2e {
		t.Fatalf("conflict should apply nothing, content=%q", got)
	}
	if h.pageResources()[0].InstalledContentHash != baseV2 {
		t.Fatal("conflict should not advance the baseline")
	}

	// resolve keep: local edit stays, baseline unchanged.
	result, err = h.reconcile(pkg, docs, map[string]string{"page:Login": resolveKeep})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if got := h.pageContent(id); got != v2e {
		t.Fatalf("keep did not preserve the local edit, content=%q", got)
	}
	if h.pageResources()[0].InstalledContentHash != baseV2 {
		t.Fatal("keep should not advance the baseline")
	}
	if !containsSuffix(result.Skipped, "kept (local edit)") {
		t.Fatalf("keep not reported in skipped: %+v", result.Skipped)
	}

	// resolve overwrite: upstream replaces the local edit in place, baseline advances.
	result, err = h.reconcile(pkg, docs, map[string]string{"page:Login": resolveOverwrite})
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := h.pageContent(id); got != v3 {
		t.Fatalf("overwrite did not apply upstream, content=%q", got)
	}
	baseV3 := h.pageResources()[0].InstalledContentHash
	if baseV3 == baseV2 {
		t.Fatal("overwrite did not advance the baseline")
	}
	if h.pageResources()[0].ResourceID.String() != id.String() {
		t.Fatal("overwrite should keep the same entity id")
	}

	// resolve both: detach the local edit to a renamed copy, install fresh upstream.
	h.editPage(id, v3e)
	pkg, docs = pageDocs("Login", "login.html", v4)
	if _, err := h.reconcile(pkg, docs, map[string]string{"page:Login": resolveBoth}); err != nil {
		t.Fatalf("both: %v", err)
	}
	newID, ok := h.pageIDByName("Login")
	if !ok {
		t.Fatal("both: fresh Login page not created")
	}
	if newID.String() == id.String() {
		t.Fatal("both: fresh page should be a new entity")
	}
	if got := h.pageContent(newID); got != v4 {
		t.Fatalf("both: fresh page content=%q, want v4", got)
	}
	// the old copy still exists under a different name with its local edit, untracked.
	if got := h.pageContent(id); got != v3e {
		t.Fatalf("both: detached copy lost its edit, content=%q", got)
	}
	rs := h.pageResources()
	if len(rs) != 1 || rs[0].ResourceID.String() != newID.String() {
		t.Fatalf("both: expected one provenance row for the fresh page, got %+v", rs)
	}

	// dropped upstream, clean: the tracked page is removed, its provenance gone.
	epkg, edocs := emptyDocs()
	result, err = h.reconcile(epkg, edocs, nil)
	if err != nil {
		t.Fatalf("drop clean: %v", err)
	}
	if _, ok := h.pageIDByName("Login"); ok {
		t.Fatal("drop clean: page was not removed")
	}
	if rs := h.pageResources(); len(rs) != 0 {
		t.Fatalf("drop clean: provenance not removed, got %d", len(rs))
	}
	if !containsSuffix(result.Skipped, "removed (no longer in package)") {
		t.Fatalf("drop clean not reported: %+v", result.Skipped)
	}

	// dropped upstream, locally edited: the page is kept standalone, provenance gone.
	pkg, docs = pageDocs("Keep", "keep.html", v1)
	if _, err := h.reconcile(pkg, docs, nil); err != nil {
		t.Fatalf("install Keep: %v", err)
	}
	keepID, ok := h.pageIDByName("Keep")
	if !ok {
		t.Fatal("Keep page not created")
	}
	h.editPage(keepID, v1e)
	epkg, edocs = emptyDocs()
	result, err = h.reconcile(epkg, edocs, nil)
	if err != nil {
		t.Fatalf("drop edited: %v", err)
	}
	if _, ok := h.pageIDByName("Keep"); !ok {
		t.Fatal("drop edited: locally edited page should be kept")
	}
	// a locally edited, dropped resource keeps its entity AND its provenance row
	// (reconcileRemove continues past the delete), so it stays tracked.
	if rs := h.pageResources(); len(rs) != 1 {
		t.Fatalf("drop edited: expected the Keep provenance row to remain, got %d", len(rs))
	}
	if !containsSuffix(result.Skipped, "kept (local edit; dropped upstream)") {
		t.Fatalf("drop edited not reported: %+v", result.Skipped)
	}
}

// containsSuffix reports whether any entry ends with suffix.
func containsSuffix(entries []string, suffix string) bool {
	for _, e := range entries {
		if len(e) >= len(suffix) && e[len(e)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

// TestNameTakenFailsClosed checks that a type without a name check, and a failed
// check, count as taken, and that a free page name does not.
func TestNameTakenFailsClosed(t *testing.T) {
	h := newRegFixture(t)
	if h.s.nameTaken(h.ctx, "page", "Nobody uses this") {
		t.Fatal("free page name reported as taken")
	}
	if !h.s.nameTaken(h.ctx, "smtp", "Nobody uses this") {
		t.Fatal("unchecked type reported as free")
	}
	sqlDB, err := h.db.DB()
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	_ = sqlDB.Close()
	if !h.s.nameTaken(h.ctx, "page", "Nobody uses this") {
		t.Fatal("failed check reported as free")
	}
}

// TestKeepBothSkipsCompanyNames checks that the keep both rename treats a name a
// company page uses as taken, so the renamed local copy never collides with it.
func TestKeepBothSkipsCompanyNames(t *testing.T) {
	h := newRegFixture(t)
	now := time.Now()
	companyID := uuid.New()
	pid := uuid.New()
	company := &database.Page{ID: &pid, CreatedAt: &now, UpdatedAt: &now, CompanyID: &companyID, Name: "Login (local)"}
	if err := h.db.Create(company).Error; err != nil {
		t.Fatalf("company page: %v", err)
	}
	if got := h.s.freeName(h.ctx, "page", "Login (local)"); got != "Login (local) (1)" {
		t.Fatalf("freeName = %q, want %q", got, "Login (local) (1)")
	}

	pkg, docs := pageDocs("Login", "login.html", "<html>v1</html>")
	if _, err := h.reconcile(pkg, docs, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	id, ok := h.pageIDByName("Login")
	if !ok {
		t.Fatal("page not installed")
	}
	h.editPage(id, "<html>local</html>")
	pkg, docs = pageDocs("Login", "login.html", "<html>v2</html>")
	if _, err := h.reconcile(pkg, docs, map[string]string{"page:Login": resolveBoth}); err != nil {
		t.Fatalf("both with a company page named like the local copy: %v", err)
	}
	p, err := h.s.PageRepository.GetByID(h.ctx, id, &repository.PageOption{})
	if err != nil {
		t.Fatalf("read local copy: %v", err)
	}
	if got := p.Name.MustGet().String(); got != "Login (local) (1)" {
		t.Fatalf("local copy name = %q, want %q", got, "Login (local) (1)")
	}
}

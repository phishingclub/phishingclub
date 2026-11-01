package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// withUpdateDeps extends the page fixture with what applyUpdate and campaign
// templates touch: identifiers, domains, package members and the campaign
// template service, plus the installed package row for the fixture install.
func (h *regFixture) withUpdateDeps(packageID string) *database.InstalledPackage {
	h.t.Helper()
	if err := h.db.AutoMigrate(
		&database.Identifier{},
		&database.Domain{},
		&database.InstalledPackageMember{},
		&database.Email{},
	); err != nil {
		h.t.Fatalf("migrate: %v", err)
	}
	identRepo := &repository.Identifier{DB: h.db}
	for _, n := range []string{"id", "p"} {
		id := uuid.New()
		if err := h.db.Create(&database.Identifier{ID: &id, Name: n}).Error; err != nil {
			h.t.Fatalf("identifier %s: %v", n, err)
		}
	}
	ctRepo := &repository.CampaignTemplate{DB: h.db}
	h.s.CampaignTemplateRepository = ctRepo
	h.s.CampaignTemplate = &CampaignTemplate{
		Common:                     h.s.Common,
		CampaignTemplateRepository: ctRepo,
		CampaignRepository:         &repository.Campaign{DB: h.db},
		IdentifierRepository:       identRepo,
	}
	h.s.IdentifierRepository = identRepo
	h.s.DomainRepository = &repository.Domain{DB: h.db}
	h.s.EmailRepository = &repository.Email{DB: h.db}
	return h.insertPackage(h.ipID, packageID)
}

func (h *regFixture) insertPackage(id *uuid.UUID, packageID string) *database.InstalledPackage {
	h.t.Helper()
	regID := uuid.New()
	ip := &database.InstalledPackage{
		ID: id, RegistryID: &regID, PackageID: packageID, PackageType: "bundle", Version: "1.0.0", ContentHash: "h1",
	}
	if err := h.db.Create(ip).Error; err != nil {
		h.t.Fatalf("installed package: %v", err)
	}
	return ip
}

// yamlPackage builds a verified package with one content folder holding the
// given data.yaml and files.
func yamlPackage(id, version, dataYAML string, files map[string]string) *registry.VerifiedPackage {
	pkg := &registry.VerifiedPackage{
		Manifest: &registry.Manifest{ID: id, Version: version, ContentHash: "h-" + version},
		Files:    map[string][]byte{"content/main/data.yaml": []byte(dataYAML)},
	}
	for k, v := range files {
		pkg.Files["content/main/"+k] = []byte(v)
	}
	return pkg
}

func (h *regFixture) apply(
	ip *database.InstalledPackage,
	pkg *registry.VerifiedPackage,
	fetch func(string) (*registry.VerifiedPackage, error),
) (*InstallResult, *AuditEvent, error) {
	ae := NewAuditEvent("Registry.PackageUpdate", h.session)
	res, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetch, nil, nil, nil)
	return res, ae, err
}

func (h *regFixture) campaignTemplateRows() []*database.InstalledResource {
	h.t.Helper()
	all, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, h.ipID)
	if err != nil {
		h.t.Fatalf("list resources: %v", err)
	}
	var out []*database.InstalledResource
	for _, r := range all {
		if r.ResourceType == "campaigntemplate" {
			out = append(out, r)
		}
	}
	return out
}

// TestUpdateTemplateFailureLeavesNoFalseLocalEdit updates a page in place and then
// fails at the campaign template step, once in the plan phase (a reference that
// does not exist) and once at write time (a template name already taken). Either
// way the page keeps its content and its baseline, so a retry is not mistaken for
// a local edit and applies cleanly.
func TestUpdateTemplateFailureLeavesNoFalseLocalEdit(t *testing.T) {
	const (
		v1 = "<html><body>v1</body></html>"
		v2 = "<html><body>v2</body></html>"
	)
	h := newRegFixture(t)
	ip := h.withUpdateDeps("ns/kit")

	pkg, docs := pageDocs("Login", "login.html", v1)
	if _, err := h.reconcile(pkg, docs, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	pageID, ok := h.pageIDByName("Login")
	if !ok {
		t.Fatal("page Login not created")
	}
	baseline := h.pageResources()[0].InstalledContentHash

	assertUntouched := func(step string) {
		t.Helper()
		if got := h.pageContent(pageID); got != v1 {
			t.Fatalf("%s: page content changed to %q", step, got)
		}
		if got := h.pageResources()[0].InstalledContentHash; got != baseline {
			t.Fatalf("%s: page baseline changed", step)
		}
		if got := h.s.storedPageHash(h.ctx, pageID); got != baseline {
			t.Fatalf("%s: stored page no longer matches its baseline (false local edit)", step)
		}
		if rows := h.campaignTemplateRows(); len(rows) != 0 {
			t.Fatalf("%s: campaign template provenance left behind: %d", step, len(rows))
		}
	}

	// plan phase failure: the template names an email that does not exist.
	missingRef := yamlPackage("ns/kit", "1.1.0", `
pages:
  - name: Login
    file: login.html
campaignTemplates:
  - name: Kit
    landingPage: Login
    email: Nope
`, map[string]string{"login.html": v2})
	_, ae, err := h.apply(ip, missingRef, nil)
	if err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("expected a missing reference error, got %v", err)
	}
	if _, wrote := ae.Details["rollback"]; wrote {
		t.Fatal("plan phase failure should fail before any write")
	}
	assertUntouched("plan failure")

	// write time failure: a campaign template named Kit already exists and is not
	// managed by this install, so creating the package's template fails after the
	// page was updated in place.
	taken := &model.CampaignTemplate{}
	n, _ := vo.NewString64("Kit")
	taken.Name = nn(*n)
	taken.LandingPageID = nn(*pageID)
	if _, err := h.s.CampaignTemplate.Create(h.ctx, h.session, taken); err != nil {
		t.Fatalf("create unmanaged template: %v", err)
	}
	collide := yamlPackage("ns/kit", "1.1.0", `
pages:
  - name: Login
    file: login.html
campaignTemplates:
  - name: Kit
    landingPage: Login
`, map[string]string{"login.html": v2})
	_, ae, err = h.apply(ip, collide, nil)
	if err == nil {
		t.Fatal("expected the campaign template create to fail")
	}
	if ae.Details["rollback"] != "complete" {
		t.Fatalf("expected a complete rollback, got %v", ae.Details["rollback"])
	}
	assertUntouched("write failure")

	// retry with a free template name: no conflict is raised for the page, the new
	// content applies and the template is created and wired to it.
	ok2 := yamlPackage("ns/kit", "1.1.0", `
pages:
  - name: Login
    file: login.html
campaignTemplates:
  - name: Kit Two
    landingPage: Login
`, map[string]string{"login.html": v2})
	res, _, err := h.apply(ip, ok2, nil)
	if err != nil {
		var uc ErrUpdateConflicts
		if errors.As(err, &uc) {
			t.Fatalf("retry raised a false local edit conflict: %+v", uc.Conflicts)
		}
		t.Fatalf("retry: %v", err)
	}
	if got := h.pageContent(pageID); got != v2 {
		t.Fatalf("retry did not apply the page, content=%q", got)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("retry did not report the page: %+v", res.Pages)
	}
	rows := h.campaignTemplateRows()
	if len(rows) != 1 {
		t.Fatalf("expected one campaign template row, got %d", len(rows))
	}
	ct, err := h.s.CampaignTemplateRepository.GetByID(h.ctx, rows[0].ResourceID, &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if lp, lerr := ct.LandingPageID.Get(); lerr != nil || lp.String() != pageID.String() {
		t.Fatalf("template not wired to the page: %v", lp)
	}
}

// TestUpdateBundleReusedMemberResolvesDomain installs a bundle whose campaign
// template names a domain through @@DOMAIN@@, a variable only the reused domain
// member declares. Update must resolve the token from the member's own install,
// so an unchanged template does not read as changed upstream and a changed one
// is rewired to the member's domain, with and without the member manifest.
func TestUpdateBundleReusedMemberResolvesDomain(t *testing.T) {
	const domainName = "shop.example.test"
	h := newRegFixture(t)
	bundle := h.withUpdateDeps("ns/bundle")

	dm, err := buildDomainModel(domainName, false, false, false, false, "", "", "", "", "")
	if err != nil {
		t.Fatalf("domain model: %v", err)
	}
	domainID, err := h.s.DomainRepository.Insert(h.ctx, dm)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	// the domain member was installed earlier on its own and is reused by the bundle.
	memberID := uuid.New()
	member := h.insertPackage(&memberID, "ns/domain")
	if _, err := h.s.RegistryRepository.InsertInstalledResource(h.ctx, &database.InstalledResource{
		InstalledPackageID: member.ID, ResourceType: "domain", ResourceID: domainID,
		SourceName: domainName, InstalledContentHash: "d", BoundVariables: `{"DOMAIN":"` + domainName + `"}`,
	}); err != nil {
		t.Fatalf("member resource: %v", err)
	}
	if err := h.s.RegistryRepository.InsertPackageMember(h.ctx, bundle.ID, member.ID); err != nil {
		t.Fatalf("member link: %v", err)
	}
	memberPkg := &registry.VerifiedPackage{Manifest: &registry.Manifest{
		ID: "ns/domain", Version: "1.0.0",
		Variables: []registry.Variable{{Name: "DOMAIN", Type: "domain", Required: true}},
	}}
	fetch := func(id string) (*registry.VerifiedPackage, error) {
		if id == "ns/domain" {
			return memberPkg, nil
		}
		return nil, errors.New("not found")
	}

	if _, err := h.s.createPage(h.ctx, h.session, "Landing", "<html>landing</html>"); err != nil {
		t.Fatalf("landing page: %v", err)
	}

	// redirect stands in for any change to the template declaration.
	ctYAML := func(redirect string) string {
		return `
campaignTemplates:
  - name: Shop
    domain: "@@DOMAIN@@"
    landingPage: Landing
    afterLandingPageRedirectURL: ` + redirect + `
`
	}

	// the install as Install records it: the template built with the member values
	// merged in, and the bundle bound nothing because the member was reused.
	merged, err := h.s.memberVarsFor(h.ctx, fetch, []*uuid.UUID{member.ID})
	if err != nil {
		t.Fatalf("member values: %v", err)
	}
	if merged["DOMAIN"] != domainName {
		t.Fatalf("member values not read: %+v", merged)
	}
	v1 := yamlPackage("ns/bundle", "1.0.0", ctYAML("https://a.example.test"), nil)
	v1docs, err := contentDocs(v1)
	if err != nil {
		t.Fatalf("docs: %v", err)
	}
	ct1 := v1docs[0].dy.CampaignTemplates[0]
	m, err := h.s.buildCampaignTemplateModel(h.ctx, ct1, merged)
	if err != nil {
		t.Fatalf("install template: %v", err)
	}
	ctID, err := h.s.CampaignTemplate.Create(h.ctx, h.session, m)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := h.s.recordResource(
		h.ctx, bundle.ID, "campaigntemplate", ct1.Name, ctID, campaignTemplateDeclFingerprint(ct1, merged), "{}",
	); err != nil {
		t.Fatalf("record template: %v", err)
	}

	// each bundle version keeps referencing the domain member.
	bundleV := func(version, redirect string) *registry.VerifiedPackage {
		p := yamlPackage("ns/bundle", version, ctYAML(redirect), nil)
		p.Manifest.Dependencies = []registry.Dependency{{ID: "ns/domain"}}
		return p
	}

	// unchanged upstream: nothing is rewired and nothing is reported.
	res, _, err := h.apply(bundle, bundleV("1.0.1", "https://a.example.test"), fetch)
	if err != nil {
		t.Fatalf("unchanged update: %v", err)
	}
	if len(res.Others) != 0 {
		t.Fatalf("unchanged template read as changed upstream: %+v", res.Others)
	}

	checkWired := func(step string, redirect string) {
		t.Helper()
		cur, err := h.s.CampaignTemplateRepository.GetByID(h.ctx, ctID, &repository.CampaignTemplateOption{})
		if err != nil {
			t.Fatalf("%s: get template: %v", step, err)
		}
		if d, derr := cur.DomainID.Get(); derr != nil || d.String() != domainID.String() {
			t.Fatalf("%s: template domain not the member domain", step)
		}
		if u, _ := cur.AfterLandingPageRedirectURL.Get(); u.String() != redirect {
			t.Fatalf("%s: template change not applied", step)
		}
	}

	// changed upstream: the template is rewired with @@DOMAIN@@ resolved.
	res, _, err = h.apply(bundle, bundleV("1.0.2", "https://b.example.test"), fetch)
	if err != nil {
		t.Fatalf("changed update: %v", err)
	}
	if len(res.Others) != 1 {
		t.Fatalf("changed template not reported: %+v", res.Others)
	}
	checkWired("with manifest", "https://b.example.test")

	// the member manifest cannot be fetched: the bound values alone still resolve it.
	noFetch := func(string) (*registry.VerifiedPackage, error) { return nil, errors.New("offline") }
	if _, _, err := h.apply(bundle, bundleV("1.0.3", "https://a.example.test"), noFetch); err != nil {
		t.Fatalf("update without member manifest: %v", err)
	}
	checkWired("bound values only", "https://a.example.test")
}

// TestCompanyDomainRefused: a package campaign template binds only to a global
// domain. A domain owned by a company is refused with a clear error at install
// and in the update preflight, never bound.
func TestCompanyDomainRefused(t *testing.T) {
	h := newRegFixture(t)
	h.withUpdateDeps("ns/bundle")
	now := time.Now()
	companyID := uuid.New()
	globalID, ownedID := uuid.New(), uuid.New()
	for _, d := range []*database.Domain{
		{ID: globalID, CreatedAt: &now, UpdatedAt: &now, Name: "global.example.test"},
		{ID: ownedID, CreatedAt: &now, UpdatedAt: &now, Name: "owned.example.test", CompanyID: &companyID},
	} {
		if err := h.db.Create(d).Error; err != nil {
			t.Fatalf("domain %s: %v", d.Name, err)
		}
	}

	if id, err := h.s.resolveDomainID(h.ctx, "global.example.test"); err != nil || *id != globalID {
		t.Fatalf("global domain: id %v err %v", id, err)
	}
	if id, err := h.s.resolveDomainID(h.ctx, "owned.example.test"); !errors.Is(err, errCompanyDomain) || id != nil {
		t.Fatalf("company domain resolved: id %v err %v", id, err)
	}

	ct := installCampaignTemplateYAML{Name: "Tmpl", Domain: "owned.example.test"}
	_, err := h.s.buildCampaignTemplateModel(h.ctx, ct, map[string]string{})
	if err == nil || !isValidation(err) || !strings.Contains(err.Error(), "belongs to a company") {
		t.Fatalf("install: expected the company domain error, got %v", err)
	}
	err = h.s.checkCampaignTemplateRefs(h.ctx, ct, map[string]string{}, declaredNames{})
	if err == nil || !strings.Contains(err.Error(), "belongs to a company") {
		t.Fatalf("update preflight: expected the company domain error, got %v", err)
	}
	// a missing domain keeps the plain not available error.
	ct.Domain = "missing.example.test"
	err = h.s.checkCampaignTemplateRefs(h.ctx, ct, map[string]string{}, declaredNames{})
	if err == nil || !strings.Contains(err.Error(), "is not available") {
		t.Fatalf("missing domain: got %v", err)
	}
}

package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newForeignKeyRegFixture is the reconcile fixture over a database with foreign
// keys on, as the server runs it, with the oauth provider and api sender
// services wired.
func newForeignKeyRegFixture(t *testing.T) *regFixture {
	t.Helper()
	h := newRegFixture(t)
	dsn := filepath.Join(t.TempDir(), "fk.db") + "?_foreign_keys=ON"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// one connection, so the pragmas below apply to every query.
	sqlDB.SetMaxOpenConns(1)
	// migrate with foreign keys off the way the server does, then turn them on.
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("pragma off: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Page{},
		&database.Email{},
		&database.Domain{},
		&database.Identifier{},
		&database.OAuthProvider{},
		&database.OAuthState{},
		&database.APISender{},
		&database.APISenderHeader{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.Registry{},
		&database.InstalledPackage{},
		&database.InstalledResource{},
		&database.InstalledPackageMember{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("pragma on: %v", err)
	}
	var on int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&on).Error; err != nil || on != 1 {
		t.Fatalf("foreign keys not on: %d %v", on, err)
	}

	common := h.s.Common
	ctRepo := &repository.CampaignTemplate{DB: db}
	ctSvc := &CampaignTemplate{
		Common:                     common,
		CampaignTemplateRepository: ctRepo,
		CampaignRepository:         &repository.Campaign{DB: db},
		IdentifierRepository:       &repository.Identifier{DB: db},
	}
	oauthRepo := &repository.OAuthProvider{DB: db}
	oauthSvc := &OAuthProvider{
		Common:                  common,
		OAuthProviderRepository: oauthRepo,
		OAuthStateRepository:    &repository.OAuthState{DB: db},
	}
	senderRepo := &repository.APISender{DB: db}
	pageRepo := &repository.Page{DB: db}
	h.db = db
	h.s.RegistryRepository = &repository.Registry{DB: db}
	h.s.PageRepository = pageRepo
	h.s.Page = &Page{
		Common:                  common,
		PageRepository:          pageRepo,
		CampaignRepository:      ctSvc.CampaignRepository,
		CampaignTemplateService: ctSvc,
		TemplateService:         &Template{Common: common},
	}
	h.s.CampaignTemplate = ctSvc
	h.s.CampaignTemplateRepository = ctRepo
	h.s.IdentifierRepository = ctSvc.IdentifierRepository
	h.s.DomainRepository = &repository.Domain{DB: db}
	h.s.EmailRepository = &repository.Email{DB: db}
	h.s.OAuthProvider = oauthSvc
	h.s.OAuthProviderRepository = oauthRepo
	h.s.APISenderRepository = senderRepo
	h.s.APISender = &APISender{
		Common:                  common,
		TemplateService:         &Template{Common: common},
		CampaignTemplateService: ctSvc,
		APISenderRepository:     senderRepo,
		OAuthProviderService:    oauthSvc,
	}
	return h
}

// oauthSenderPackage ships an oauth provider and an api sender that names it.
func oauthSenderPackage() *registry.VerifiedPackage {
	return typedPackage("ns/graph", "apisender", "1.0.0", `
oauthProviders:
  - name: Graph
    authURL: https://login.example.test/authorize
    tokenURL: https://login.example.test/token
    scopes: mail.send
    clientID: client
    clientSecret: secret
apiSenders:
  - name: Graph Sender
    requestMethod: POST
    requestURL: https://graph.example.test/send
    requestBody: "{}"
    expectedResponseStatusCode: 202
    oauthProvider: Graph
`, nil)
}

// assertOAuthSenderGone checks the provider, the sender and every provenance row
// of the install are gone.
func (h *regFixture) assertOAuthSenderGone(step string, ipID *uuid.UUID) {
	h.t.Helper()
	var providers, senders int64
	h.db.Model(&database.OAuthProvider{}).Count(&providers)
	h.db.Model(&database.APISender{}).Count(&senders)
	if providers != 0 || senders != 0 {
		h.t.Fatalf("%s: left %d oauth providers and %d api senders", step, providers, senders)
	}
	rows, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ipID)
	if err != nil {
		h.t.Fatalf("%s: list resources: %v", step, err)
	}
	if len(rows) != 0 {
		h.t.Fatalf("%s: %d provenance rows left", step, len(rows))
	}
	if _, err := h.s.RegistryRepository.GetInstalledPackageByID(h.ctx, ipID); err == nil {
		h.t.Fatalf("%s: installed package row left", step)
	}
}

// TestRemoveOAuthProviderWithOwnSender installs a package with an oauth provider
// and an api sender that points at it, with foreign keys on. The provider is
// recorded first, so deleting in that order fails on the sender's key. Uninstall
// and an install rollback must both delete the sender first and finish in one go.
func TestRemoveOAuthProviderWithOwnSender(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	pkg := oauthSenderPackage()

	ip := h.install(&regID, "ns/graph", nil, fetchFrom(pkg))
	rows, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ip.ID)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(rows) != 2 || rows[0].ResourceType != "oauthprovider" || rows[1].ResourceType != "apisender" {
		t.Fatalf("expected the provider recorded before the sender, got %d rows", len(rows))
	}
	if err := h.s.remove(h.ctx, h.session, ip.ID); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	h.assertOAuthSenderGone("uninstall", ip.ID)

	ip = h.install(&regID, "ns/graph", nil, fetchFrom(pkg))
	if !h.s.rollbackInstall(h.ctx, h.session, ip.ID, false) {
		t.Fatal("rollback reported leftovers")
	}
	h.assertOAuthSenderGone("rollback", ip.ID)
}

const promoMailYAML = `
emails:
  - name: Promo
    file: mail.html
    subject: Hello
    from: news@example.test
    envelope from: news@example.test
`

// installPromoMail installs the Promo email through an update of an empty
// install and returns its id.
func (h *ownershipFixture) installPromoMail(ip *database.InstalledPackage) *uuid.UUID {
	h.t.Helper()
	v1 := yamlPackage(
		"ns/mail", "1.0.0", promoMailYAML, map[string]string{"mail.html": "<html><body>promo</body></html>"},
	)
	if _, _, err := h.apply(ip, v1, nil); err != nil {
		h.t.Fatalf("install email: %v", err)
	}
	id, err := h.s.resolveEmailID(h.ctx, "Promo")
	if err != nil {
		h.t.Fatalf("email not created: %v", err)
	}
	return id
}

func (h *ownershipFixture) emailRows(ipID *uuid.UUID) int {
	h.t.Helper()
	rows, err := h.s.RegistryRepository.GetInstalledResourcesByPackage(h.ctx, ipID)
	if err != nil {
		h.t.Fatalf("list resources: %v", err)
	}
	n := 0
	for _, r := range rows {
		if r.ResourceType == "email" {
			n++
		}
	}
	return n
}

func hasSkipped(res *InstallResult, want string) bool {
	for _, s := range res.Skipped {
		if s == want {
			return true
		}
	}
	return false
}

// TestUpdateDroppedEmailUsedByTemplateIsKept drops an email the user's own
// campaign template uses. The update keeps the email, stops tracking it and
// says why, and the template still points at it.
func TestUpdateDroppedEmailUsedByTemplateIsKept(t *testing.T) {
	h := newOwnershipFixture(t)
	ip := h.withUpdateDeps("ns/mail")
	h.withEmailService()
	emailID := h.installPromoMail(ip)

	now := time.Now()
	ctID := uuid.New()
	urlID := uuid.New()
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &ctID, CreatedAt: &now, UpdatedAt: &now, Name: "Operator",
		URLIdentifierID: &urlID, EmailID: emailID,
	}).Error; err != nil {
		t.Fatalf("operator template: %v", err)
	}

	v2 := yamlPackage("ns/mail", "1.1.0", "", nil)
	res, _, err := h.apply(ip, v2, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	want := `email Promo kept (no longer in package, still used by campaign template "Operator")`
	if !hasSkipped(res, want) {
		t.Fatalf("missing kept message, got %q", res.Skipped)
	}
	if !h.emailExists("Promo") {
		t.Fatal("used email was deleted")
	}
	if n := h.emailRows(ip.ID); n != 0 {
		t.Fatalf("kept email is still tracked: %d rows", n)
	}
	ct, err := h.s.CampaignTemplateRepository.GetByID(h.ctx, &ctID, &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if got, gerr := ct.EmailID.Get(); gerr != nil || got.String() != emailID.String() {
		t.Fatal("operator template lost its email")
	}

	// the email is the user's own now, so uninstalling the package leaves it.
	if err := h.s.remove(h.ctx, h.session, ip.ID); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !h.emailExists("Promo") {
		t.Fatal("uninstall deleted the untracked email")
	}
}

// TestUpdateDroppedUnusedEmailIsRemoved drops an email nothing else uses. The
// update deletes it and its provenance row.
func TestUpdateDroppedUnusedEmailIsRemoved(t *testing.T) {
	h := newOwnershipFixture(t)
	ip := h.withUpdateDeps("ns/mail")
	h.withEmailService()
	h.installPromoMail(ip)

	v2 := yamlPackage("ns/mail", "1.1.0", "", nil)
	res, _, err := h.apply(ip, v2, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !hasSkipped(res, "email Promo removed (no longer in package)") {
		t.Fatalf("missing removed message, got %q", res.Skipped)
	}
	if h.emailExists("Promo") {
		t.Fatal("unused dropped email was kept")
	}
	if n := h.emailRows(ip.ID); n != 0 {
		t.Fatalf("provenance left for the removed email: %d rows", n)
	}
	if strings.Contains(strings.Join(res.Skipped, "\n"), "kept") {
		t.Fatalf("unexpected kept message: %q", res.Skipped)
	}
}

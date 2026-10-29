package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testAdminHost = "admin.example.test"

type domainFixture struct {
	t       *testing.T
	db      *gorm.DB
	ctx     context.Context
	session *model.Session
	domain  *Domain
	proxy   *Proxy
	oauth   *OAuthProvider
}

func newDomainFixture(t *testing.T) *domainFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "domain.db") + "?_foreign_keys=OFF"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Domain{}, &database.OAuthState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	common := Common{Logger: zap.NewNop().Sugar()}
	domainRepo := &repository.Domain{DB: db}
	domainSvc := &Domain{
		Common:           common,
		DomainRepository: domainRepo,
		TemplateService:  &Template{Common: common},
		AdminHosts:       func() []string { return []string{testAdminHost} },
	}
	session := &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(uuid.New()),
			Role: &model.Role{ID: uuid.New(), Name: data.RoleSuperAdministrator},
		},
	}
	return &domainFixture{
		t:       t,
		db:      db,
		ctx:     context.Background(),
		session: session,
		domain:  domainSvc,
		proxy: &Proxy{
			Common:           common,
			DomainRepository: domainRepo,
			DomainService:    domainSvc,
		},
		oauth: &OAuthProvider{
			Common:               common,
			OAuthStateRepository: &repository.OAuthState{DB: db},
		},
	}
}

func newRegularDomain(name, redirect string) *model.Domain {
	return &model.Domain{
		Name:                nullable.NewNullableWithValue(*vo.NewString255Must(name)),
		Type:                nullable.NewNullableWithValue(*vo.NewString32Must("regular")),
		HostWebsite:         nullable.NewNullableWithValue(true),
		ManagedTLS:          nullable.NewNullableWithValue(false),
		OwnManagedTLS:       nullable.NewNullableWithValue(false),
		SelfSignedTLS:       nullable.NewNullableWithValue(false),
		PageContent:         nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust("")),
		PageNotFoundContent: nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust("")),
		RedirectURL:         nullable.NewNullableWithValue(*vo.NewOptionalString1024Must(redirect)),
	}
}

// insertLegacyDomain stores a domain directly, as rows from before the name
// format rules exist in older installs
func (f *domainFixture) insertLegacyDomain(name, domainType string, proxyID *uuid.UUID, redirect string) uuid.UUID {
	f.t.Helper()
	now := time.Now()
	id := uuid.New()
	row := &database.Domain{
		ID:          id,
		CreatedAt:   &now,
		UpdatedAt:   &now,
		Name:        name,
		Type:        domainType,
		ProxyID:     proxyID,
		HostWebsite: true,
		RedirectURL: redirect,
	}
	if domainType == "proxy" {
		row.ProxyTargetDomain = "login.example.com"
		row.HostWebsite = false
	}
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("insert legacy domain: %v", err)
	}
	return id
}

func (f *domainFixture) storedDomain(id uuid.UUID) *database.Domain {
	f.t.Helper()
	var row database.Domain
	if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
		f.t.Fatalf("get domain: %v", err)
	}
	return &row
}

// TestDomainCreateNormalizesName checks create trims, lowercases and drops one
// trailing dot before validating (16)
func TestDomainCreateNormalizesName(t *testing.T) {
	f := newDomainFixture(t)
	id, err := f.domain.Create(f.ctx, f.session, newRegularDomain("  Phish.Example.TEST.  ", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := f.storedDomain(*id).Name; got != "phish.example.test" {
		t.Fatalf("stored name = %q, want phish.example.test", got)
	}
	// still rejected after normalizing
	for _, n := range []string{"bad_name.test", "two.dots..", " ADMIN.example.test. "} {
		if _, err := f.domain.Create(f.ctx, f.session, newRegularDomain(n, "")); err == nil {
			t.Errorf("expected %q to be rejected", n)
		}
	}
}

// TestDomainUpdateKeepsLegacyNameEditable checks an existing domain whose name
// fails the format rules can still be updated (15), the reserved host check
// still applies, and the redirect is only checked when a new value is sent (17)
func TestDomainUpdateKeepsLegacyNameEditable(t *testing.T) {
	f := newDomainFixture(t)
	id := f.insertLegacyDomain("Legacy_Name.test", "regular", nil, "")

	incoming := &model.Domain{
		HostWebsite: nullable.NewNullableWithValue(false),
	}
	if err := f.domain.UpdateByID(f.ctx, f.session, &id, incoming); err != nil {
		t.Fatalf("update legacy domain: %v", err)
	}
	if f.storedDomain(id).HostWebsite {
		t.Fatal("expected host website to be updated")
	}

	// a relative redirect is accepted again
	incoming = &model.Domain{
		RedirectURL: nullable.NewNullableWithValue(*vo.NewOptionalString1024Must("/landing")),
	}
	if err := f.domain.UpdateByID(f.ctx, f.session, &id, incoming); err != nil {
		t.Fatalf("update relative redirect: %v", err)
	}
	if got := f.storedDomain(id).RedirectURL; got != "/landing" {
		t.Fatalf("redirect = %q, want /landing", got)
	}

	// a bad incoming redirect is refused
	for _, bad := range []string{"//evil.test", "javascript:alert(1)", "/\\evil.test"} {
		incoming = &model.Domain{
			RedirectURL: nullable.NewNullableWithValue(*vo.NewOptionalString1024Must(bad)),
		}
		if err := f.domain.UpdateByID(f.ctx, f.session, &id, incoming); err == nil {
			t.Errorf("expected redirect %q to be rejected", bad)
		}
	}

	// a stored redirect from before the rules does not block other edits
	legacyRedirect := f.insertLegacyDomain("old-redirect.test", "regular", nil, "legacy-value")
	incoming = &model.Domain{HostWebsite: nullable.NewNullableWithValue(false)}
	if err := f.domain.UpdateByID(f.ctx, f.session, &legacyRedirect, incoming); err != nil {
		t.Fatalf("update with stored legacy redirect: %v", err)
	}

	// a stored name equal to the admin host is still refused
	reserved := f.insertLegacyDomain(testAdminHost, "regular", nil, "")
	incoming = &model.Domain{HostWebsite: nullable.NewNullableWithValue(true)}
	if err := f.domain.UpdateByID(f.ctx, f.session, &reserved, incoming); err == nil {
		t.Fatal("expected reserved admin host to be rejected on update")
	}
}

// TestDomainUpdateResentRedirectNotChecked checks that the client sending the
// stored redirect back unchanged does not block an edit, while a changed value
// is still checked (8)
func TestDomainUpdateResentRedirectNotChecked(t *testing.T) {
	f := newDomainFixture(t)
	id := f.insertLegacyDomain("resend.example.test", "regular", nil, "legacy-value")

	incoming := &model.Domain{
		HostWebsite: nullable.NewNullableWithValue(false),
		RedirectURL: nullable.NewNullableWithValue(*vo.NewOptionalString1024Must("legacy-value")),
	}
	if err := f.domain.UpdateByID(f.ctx, f.session, &id, incoming); err != nil {
		t.Fatalf("update with resent stored redirect: %v", err)
	}
	if f.storedDomain(id).HostWebsite {
		t.Fatal("expected host website to be updated")
	}

	incoming = &model.Domain{
		RedirectURL: nullable.NewNullableWithValue(*vo.NewOptionalString1024Must("javascript:alert(1)")),
	}
	if err := f.domain.UpdateByID(f.ctx, f.session, &id, incoming); err == nil {
		t.Fatal("expected changed javascript redirect to be rejected")
	}
	if got := f.storedDomain(id).RedirectURL; got != "legacy-value" {
		t.Fatalf("redirect = %q, want legacy-value", got)
	}
}

func proxyConfigYAML(to string) string {
	return "version: \"0.0\"\nlogin.example.com:\n  to: \"" + to + "\"\n"
}

func newProxyModel(to string) *model.Proxy {
	return &model.Proxy{
		StartURL:    nullable.NewNullableWithValue(*vo.NewString1024Must("https://login.example.com/")),
		ProxyConfig: nullable.NewNullableWithValue(*vo.NewString1MBMust(proxyConfigYAML(to))),
	}
}

// TestProxyConfigValidatesToDomain checks each 'to' domain is validated in the
// config before any domain is created, and that a legacy name already owned by
// the proxy is kept on update (19)
func TestProxyConfigValidatesToDomain(t *testing.T) {
	f := newDomainFixture(t)

	if err := f.proxy.validateProxyConfig(f.ctx, newProxyModel("phish.example.test")); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	cases := map[string]string{
		"bad_name.example.test": "underscores",
		"Upper.example.test":    "lowercase",
		testAdminHost:           "reserved",
	}
	for to, want := range cases {
		err := f.proxy.validateProxyConfig(f.ctx, newProxyModel(to))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("to %q: err = %v, want it to mention %q", to, err, want)
		}
	}
	var count int64
	f.db.Model(&database.Domain{}).Count(&count)
	if count != 0 {
		t.Fatalf("validation created %d domains, want 0", count)
	}

	// update: a legacy invalid name owned by this proxy is kept
	proxyID := uuid.New()
	f.insertLegacyDomain("legacy_proxy.example.test", "proxy", &proxyID, "")
	if err := f.proxy.validateProxyToDomain(f.ctx, "legacy_proxy.example.test", &proxyID); err != nil {
		t.Fatalf("legacy owned domain: %v", err)
	}
	// owned by another proxy or new, the format check applies
	other := uuid.New()
	if err := f.proxy.validateProxyToDomain(f.ctx, "legacy_proxy.example.test", &other); err == nil {
		t.Fatal("expected legacy name owned by another proxy to be rejected")
	}
	if err := f.proxy.validateProxyToDomain(f.ctx, "new_name.example.test", &proxyID); err == nil {
		t.Fatal("expected new invalid name to be rejected")
	}
	// the reserved check always applies
	f.insertLegacyDomain(testAdminHost, "proxy", &proxyID, "")
	if err := f.proxy.validateProxyToDomain(f.ctx, testAdminHost, &proxyID); err == nil {
		t.Fatal("expected reserved host to be rejected even when owned")
	}

	// sync keeps a legacy proxy domain: the update path no longer runs the
	// name format check
	legacy := uuid.New()
	legacyID := f.insertLegacyDomain("sync_legacy.example.test", "proxy", &legacy, "")
	existing, err := f.domain.DomainRepository.GetByID(f.ctx, &legacyID, &repository.DomainOption{})
	if err != nil {
		t.Fatalf("get legacy: %v", err)
	}
	existing.ProxyTargetDomain.Set(*vo.NewOptionalString255Must("other.example.com"))
	if err := f.domain.UpdateProxyDomain(f.ctx, f.session, &legacyID, existing); err != nil {
		t.Fatalf("update legacy proxy domain: %v", err)
	}
	if got := f.storedDomain(legacyID).ProxyTargetDomain; got != "other.example.com" {
		t.Fatalf("proxy target = %q, want other.example.com", got)
	}
}

// TestOAuthExchangeRefusesReplay checks a used or expired state never reaches
// the token exchange, and that the status the admin page polls reflects it (13)
func TestOAuthExchangeRefusesReplay(t *testing.T) {
	f := newDomainFixture(t)
	repo := f.oauth.OAuthStateRepository
	providerID := uuid.New()

	insert := func(token string, expires time.Time) uuid.UUID {
		id, err := repo.Insert(f.ctx, &model.OAuthState{
			StateToken:      nullable.NewNullableWithValue(*vo.NewString255Must(token)),
			OAuthProviderID: nullable.NewNullableWithValue(providerID),
			ExpiresAt:       &expires,
		})
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		return *id
	}

	// already claimed by another callback
	usedID := insert("used-state", time.Now().Add(time.Minute))
	if err := repo.MarkAsUsed(f.ctx, usedID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	gotID, err := f.oauth.ExchangeCodeForTokens(f.ctx, nil, "used-state", "code", "https://cb")
	if err == nil {
		t.Fatal("expected replayed state to be refused")
	}
	if gotID != uuid.Nil {
		t.Fatalf("replay returned provider id %v, want nil", gotID)
	}

	// expired: refused and recorded as failed
	insert("expired-state", time.Now().Add(-time.Minute))
	gotID, err = f.oauth.ExchangeCodeForTokens(f.ctx, nil, "expired-state", "code", "https://cb")
	if err == nil {
		t.Fatal("expected expired state to be refused")
	}
	if gotID != providerID {
		t.Fatalf("expired returned provider id %v, want %v", gotID, providerID)
	}
	status, err := f.oauth.GetAuthorizationStatus(f.ctx, f.session, &providerID, "expired-state")
	if err != nil || status != "failed" {
		t.Fatalf("expired status = %q, %v, want failed", status, err)
	}

	// provider error
	insert("error-state", time.Now().Add(time.Minute))
	status, _ = f.oauth.GetAuthorizationStatus(f.ctx, f.session, &providerID, "error-state")
	if status != "pending" {
		t.Fatalf("status before callback = %q, want pending", status)
	}
	if got := f.oauth.FailAuthorization(f.ctx, "error-state"); got != providerID {
		t.Fatalf("fail returned %v, want %v", got, providerID)
	}
	if got := f.oauth.FailAuthorization(f.ctx, "error-state"); got != uuid.Nil {
		t.Fatalf("second fail returned %v, want nil", got)
	}
	status, _ = f.oauth.GetAuthorizationStatus(f.ctx, f.session, &providerID, "error-state")
	if status != "failed" {
		t.Fatalf("status after provider error = %q, want failed", status)
	}

	// the state of one provider is not readable through another
	other := uuid.New()
	if _, err := f.oauth.GetAuthorizationStatus(f.ctx, f.session, &other, "error-state"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("other provider err = %v, want not found", err)
	}
	if status, _ := f.oauth.GetAuthorizationStatus(f.ctx, f.session, &providerID, "unknown"); status != "expired" {
		t.Fatalf("unknown state status = %q, want expired", status)
	}
}

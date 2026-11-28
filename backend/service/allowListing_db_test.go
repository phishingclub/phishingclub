package service

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type allowListingFixture struct {
	t       *testing.T
	ctx     context.Context
	db      *gorm.DB
	svc     *AllowListing
	smtp    *SMTPConfiguration
	session *model.Session
	now     time.Time
}

func newAllowListingFixture(t *testing.T) *allowListingFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "allowlisting.db") + "?_foreign_keys=OFF"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Domain{},
		&database.Proxy{},
		&database.Identifier{},
		&database.Email{},
		&database.SMTPConfiguration{},
		&database.SMTPHeader{},
		&database.APISender{},
		&database.CampaignTemplate{},
		&database.CampaignTemplateLanguage{},
		&database.Campaign{},
		&database.CampaignLanguageRoute{},
		&database.CampaignTemplatePool{},
		&database.Program{},
		&database.ProgramLanguageRoute{},
		&database.ProgramStep{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	common := Common{Logger: zap.NewNop().Sugar()}
	smtpRepo := &repository.SMTPConfiguration{DB: db}
	return &allowListingFixture{
		t:   t,
		ctx: context.Background(),
		db:  db,
		svc: &AllowListing{
			Common:                     common,
			CampaignTemplateRepository: &repository.CampaignTemplate{DB: db},
			CampaignRepository:         &repository.Campaign{DB: db},
			ProgramRepository:          &repository.Program{DB: db},
			DomainRepository:           &repository.Domain{DB: db},
		},
		smtp:    &SMTPConfiguration{Common: common, SMTPConfigurationRepository: smtpRepo},
		session: authorizedSession(t),
		now:     time.Now().UTC(),
	}
}

func (f *allowListingFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("create %T: %v", row, err)
	}
}

// company returns a company id, the scope checks only compare ids
func (f *allowListingFixture) company() uuid.UUID {
	return uuid.New()
}

func (f *allowListingFixture) domain(name string, proxyID *uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.create(&database.Domain{ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, ProxyID: proxyID})
	return id
}

func (f *allowListingFixture) proxy() uuid.UUID {
	id := uuid.New()
	f.create(&database.Proxy{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "proxy " + id.String(),
		StartURL: "https://login.example.test", ProxyConfig: "version: \"0.0\"",
	})
	return id
}

func (f *allowListingFixture) email(mailFrom string) uuid.UUID {
	id := uuid.New()
	f.create(&database.Email{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email " + id.String()[:8],
		Content: "hi", MailFrom: mailFrom, Subject: "subject", From: "Sender <" + mailFrom + ">",
	})
	return id
}

func (f *allowListingFixture) smtpConfig(ips string) uuid.UUID {
	id := uuid.New()
	f.create(&database.SMTPConfiguration{
		ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "smtp " + id.String()[:8],
		Host: "smtp.example.test", Port: 587, PublicSendingIPs: ips,
	})
	return id
}

func (f *allowListingFixture) apiSender() uuid.UUID {
	id := uuid.New()
	f.create(&database.APISender{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "api " + id.String()[:8],
		RequestMethod: "POST", RequestURL: "https://api.example.test/send",
	})
	return id
}

type allowListingTemplateOpts struct {
	name           string
	domainID       *uuid.UUID
	emailID        *uuid.UUID
	smtpID         *uuid.UUID
	apiSenderID    *uuid.UUID
	landingProxyID *uuid.UUID
	companyID      *uuid.UUID
}

func (f *allowListingFixture) template(o allowListingTemplateOpts) uuid.UUID {
	id := uuid.New()
	urlIdentifier := uuid.New()
	f.create(&database.CampaignTemplate{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: o.name,
		URLIdentifierID: &urlIdentifier, DomainID: o.domainID, EmailID: o.emailID,
		SMTPConfigurationID: o.smtpID, APISenderID: o.apiSenderID,
		LandingProxyID: o.landingProxyID, CompanyID: o.companyID,
		LureURLMode: "query", LureCodeAlgo: "crockford32", LureCodeLength: 12,
	})
	return id
}

func (f *allowListingFixture) campaign(pool []uuid.UUID, companyID, programID *uuid.UUID) uuid.UUID {
	id := uuid.New()
	row := map[string]any{
		"id":                   id,
		"created_at":           utils.NowRFC3339UTC(),
		"updated_at":           utils.NowRFC3339UTC(),
		"name":                 "campaign " + id.String(),
		"sort_field":           "email",
		"sort_order":           "asc",
		"campaign_template_id": pool[0].String(),
	}
	if companyID != nil {
		row["company_id"] = companyID.String()
	}
	if programID != nil {
		row["program_id"] = programID.String()
	}
	if err := f.db.Model(&database.Campaign{}).Create(row).Error; err != nil {
		f.t.Fatalf("insert campaign: %v", err)
	}
	if err := f.svc.CampaignRepository.SetTemplatePool(f.ctx, &id, pool); err != nil {
		f.t.Fatalf("set pool: %v", err)
	}
	return id
}

func (f *allowListingFixture) program(templates []uuid.UUID, companyID *uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.create(&database.Program{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "program " + id.String(),
		LoopMode: model.ProgramLoopComplete, OrderMode: model.ProgramOrderSequential,
		CompanyID: companyID,
	})
	for i, tid := range templates {
		stepID := uuid.New()
		templateID := tid
		f.create(&database.ProgramStep{
			ID: &stepID, CreatedAt: &f.now, UpdatedAt: &f.now,
			ProgramID: &id, CampaignTemplateID: &templateID, Position: i,
		})
	}
	return id
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func joined(v []string) string {
	return strings.Join(v, "|")
}

// a campaign with a template pool combines the values of every template,
// each once, and flags what each template is missing
func TestAllowListingCampaignPoolDeduplicatesAndFlags(t *testing.T) {
	f := newAllowListingFixture(t)
	domainA := f.domain("Phish-A.example.test", nil)
	domainB := f.domain("phish-b.example.test", nil)
	proxyID := f.proxy()
	f.domain("login.phish-a.example.test", &proxyID)
	f.domain("phish-a.example.test.proxy", &proxyID)
	emailA := f.email("bounce@Mail-A.example.test")
	emailB := f.email("bounce@mail-b.example.test")
	smtpA := f.smtpConfig("203.0.113.10\n2001:db8::1")
	smtpB := f.smtpConfig("203.0.113.10\n198.51.100.0/24")
	smtpNoIP := f.smtpConfig("")
	api := f.apiSender()

	t1 := f.template(allowListingTemplateOpts{name: "one", domainID: &domainA, emailID: &emailA, smtpID: &smtpA})
	t2 := f.template(allowListingTemplateOpts{name: "two", domainID: &domainA, emailID: &emailA, smtpID: &smtpB, landingProxyID: &proxyID})
	t3 := f.template(allowListingTemplateOpts{name: "three", domainID: &domainB, emailID: &emailB, smtpID: &smtpNoIP})
	t4 := f.template(allowListingTemplateOpts{name: "four", domainID: &domainB, emailID: &emailB, apiSenderID: &api})
	t5 := f.template(allowListingTemplateOpts{name: "five"})
	campaignID := f.campaign([]uuid.UUID{t1, t2, t3, t4, t5}, nil, nil)

	got, err := f.svc.GetByCampaignID(f.ctx, f.session, &campaignID, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if want := "mail-a.example.test|mail-b.example.test"; joined(got.MailFromDomains) != want {
		t.Fatalf("mail from: got %v", got.MailFromDomains)
	}
	if want := "203.0.113.10|2001:db8::1|198.51.100.0/24"; joined(got.SendingIPs) != want {
		t.Fatalf("sending IPs: got %v", got.SendingIPs)
	}
	want := "phish-a.example.test/*|login.phish-a.example.test/*|phish-a.example.test.proxy/*|phish-b.example.test/*"
	if joined(got.SimulationURLs) != want {
		t.Fatalf("simulation URLs: got %v", got.SimulationURLs)
	}
	// the proxy domain under the template domain shares its registered domain
	wantDomains := "example.test/*|*.example.test/*|test.proxy/*|*.test.proxy/*"
	if joined(got.SimulationDomainURLs) != wantDomains {
		t.Fatalf("simulation domain URLs: got %v", got.SimulationDomainURLs)
	}
	if !got.HasIPv6SendingIP {
		t.Fatalf("an IPv6 sending IP is not flagged")
	}
	if got.OverLimit != (model.AllowListingOverLimit{}) {
		t.Fatalf("over limit: got %+v", got.OverLimit)
	}
	if len(got.Templates) != 5 {
		t.Fatalf("templates: got %d", len(got.Templates))
	}
	missing := map[string]string{}
	senders := map[string]string{}
	for _, tmpl := range got.Templates {
		missing[tmpl.Name] = joined(tmpl.Missing)
		senders[tmpl.Name] = tmpl.SenderType
	}
	wantMissing := map[string]string{
		"one":   "",
		"two":   "",
		"three": model.AllowListingMissingSendingIP,
		"four":  "",
		"five": strings.Join([]string{
			model.AllowListingMissingDomain, model.AllowListingMissingEmail, model.AllowListingMissingSender,
		}, "|"),
	}
	for name, w := range wantMissing {
		if missing[name] != w {
			t.Fatalf("%s missing: got %q, want %q", name, missing[name], w)
		}
	}
	if senders["four"] != model.AllowListingSenderAPISender || senders["one"] != model.AllowListingSenderSMTP {
		t.Fatalf("sender types: %v", senders)
	}
	if got.Templates[1].Name != "two" || joined(got.Templates[1].ProxyDomains) != "login.phish-a.example.test|phish-a.example.test.proxy" {
		t.Fatalf("proxy domains: %+v", got.Templates[1])
	}
}

// a template outside the company in view is not found, a global template is
// found in every view
func TestAllowListingTemplateScope(t *testing.T) {
	f := newAllowListingFixture(t)
	companyA, companyB := f.company(), f.company()
	own := f.template(allowListingTemplateOpts{name: "own", companyID: &companyA})
	global := f.template(allowListingTemplateOpts{name: "global"})

	if _, err := f.svc.GetByTemplateID(f.ctx, f.session, &own, &companyA); err != nil {
		t.Fatalf("own company: %v", err)
	}
	if _, err := f.svc.GetByTemplateID(f.ctx, f.session, &own, &companyB); !isNotFound(err) {
		t.Fatalf("other company: want not found, got %v", err)
	}
	if _, err := f.svc.GetByTemplateID(f.ctx, f.session, &own, nil); !isNotFound(err) {
		t.Fatalf("global view: want not found, got %v", err)
	}
	for _, view := range []*uuid.UUID{nil, &companyA, &companyB} {
		if _, err := f.svc.GetByTemplateID(f.ctx, f.session, &global, view); err != nil {
			t.Fatalf("global template: %v", err)
		}
	}
}

// campaigns and programs a company owns are found only in its view, global
// ones in every view
func TestAllowListingCampaignAndProgramScope(t *testing.T) {
	f := newAllowListingFixture(t)
	companyA, companyB := f.company(), f.company()
	tmpl := f.template(allowListingTemplateOpts{name: "shared"})
	campaignA := f.campaign([]uuid.UUID{tmpl}, &companyA, nil)
	globalCampaign := f.campaign([]uuid.UUID{tmpl}, nil, nil)
	programA := f.program([]uuid.UUID{tmpl}, &companyA)

	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &campaignA, &companyA); err != nil {
		t.Fatalf("campaign own company: %v", err)
	}
	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &campaignA, &companyB); !isNotFound(err) {
		t.Fatalf("campaign other company: want not found, got %v", err)
	}
	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &campaignA, nil); !isNotFound(err) {
		t.Fatalf("campaign global view: want not found, got %v", err)
	}
	// a global campaign is shown in every view, the same as its page
	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &globalCampaign, &companyA); err != nil {
		t.Fatalf("global campaign in company view: %v", err)
	}
	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &globalCampaign, nil); err != nil {
		t.Fatalf("global campaign in global view: %v", err)
	}
	if _, err := f.svc.GetByProgramID(f.ctx, f.session, &programA, &companyA); err != nil {
		t.Fatalf("program own company: %v", err)
	}
	if _, err := f.svc.GetByProgramID(f.ctx, f.session, &programA, &companyB); !isNotFound(err) {
		t.Fatalf("program other company: want not found, got %v", err)
	}
	if _, err := f.svc.GetByProgramID(f.ctx, f.session, &programA, nil); !isNotFound(err) {
		t.Fatalf("program global view: want not found, got %v", err)
	}
	// a global program is shown in every view, the same as its page
	globalProgram := f.program([]uuid.UUID{tmpl}, nil)
	for _, view := range []*uuid.UUID{nil, &companyA, &companyB} {
		listing, err := f.svc.GetByProgramID(f.ctx, f.session, &globalProgram, view)
		if err != nil {
			t.Fatalf("global program in view %v: %v", view, err)
		}
		if len(listing.Templates) != 1 {
			t.Fatalf("global program templates: %+v", listing.Templates)
		}
	}
	unknown := uuid.New()
	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &unknown, nil); !isNotFound(err) {
		t.Fatalf("unknown campaign: want not found, got %v", err)
	}
}

// a hidden program campaign is never read by its campaign id, the program
// variant returns the templates of all its steps
func TestAllowListingRefusesProgramCampaign(t *testing.T) {
	f := newAllowListingFixture(t)
	domainID := f.domain("program.example.test", nil)
	emailID := f.email("bounce@program.example.test")
	smtpID := f.smtpConfig("203.0.113.20")
	t1 := f.template(allowListingTemplateOpts{name: "a", domainID: &domainID, emailID: &emailID, smtpID: &smtpID})
	t2 := f.template(allowListingTemplateOpts{name: "b", domainID: &domainID, emailID: &emailID, smtpID: &smtpID})
	programID := f.program([]uuid.UUID{t1, t2, t1}, nil)
	hidden := f.campaign([]uuid.UUID{t1}, nil, &programID)

	if _, err := f.svc.GetByCampaignID(f.ctx, f.session, &hidden, nil); !isNotFound(err) {
		t.Fatalf("hidden campaign: want not found, got %v", err)
	}
	got, err := f.svc.GetByProgramID(f.ctx, f.session, &programID, nil)
	if err != nil {
		t.Fatalf("program: %v", err)
	}
	if len(got.Templates) != 2 || joined(got.SendingIPs) != "203.0.113.20" ||
		joined(got.SimulationURLs) != "program.example.test/*" {
		t.Fatalf("program result: %+v", got)
	}
}

// a session without the permission is refused
func TestAllowListingRefusesUnauthorized(t *testing.T) {
	f := newAllowListingFixture(t)
	tmpl := f.template(allowListingTemplateOpts{name: "x"})
	if _, err := f.svc.GetByTemplateID(f.ctx, unauthorizedSession(t), &tmpl, nil); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("want authorization failed, got %v", err)
	}
}

// an update that sets the public sending IPs saves them, and an update that
// leaves the field out keeps them
func TestSMTPConfigurationUpdateSavesPublicSendingIPs(t *testing.T) {
	f := newAllowListingFixture(t)
	conf := &model.SMTPConfiguration{
		Name:             nullable.NewNullableWithValue(*vo.NewString127Must("ips")),
		Host:             nullable.NewNullableWithValue(*vo.NewString255Must("smtp.example.test")),
		Port:             nullable.NewNullableWithValue(*vo.NewPortMust(587)),
		Username:         nullable.NewNullableWithValue(*vo.NewOptionalString255Must("")),
		Password:         nullable.NewNullableWithValue(*vo.NewOptionalString255Must("")),
		IgnoreCertErrors: nullable.NewNullableWithValue(false),
	}
	id, err := f.smtp.Create(f.ctx, f.session, conf)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	read := func() string {
		got, err := f.smtp.SMTPConfigurationRepository.GetByID(f.ctx, id, &repository.SMTPConfigurationOption{})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return got.PublicSendingIPs.MustGet()
	}
	if got := read(); got != "" {
		t.Fatalf("new config: got %q", got)
	}

	update := *conf
	update.PublicSendingIPs = nullable.NewNullableWithValue("203.0.113.5, 2001:DB8::5")
	if err := f.smtp.UpdateByID(f.ctx, f.session, id, &update); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := read(); got != "203.0.113.5\n2001:db8::5" {
		t.Fatalf("after update: got %q", got)
	}

	// an update without the field, as a package update sends, keeps the value
	keep := *conf
	keep.Host = nullable.NewNullableWithValue(*vo.NewString255Must("smtp2.example.test"))
	if err := f.smtp.UpdateByID(f.ctx, f.session, id, &keep); err != nil {
		t.Fatalf("update without field: %v", err)
	}
	if got := read(); got != "203.0.113.5\n2001:db8::5" {
		t.Fatalf("after update without field: got %q", got)
	}

	invalid := *conf
	invalid.PublicSendingIPs = nullable.NewNullableWithValue("mail.example.test")
	if err := f.smtp.UpdateByID(f.ctx, f.session, id, &invalid); err == nil {
		t.Fatal("a hostname must be refused")
	}

	cleared := *conf
	cleared.PublicSendingIPs = nullable.NewNullableWithValue("")
	if err := f.smtp.UpdateByID(f.ctx, f.session, id, &cleared); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := read(); got != "" {
		t.Fatalf("after clear: got %q", got)
	}
}

// TestAllowListingTemplateReadOnce checks the allow listing of one template
// reads the template once, and a template that does not exist is not found
func TestAllowListingTemplateReadOnce(t *testing.T) {
	f := newAllowListingFixture(t)
	tmpl := f.template(allowListingTemplateOpts{name: "one"})
	var reads int
	name := "test:count_template_reads"
	if err := f.db.Callback().Query().Before("gorm:query").Register(name, func(db *gorm.DB) {
		if db.Statement.Table == database.CAMPAIGN_TEMPLATE_TABLE {
			reads++
		}
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove(name) })
	got, err := f.svc.GetByTemplateID(f.ctx, f.session, &tmpl, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Templates) != 1 || got.Templates[0].Name != "one" {
		t.Fatalf("templates: %+v", got.Templates)
	}
	if reads != 1 {
		t.Fatalf("expected the template read once, got %d", reads)
	}
	missing := uuid.New()
	if _, err := f.svc.GetByTemplateID(f.ctx, f.session, &missing, nil); !isNotFound(err) {
		t.Fatalf("missing template: want not found, got %v", err)
	}
}

// the whole domain list has two entries per registered domain, and falls back
// to the exact host for a public suffix or an IP address
func TestAllowListingSimulationDomainURLs(t *testing.T) {
	cases := []struct {
		name  string
		hosts []string
		exact string
		whole string
	}{
		{
			name:  "subdomains of one domain",
			hosts: []string{"login.contoso.com", "a.b.contoso.com", "contoso.com"},
			exact: "login.contoso.com/*|a.b.contoso.com/*|contoso.com/*",
			whole: "contoso.com/*|*.contoso.com/*",
		},
		{
			name:  "co.uk style suffix",
			hosts: []string{"secure.login.contoso.co.uk", "contoso.co.uk"},
			exact: "secure.login.contoso.co.uk/*|contoso.co.uk/*",
			whole: "contoso.co.uk/*|*.contoso.co.uk/*",
		},
		{
			name:  "public suffix host",
			hosts: []string{"github.io", "co.uk", "foo.github.io"},
			exact: "github.io/*|co.uk/*|foo.github.io/*",
			whole: "github.io/*|co.uk/*|foo.github.io/*|*.foo.github.io/*",
		},
		{
			name:  "ip host",
			hosts: []string{"203.0.113.7", "[2001:db8::1]"},
			exact: "203.0.113.7/*|[2001:db8::1]/*",
			whole: "203.0.113.7/*|[2001:db8::1]/*",
		},
		{
			name:  "international name",
			hosts: []string{"Login.Bücher.Example", "bücher.example"},
			exact: "login.xn--bcher-kva.example/*|xn--bcher-kva.example/*",
			whole: "xn--bcher-kva.example/*|*.xn--bcher-kva.example/*",
		},
		{
			name:  "deduplicated and lower case",
			hosts: []string{"A.Contoso.com", "a.contoso.com", "B.CONTOSO.COM.", ""},
			exact: "a.contoso.com/*|b.contoso.com/*",
			whole: "contoso.com/*|*.contoso.com/*",
		},
		{
			name: "mixed with proxy domains",
			hosts: []string{
				"phish.example.test", "login.microsoftonline.com.phish.example.test",
				"login.other.example", "other.example",
			},
			exact: "phish.example.test/*|login.microsoftonline.com.phish.example.test/*|login.other.example/*|other.example/*",
			whole: "example.test/*|*.example.test/*|other.example/*|*.other.example/*",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := &model.AllowListing{}
			setSimulationURLs(got, c.hosts)
			if joined(got.SimulationURLs) != c.exact {
				t.Fatalf("exact: got %v, want %v", got.SimulationURLs, c.exact)
			}
			if joined(got.SimulationDomainURLs) != c.whole {
				t.Fatalf("whole domain: got %v, want %v", got.SimulationDomainURLs, c.whole)
			}
			if len(got.TooLongURLs) != 0 {
				t.Fatalf("too long: got %v", got.TooLongURLs)
			}
		})
	}
}

// an entry longer than Microsoft allows is left out and listed as too long
func TestAllowListingSimulationURLTooLong(t *testing.T) {
	label := strings.Repeat("a", 60)
	long := strings.Join([]string{label, label, label, label, "contoso.com"}, ".")
	got := &model.AllowListing{}
	setSimulationURLs(got, []string{long})
	if len(got.SimulationURLs) != 0 || joined(got.TooLongURLs) != long+"/*" {
		t.Fatalf("exact: got %v, too long %v", got.SimulationURLs, got.TooLongURLs)
	}
	if joined(got.SimulationDomainURLs) != "contoso.com/*|*.contoso.com/*" {
		t.Fatalf("whole domain: got %v", got.SimulationDomainURLs)
	}
}

// each list is flagged once it has more entries than Microsoft allows, and
// the whole domain list counts two entries per registered domain
func TestAllowListingOverLimit(t *testing.T) {
	list := func(n int, format func(int) string) []string {
		out := []string{}
		for i := 0; i < n; i++ {
			out = append(out, format(i))
		}
		return out
	}
	host := func(i int) string { return "d" + strconv.Itoa(i) + ".example" }
	sub := func(i int) string { return "s" + strconv.Itoa(i) + ".contoso.com" }

	got := &model.AllowListing{
		MailFromDomains: list(model.AllowListingMaxDomains, host),
		SendingIPs:      list(model.AllowListingMaxSendingIPs, func(i int) string { return "203.0.113." + strconv.Itoa(i) }),
	}
	// 15 registered domains give 30 whole domain entries, the limit
	setSimulationURLs(got, list(15, host))
	if o := model.NewAllowListingOverLimit(got); o != (model.AllowListingOverLimit{}) {
		t.Fatalf("at the limits: got %+v", o)
	}

	got.MailFromDomains = list(model.AllowListingMaxDomains+1, host)
	got.SendingIPs = append(got.SendingIPs, "198.51.100.0/24")
	setSimulationURLs(got, list(16, host))
	o := model.NewAllowListingOverLimit(got)
	if !o.MailFromDomains || !o.SendingIPs || !o.SimulationDomainURLs || o.SimulationURLs {
		t.Fatalf("16 domains: got %+v", o)
	}

	// 31 subdomains of one domain pass the exact limit only
	setSimulationURLs(got, list(31, sub))
	o = model.NewAllowListingOverLimit(got)
	if !o.SimulationURLs || o.SimulationDomainURLs {
		t.Fatalf("31 subdomains: got %+v", o)
	}
}

// every language email adds its envelope domain to the mail from list, and
// each language flags a missing email or envelope sender on its own. The
// sender and the domains are shared, so they do not grow.
func TestAllowListingLanguageEnvelopes(t *testing.T) {
	f := newAllowListingFixture(t)
	domainID := f.domain("phish.example.test", nil)
	smtpID := f.smtpConfig("203.0.113.5")
	enEmail := f.email("bounce@en.example.test")
	tmpl := f.template(allowListingTemplateOpts{name: "multi", domainID: &domainID, emailID: &enEmail, smtpID: &smtpID})
	f.db.Model(&database.CampaignTemplate{}).Where("id = ?", tmpl).Update("language", "en")
	gone := uuid.New()
	for code, emailID := range map[string]uuid.UUID{
		"de": f.email("bounce@de.example.test"),
		// the same envelope domain as English is listed once
		"da": f.email("other@en.example.test"),
		"fr": gone,
		"sv": f.email("no-domain"),
	} {
		rowID := uuid.New()
		templateID := tmpl
		f.create(&database.CampaignTemplateLanguage{
			ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignTemplateID: &templateID,
			Language: code, EmailID: &emailID, IsUsable: true,
		})
	}
	got, err := f.svc.GetByTemplateID(f.ctx, f.session, &tmpl, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if joined(got.MailFromDomains) != "en.example.test|de.example.test" {
		t.Fatalf("mail from domains: %v", got.MailFromDomains)
	}
	if joined(got.SendingIPs) != "203.0.113.5" || joined(got.SimulationURLs) != "phish.example.test/*" {
		t.Fatalf("shared values grew: %v %v", got.SendingIPs, got.SimulationURLs)
	}
	entry := got.Templates[0]
	if entry.Language != "en" || len(entry.Languages) != 4 {
		t.Fatalf("template languages: %q %d", entry.Language, len(entry.Languages))
	}
	want := map[string]string{"da": "", "de": "", "fr": model.AllowListingMissingEmail, "sv": model.AllowListingMissingMailFrom}
	for _, l := range entry.Languages {
		if joined(l.Missing) != want[l.Language] {
			t.Fatalf("%s missing: %v, want %q", l.Language, l.Missing, want[l.Language])
		}
		if l.Language == "de" && (l.MailFromDomain != "de.example.test" || l.EmailID == nil) {
			t.Fatalf("German entry: %+v", l)
		}
	}
	// a template without languages has an empty list, not null
	plain := f.template(allowListingTemplateOpts{name: "plain", domainID: &domainID, emailID: &enEmail, smtpID: &smtpID})
	got, err = f.svc.GetByTemplateID(f.ctx, f.session, &plain, nil)
	if err != nil || got.Templates[0].Languages == nil || len(got.Templates[0].Languages) != 0 {
		t.Fatalf("plain template: %+v %v", got, err)
	}
}

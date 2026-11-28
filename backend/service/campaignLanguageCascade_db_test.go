package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap/zapcore"
)

// openCampaign stores an open campaign on templateID with language routing set
// to fallback (off when empty) and one recipient row per given stamp
func (f *templateLanguageFixture) openCampaign(templateID uuid.UUID, fallback string, programID *uuid.UUID, stamps ...string) uuid.UUID {
	f.t.Helper()
	campaignID := uuid.New()
	c := &database.Campaign{
		ID: &campaignID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "campaign " + campaignID.String()[:8],
		SortField: "email", SortOrder: "asc", CampaignTemplateID: &templateID, ProgramID: programID,
		CompanyID: f.templateCompany(templateID),
	}
	if fallback != "" {
		c.LanguageRouteBy = "country"
		c.LanguageDefault = fallback
	}
	f.create(c)
	for _, code := range stamps {
		code := code
		rowID, recipientID := uuid.New(), uuid.New()
		f.create(&database.CampaignRecipient{
			ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignID: &campaignID,
			RecipientID: &recipientID, CampaignTemplateID: &templateID, Language: &code, SendAt: &f.now,
		})
	}
	return campaignID
}

// closed reports whether the campaign is set to close, as Close does
func (f *templateLanguageFixture) closed(campaignID uuid.UUID) bool {
	f.t.Helper()
	var row database.Campaign
	if err := f.db.Where("id = ?", campaignID).First(&row).Error; err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	return row.CloseAt != nil || row.ClosedAt != nil
}

func (f *templateLanguageFixture) languageRow(templateID uuid.UUID, code string) database.CampaignTemplateLanguage {
	f.t.Helper()
	var row database.CampaignTemplateLanguage
	if err := f.db.Where("campaign_template_id = ? AND language = ?", templateID, code).First(&row).Error; err != nil {
		f.t.Fatalf("load language row: %v", err)
	}
	return row
}

func (f *templateLanguageFixture) templateIDs(templates []*model.CampaignTemplate) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, t := range templates {
		out[t.ID.MustGet()] = true
	}
	return out
}

// TestLookupsFindTemplatesThroughLanguageRows checks GetByEmailID and
// GetByPageID find a template that uses the content only on a language row,
// each template once, and leave other templates out
func TestLookupsFindTemplatesThroughLanguageRows(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	de := f.row("de", &f.c1)
	withLanguage := f.createTemplate(f.languageTemplate(&f.c1, de))
	tmpl := f.languageTemplate(&f.c1)
	plain := f.createTemplate(tmpl)
	other := f.createTemplate(f.languageTemplate(&f.c1, f.row("fr", &f.c1)))

	byEmail, err := f.repo.GetByEmailID(f.ctx, nullablePtr(de.EmailID), &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatal(err)
	}
	if ids := f.templateIDs(byEmail); len(byEmail) != 1 || !ids[withLanguage] {
		t.Fatalf("email lookup must find only the template with the language row, got %v", ids)
	}
	byPage, err := f.repo.GetByPageID(f.ctx, nullablePtr(de.LandingPageID), &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatal(err)
	}
	if ids := f.templateIDs(byPage); len(byPage) != 1 || !ids[withLanguage] {
		t.Fatalf("page lookup must find only the template with the language row, got %v", ids)
	}
	// the template's own content is still found, and only its template
	own, err := f.repo.GetByEmailID(f.ctx, nullablePtr(tmpl.EmailID), &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatal(err)
	}
	if ids := f.templateIDs(own); len(own) != 1 || !ids[plain] || ids[other] {
		t.Fatalf("own email lookup, got %v", ids)
	}
	uses, err := f.repo.GetLanguageUsesByPageID(f.ctx, nullablePtr(de.AfterLandingPageID))
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 1 || uses[0].TemplateID != withLanguage || uses[0].Language != "de" {
		t.Fatalf("language uses: %+v", uses)
	}
}

func nullablePtr[T any](v interface{ Get() (T, error) }) *T {
	got, err := v.Get()
	if err != nil {
		return nil
	}
	return &got
}

// TestLanguageEmailDeleteClosesCampaignsUsingIt checks a delete of an email
// that a language row uses clears the row and marks it as not usable, keeps
// the template's own email, closes the open campaigns that reach the language
// through a stamp or a route, leaves the others and program campaigns open,
// and audits the affected template languages
func TestLanguageEmailDeleteClosesCampaignsUsingIt(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	logger, buf := bufferLogger(zapcore.InfoLevel)
	f.svc.AuditLogger = logger
	de := f.row("de", &f.c1)
	templateID := f.createTemplate(f.languageTemplate(&f.c1, de, f.row("fr", &f.c1)))

	stamped := f.openCampaign(templateID, "", nil, "en", "de")
	routed := f.openCampaign(templateID, "de", nil)
	englishOnly := f.openCampaign(templateID, "", nil, "en", "fr")
	programID := uuid.New()
	program := f.openCampaign(templateID, "", &programID, "de")
	sentDE := f.openCampaign(templateID, "", nil)
	f.campaignRecipientSent(sentDE, templateID, "de")

	if err := f.svc.RemoveEmailByEmailID(f.ctx, f.session, nullablePtr(de.EmailID)); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]uuid.UUID{"stamped": stamped, "routed": routed, "sent": sentDE} {
		if !f.closed(c) {
			t.Fatalf("the %s campaign must be closed", name)
		}
	}
	if f.closed(englishOnly) {
		t.Fatal("a campaign that can not reach the language must stay open")
	}
	if f.closed(program) {
		t.Fatal("a program campaign must stay open")
	}
	row := f.languageRow(templateID, "de")
	if row.EmailID != nil || row.IsUsable {
		t.Fatalf("the language row must lose the email and not be usable, got %+v", row)
	}
	if fr := f.languageRow(templateID, "fr"); fr.EmailID == nil || !fr.IsUsable {
		t.Fatal("another language row must be kept")
	}
	var tmpl database.CampaignTemplate
	if err := f.db.Where("id = ?", templateID).First(&tmpl).Error; err != nil {
		t.Fatal(err)
	}
	if tmpl.EmailID == nil {
		t.Fatal("the template keeps its own email")
	}
	events := auditLines(t, buf)
	if len(events["CampaignTemplate.RemoveEmailByEmailID"]) != 1 {
		t.Fatalf("expected one audit event, got %v", events)
	}
	details, _ := events["CampaignTemplate.RemoveEmailByEmailID"][0]["details"].(map[string]any)
	affected, _ := details["affectedTemplateLanguages"].([]any)
	if len(affected) != 1 {
		t.Fatalf("affected template languages: %v", details)
	}
	first, _ := affected[0].(map[string]any)
	if first["templateId"] != templateID.String() || first["language"] != "de" {
		t.Fatalf("affected template languages: %v", affected)
	}
	if closed := anyStrings(details["closedCampaignIds"]); len(closed) != 3 {
		t.Fatalf("closed campaigns in audit: %v", closed)
	}
}

func (f *templateLanguageFixture) campaignRecipientSent(campaignID, templateID uuid.UUID, code string) {
	f.t.Helper()
	rowID, recipientID := uuid.New(), uuid.New()
	f.create(&database.CampaignRecipient{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignID: &campaignID,
		RecipientID: &recipientID, CampaignTemplateID: &templateID, Language: &code, SendAt: &f.now, SentAt: &f.now,
	})
}

// TestLanguagePageDeleteClearsRowAndClosesCampaigns checks a delete of a page
// that a language row uses clears that stage on the row, marks it as not
// usable and closes the campaigns that reach the language, and that a page
// the template itself loses keeps that stage on its language rows
func TestLanguagePageDeleteClearsRowAndClosesCampaigns(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	de := f.row("de", &f.c1)
	tmpl := f.languageTemplate(&f.c1, de)
	templateID := f.createTemplate(tmpl)
	stamped := f.openCampaign(templateID, "", nil, "de")
	englishOnly := f.openCampaign(templateID, "", nil, "en")

	if err := f.svc.RemovePagesByPageID(f.ctx, f.session, nullablePtr(de.LandingPageID)); err != nil {
		t.Fatal(err)
	}
	if !f.closed(stamped) || f.closed(englishOnly) {
		t.Fatal("only the campaign that reaches the language must be closed")
	}
	row := f.languageRow(templateID, "de")
	if row.LandingPageID != nil || row.IsUsable || row.AfterLandingPageID == nil {
		t.Fatalf("the row must lose only the landing page and not be usable, got %+v", row)
	}

	// the template loses its own after page: only the main content is
	// missing, the language rows keep their after page
	fr := f.row("fr", &f.c1)
	tmpl2 := f.languageTemplate(&f.c1, fr)
	second := f.createTemplate(tmpl2)
	if err := f.svc.RemovePagesByPageID(f.ctx, f.session, nullablePtr(tmpl2.AfterLandingPageID)); err != nil {
		t.Fatal(err)
	}
	row = f.languageRow(second, "fr")
	if row.AfterLandingPageID == nil || *row.AfterLandingPageID != fr.AfterLandingPageID.MustGet() || !row.IsUsable {
		t.Fatalf("the row must keep its after page, got %+v", row)
	}
}

// TestCompanyDeleteWithTemplateLanguages checks with foreign keys on that a
// company whose template has a language row with the company's email and
// pages is removed, with the template, its language rows and the content
func TestCompanyDeleteWithTemplateLanguages(t *testing.T) {
	f := newDeleteFixture(t)
	f.enableForeignKeys()
	session := authorizedSession(t)
	companyID := f.company("Languages")
	now := time.Now().UTC()
	content := func(row any) {
		f.create(row)
	}
	newID := func() *uuid.UUID {
		id := uuid.New()
		return &id
	}
	emailID, deEmailID := newID(), newID()
	pageID, dePageID := newID(), newID()
	for _, id := range []*uuid.UUID{emailID, deEmailID} {
		content(&database.Email{ID: id, CreatedAt: &now, UpdatedAt: &now, Name: "email " + id.String()[:8],
			Content: "x", MailFrom: "a@b.test", Subject: "s", From: "a@b.test", CompanyID: &companyID})
	}
	for _, id := range []*uuid.UUID{pageID, dePageID} {
		content(&database.Page{ID: id, CreatedAt: &now, UpdatedAt: &now, Name: "page " + id.String()[:8],
			Content: "x", CompanyID: &companyID})
	}
	templateID, rowID, urlID := newID(), newID(), newID()
	content(&database.Identifier{ID: urlID, Name: "id" + urlID.String()[:8]})
	content(&database.CampaignTemplate{ID: templateID, CreatedAt: &now, UpdatedAt: &now, Name: "multi",
		CompanyID: &companyID, URLIdentifierID: urlID, EmailID: emailID, LandingPageID: pageID, Language: "en"})
	content(&database.CampaignTemplateLanguage{ID: rowID, CreatedAt: &now, UpdatedAt: &now,
		CampaignTemplateID: templateID, Language: "de", EmailID: deEmailID, LandingPageID: dePageID, IsUsable: true})

	if _, err := f.companies.DeleteByID(f.ctx, session, &companyID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.waitDeletes()
	for what, gone := range map[string]bool{
		"company":       !f.exists(&database.Company{}, companyID),
		"template":      !f.exists(&database.CampaignTemplate{}, *templateID),
		"language row":  !f.exists(&database.CampaignTemplateLanguage{}, *rowID),
		"language page": !f.exists(&database.Page{}, *dePageID),
		"language mail": !f.exists(&database.Email{}, *deEmailID),
	} {
		if !gone {
			t.Fatalf("the company delete must remove the %s", what)
		}
	}
}

// TestContentRemovalClosesCampaignsNow checks every content removal closes the
// campaigns using the content now, also one with a later close time, leaves
// closed campaigns and program campaigns alone, and audits only the campaigns
// that were closed
func TestContentRemovalClosesCampaignsNow(t *testing.T) {
	type removal struct {
		name   string
		column string
		remove func(f *templateLanguageFixture, contentID uuid.UUID, de *model.CampaignTemplateLanguage) error
		event  string
	}
	cases := []removal{
		{"domain", "domain_id", func(f *templateLanguageFixture, id uuid.UUID, _ *model.CampaignTemplateLanguage) error {
			return f.svc.RemoveDomainByDomainID(f.ctx, f.session, &id)
		}, "CampaignTemplate.RemoveDomainByDomainID"},
		{"smtp", "smtp_configuration_id", func(f *templateLanguageFixture, id uuid.UUID, _ *model.CampaignTemplateLanguage) error {
			return f.svc.RemoveSmtpBySmtpID(f.ctx, f.session, &id)
		}, "CampaignTemplate.RemoveSmtpBySmtpID"},
		{"api sender", "api_sender_id", func(f *templateLanguageFixture, id uuid.UUID, _ *model.CampaignTemplateLanguage) error {
			return f.svc.removeAPISenderIDBySenderID(f.ctx, f.session, &id)
		}, "CampaignTemplate.RemoveAPISenderIDBySenderID"},
		{"proxy", "landing_proxy_id", func(f *templateLanguageFixture, id uuid.UUID, _ *model.CampaignTemplateLanguage) error {
			return f.svc.RemoveProxiesByProxyID(f.ctx, f.session, &id)
		}, "CampaignTemplate.RemoveProxiesByProxyID"},
		{"language email", "", func(f *templateLanguageFixture, _ uuid.UUID, de *model.CampaignTemplateLanguage) error {
			return f.svc.RemoveEmailByEmailID(f.ctx, f.session, nullablePtr(de.EmailID))
		}, "CampaignTemplate.RemoveEmailByEmailID"},
		{"language page", "", func(f *templateLanguageFixture, _ uuid.UUID, de *model.CampaignTemplateLanguage) error {
			return f.svc.RemovePagesByPageID(f.ctx, f.session, nullablePtr(de.LandingPageID))
		}, "CampaignTemplate.RemovePagesByPageID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newTemplateLanguageFixture(t)
			logger, buf := bufferLogger(zapcore.InfoLevel)
			f.svc.AuditLogger = logger
			de := f.row("de", &f.c1)
			templateID := f.createTemplate(f.languageTemplate(&f.c1, de))
			contentID := uuid.New()
			if c.column != "" {
				if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", templateID).Update(c.column, contentID).Error; err != nil {
					t.Fatal(err)
				}
			}
			later := f.now.Add(7 * 24 * time.Hour)
			past := f.now.Add(-time.Hour)
			withLater := f.openCampaign(templateID, "", nil, "de")
			noClose := f.openCampaign(templateID, "", nil, "de")
			alreadyClosed := f.openCampaign(templateID, "", nil, "de")
			programID := uuid.New()
			program := f.openCampaign(templateID, "", &programID, "de")
			set := func(id uuid.UUID, column string, v time.Time) {
				if err := f.db.Model(&database.Campaign{}).Where("id = ?", id).Update(column, v).Error; err != nil {
					t.Fatal(err)
				}
			}
			set(withLater, "close_at", later)
			set(alreadyClosed, "close_at", past)
			set(alreadyClosed, "closed_at", past)
			set(program, "close_at", later)

			if err := c.remove(f, contentID, de); err != nil {
				t.Fatal(err)
			}
			closeAt := func(id uuid.UUID) *time.Time {
				var row database.Campaign
				if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
					t.Fatal(err)
				}
				return row.CloseAt
			}
			soon := time.Now().UTC().Add(time.Minute)
			for name, id := range map[string]uuid.UUID{"later close time": withLater, "no close time": noClose} {
				if v := closeAt(id); v == nil || v.After(soon) {
					t.Fatalf("the campaign with %s must close now, close at %v", name, v)
				}
			}
			if v := closeAt(program); v == nil || !v.Equal(later) {
				t.Fatalf("a program campaign keeps its close time, got %v", v)
			}
			if v := closeAt(alreadyClosed); v == nil || !v.Equal(past) {
				t.Fatalf("a closed campaign is left alone, got %v", v)
			}
			events := auditLines(t, buf)
			if len(events[c.event]) != 1 {
				t.Fatalf("expected one %s audit event, got %v", c.event, events)
			}
			details, _ := events[c.event][0]["details"].(map[string]any)
			closed := map[string]bool{}
			for _, id := range anyStrings(details["closedCampaignIds"]) {
				closed[id] = true
			}
			if len(closed) != 2 || !closed[withLater.String()] || !closed[noClose.String()] {
				t.Fatalf("audit must list only the campaigns that were closed, got %v", details["closedCampaignIds"])
			}
		})
	}
}

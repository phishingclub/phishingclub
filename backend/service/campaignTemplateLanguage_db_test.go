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
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type templateLanguageFixture struct {
	t       *testing.T
	ctx     context.Context
	db      *gorm.DB
	now     time.Time
	session *model.Session
	svc     *CampaignTemplate
	repo    *repository.CampaignTemplate
	c1      uuid.UUID
	c2      uuid.UUID
}

func newTemplateLanguageFixture(t *testing.T) *templateLanguageFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "languages.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Identifier{},
		&database.Domain{},
		&database.Page{},
		&database.Proxy{},
		&database.Email{},
		&database.SMTPConfiguration{},
		&database.APISender{},
		&database.CampaignTemplate{},
		&database.CampaignTemplateLanguage{},
		&database.Campaign{},
		&database.CampaignTemplatePool{},
		&database.CampaignLanguageRoute{},
		&database.CampaignRecipient{},
		&database.Recipient{},
		&database.Program{},
		&database.ProgramStep{},
		&database.ProgramLanguageRoute{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	common := Common{Logger: zap.NewNop().Sugar()}
	repo := &repository.CampaignTemplate{DB: db}
	f := &templateLanguageFixture{
		t:       t,
		ctx:     context.Background(),
		db:      db,
		now:     time.Now().UTC(),
		session: authorizedSession(t),
		repo:    repo,
		svc: &CampaignTemplate{
			Common:                     common,
			CampaignTemplateRepository: repo,
			CampaignRepository:         &repository.Campaign{DB: db},
			IdentifierRepository:       &repository.Identifier{DB: db},
			ProgramRepository:          &repository.Program{DB: db},
			TemplateService:            &Template{Common: common},
		},
	}
	for _, name := range []string{"id", "p"} {
		id := uuid.New()
		f.create(&database.Identifier{ID: &id, Name: name})
	}
	f.c1 = f.company("c1")
	f.c2 = f.company("c2")
	return f
}

func (f *templateLanguageFixture) create(row any) {
	f.t.Helper()
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("insert %T: %v", row, err)
	}
}

func (f *templateLanguageFixture) company(key string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Company{ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "company " + key, AssetsKey: key})
	return id
}

func (f *templateLanguageFixture) email(companyID *uuid.UUID, content, subject string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Email{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "email " + id.String()[:8],
		Content: content, MailFrom: "a@b.test", Subject: subject, From: "a@b.test", CompanyID: companyID,
	})
	return id
}

func (f *templateLanguageFixture) page(companyID *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Page{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "page " + id.String()[:8], Content: "<p>page</p>", CompanyID: companyID})
	return id
}

func (f *templateLanguageFixture) proxy() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.create(&database.Proxy{ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "proxy " + id.String()[:8], StartURL: "https://start.test", ProxyConfig: "x"})
	return id
}

// languageTemplate is a template in companyID with an email, a landing page
// and an after page, written in English
func (f *templateLanguageFixture) languageTemplate(companyID *uuid.UUID, languages ...*model.CampaignTemplateLanguage) *model.CampaignTemplate {
	t := &model.CampaignTemplate{
		Name:               nullable.NewNullableWithValue(*vo.NewString64Must("tmpl " + uuid.NewString()[:8])),
		EmailID:            nullable.NewNullableWithValue(f.email(companyID, "hello {{.FirstName}}", "subject")),
		LandingPageID:      nullable.NewNullableWithValue(f.page(companyID)),
		AfterLandingPageID: nullable.NewNullableWithValue(f.page(companyID)),
		Language:           nullable.NewNullableWithValue("en"),
	}
	if companyID != nil {
		t.CompanyID = nullable.NewNullableWithValue(*companyID)
	}
	if languages != nil {
		t.Languages = nullable.NewNullableWithValue(languages)
	}
	return t
}

// row is a language row with an email and landing and after pages of companyID
func (f *templateLanguageFixture) row(code string, companyID *uuid.UUID) *model.CampaignTemplateLanguage {
	return &model.CampaignTemplateLanguage{
		Language:           code,
		EmailID:            nullable.NewNullableWithValue(f.email(companyID, "hallo {{.FirstName}}", "betreff")),
		LandingPageID:      nullable.NewNullableWithValue(f.page(companyID)),
		AfterLandingPageID: nullable.NewNullableWithValue(f.page(companyID)),
	}
}

func (f *templateLanguageFixture) createTemplate(tmpl *model.CampaignTemplate) uuid.UUID {
	f.t.Helper()
	id, err := f.svc.Create(f.ctx, f.session, tmpl)
	if err != nil {
		f.t.Fatalf("create template: %v", err)
	}
	return *id
}

func (f *templateLanguageFixture) storedLanguages(id uuid.UUID) []*model.CampaignTemplateLanguage {
	f.t.Helper()
	rows, err := f.repo.GetLanguagesByTemplateID(f.ctx, &id, true)
	if err != nil {
		f.t.Fatalf("get languages: %v", err)
	}
	return rows
}

// templateCompany returns the company of a stored template, nil when global
func (f *templateLanguageFixture) templateCompany(templateID uuid.UUID) *uuid.UUID {
	f.t.Helper()
	var row database.CampaignTemplate
	if err := f.db.Select("company_id").Where("id = ?", templateID.String()).First(&row).Error; err != nil {
		f.t.Fatalf("read template company: %v", err)
	}
	return row.CompanyID
}

// campaignWithStamp stores an open campaign on templateID, in the template's
// company, with one recipient stamped with code, sent or not
func (f *templateLanguageFixture) campaignWithStamp(templateID uuid.UUID, code string, sent bool, closed bool) uuid.UUID {
	f.t.Helper()
	campaignID := uuid.New()
	c := &database.Campaign{
		ID: &campaignID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "campaign " + campaignID.String()[:8],
		SortField: "email", SortOrder: "asc", CampaignTemplateID: &templateID, CompanyID: f.templateCompany(templateID),
	}
	if closed {
		c.ClosedAt = &f.now
	}
	f.create(c)
	rowID, recipientID := uuid.New(), uuid.New()
	cr := &database.CampaignRecipient{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignID: &campaignID,
		RecipientID: &recipientID, CampaignTemplateID: &templateID, Language: &code, SendAt: &f.now,
	}
	if sent {
		cr.SentAt = &f.now
	}
	f.create(cr)
	return campaignID
}

func codes(rows []*model.CampaignTemplateLanguage) string {
	return strings.Join(repository.LanguageCodes(rows), ",")
}

// TestTemplateLanguagesCreateReadReplace checks a template stores its
// languages, returns them with names, replaces them on update, keeps them
// when an update leaves them out and removes them with the template
func TestTemplateLanguagesCreateReadReplace(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	de := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, de, f.row(" DA ", &f.c1)))

	rows := f.storedLanguages(id)
	if codes(rows) != "da,de" {
		t.Fatalf("stored languages: %s", codes(rows))
	}
	for _, r := range rows {
		if !r.IsUsable || r.EmailName == "" || r.LandingPageName == "" || r.AfterLandingPageName == "" {
			t.Fatalf("row %s: usable %v names %q %q %q", r.Language, r.IsUsable, r.EmailName, r.LandingPageName, r.AfterLandingPageName)
		}
	}
	full, err := f.svc.GetByID(f.ctx, f.session, &id, &repository.CampaignTemplateOption{WithLanguages: true})
	if err != nil {
		t.Fatal(err)
	}
	if full.TemplateLanguage() != "en" || len(full.Languages.MustGet()) != 2 {
		t.Fatalf("full read: %q %d", full.TemplateLanguage(), len(full.Languages.MustGet()))
	}

	// an update without languages keeps them
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Name: nullable.NewNullableWithValue(*vo.NewString64Must("renamed " + uuid.NewString()[:8])),
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if codes(f.storedLanguages(id)) != "da,de" {
		t.Fatalf("languages after rename: %s", codes(f.storedLanguages(id)))
	}
	// given languages replace the stored rows as a whole
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{de, f.row("fr", &f.c1)}),
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if codes(f.storedLanguages(id)) != "de,fr" {
		t.Fatalf("languages after replace: %s", codes(f.storedLanguages(id)))
	}
	// an empty list removes them all
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{}),
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := len(f.storedLanguages(id)); n != 0 {
		t.Fatalf("languages after clear: %d", n)
	}
	// the rows go with the template
	id2 := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1)))
	if err := f.repo.DeleteByID(f.ctx, &id2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var left int64
	f.db.Model(&database.CampaignTemplateLanguage{}).Where("campaign_template_id = ?", id2.String()).Count(&left)
	if left != 0 {
		t.Fatalf("language rows left after delete: %d", left)
	}
}

// TestTemplateLanguagesUnique checks the database refuses a second row of the
// same language on a template
func TestTemplateLanguagesUnique(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1)))
	rowID := uuid.New()
	err := f.db.Create(&database.CampaignTemplateLanguage{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignTemplateID: &id, Language: "de",
	}).Error
	if err == nil {
		t.Fatal("second de row accepted")
	}
	// the service refuses it before the database
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, f.row("de", &f.c1), f.row("DE", &f.c1))); !isValidationError(err) {
		t.Fatalf("duplicate language: want validation error, got %v", err)
	}
	// and a row in the template language
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, f.row("en", &f.c1))); !isValidationError(err) {
		t.Fatalf("row in the template language: want validation error, got %v", err)
	}
	// rows need the template language
	tmpl := f.languageTemplate(&f.c1, f.row("de", &f.c1))
	tmpl.Language = nullable.NewNullableWithValue("")
	if _, err := f.svc.Create(f.ctx, f.session, tmpl); !isValidationError(err) {
		t.Fatalf("rows without template language: want validation error, got %v", err)
	}
	// clearing the template language while rows exist is refused
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language: nullable.NewNullableWithValue(""),
	}); !isValidationError(err) {
		t.Fatalf("clear template language with rows: want validation error, got %v", err)
	}
	// so is taking the code of a row
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language: nullable.NewNullableWithValue("de"),
	}); !isValidationError(err) {
		t.Fatalf("template language equal to a row: want validation error, got %v", err)
	}
}

// TestTemplateLanguagesCompanyScope checks the email and pages of a language
// row must be global or of the template company, on create and update
func TestTemplateLanguagesCompanyScope(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	foreignEmail := f.row("de", &f.c1)
	foreignEmail.EmailID = nullable.NewNullableWithValue(f.email(&f.c2, "x", "y"))
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, foreignEmail)); !isValidationError(err) {
		t.Fatalf("other company language email: want validation error, got %v", err)
	}
	foreignPage := f.row("de", &f.c1)
	foreignPage.AfterLandingPageID = nullable.NewNullableWithValue(f.page(&f.c2))
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, foreignPage)); !isValidationError(err) {
		t.Fatalf("other company language page: want validation error, got %v", err)
	}
	// a global template takes only global records
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(nil, f.row("de", &f.c1))); !isValidationError(err) {
		t.Fatalf("company language row on a global template: want validation error, got %v", err)
	}
	// global records are fine
	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", nil)))
	var count int64
	f.db.Model(&database.CampaignTemplate{}).Count(&count)
	if count != 1 {
		t.Fatalf("refused templates were stored: %d templates", count)
	}
	// update checks the new rows against the stored company
	bad := f.row("fr", &f.c1)
	bad.LandingPageID = nullable.NewNullableWithValue(f.page(&f.c2))
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		CompanyID: nullable.NewNullableWithValue(f.c2),
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{bad}),
	}); !isValidationError(err) {
		t.Fatalf("update with other company language page: want validation error, got %v", err)
	}
	if codes(f.storedLanguages(id)) != "de" {
		t.Fatalf("refused update changed the rows: %s", codes(f.storedLanguages(id)))
	}
}

// TestTemplateLanguagesEmailChecks checks the email of a language must parse
// and run, and its subject must parse, when it is saved
func TestTemplateLanguagesEmailChecks(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	badBody := f.row("de", &f.c1)
	badBody.EmailID = nullable.NewNullableWithValue(f.email(&f.c1, "hallo {{.FirstName", "betreff"))
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, badBody)); !isValidationError(err) {
		t.Fatalf("language email that does not parse: want validation error, got %v", err)
	}
	badSubject := f.row("de", &f.c1)
	badSubject.EmailID = nullable.NewNullableWithValue(f.email(&f.c1, "hallo", "betreff {{.FirstName"))
	_, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, badSubject))
	if !isValidationError(err) || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("language subject that does not parse: want validation error, got %v", err)
	}
	unknown := f.row("de", &f.c1)
	unknown.EmailID = nullable.NewNullableWithValue(uuid.New())
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, unknown)); !isValidationError(err) {
		t.Fatalf("missing language email: want validation error, got %v", err)
	}
	// an update checks only new or changed emails, so an email that later
	// stopped parsing does not block other edits
	good := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, good))
	if err := f.db.Model(&database.Email{}).Where("id = ?", good.EmailID.MustGet().String()).
		Update("content", "{{.Broken").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{good, f.row("fr", &f.c1)}),
	}); err != nil {
		t.Fatalf("edit next to a stored email refused: %v", err)
	}
}

// TestTemplateLanguagesStageShape checks the rows follow the page flow: a row
// can not set a page for a proxy stage, a stage that turns into a proxy is
// cleared on every row, and a new page stage makes rows that miss it not
// usable
func TestTemplateLanguagesStageShape(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	withBefore := f.row("de", &f.c1)
	withBefore.BeforeLandingPageID = nullable.NewNullableWithValue(f.page(&f.c1))
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, withBefore)); !isValidationError(err) {
		t.Fatalf("page for a stage the template does not have: want validation error, got %v", err)
	}
	missing := f.row("de", &f.c1)
	missing.AfterLandingPageID = nullable.Nullable[uuid.UUID]{}
	if _, err := f.svc.Create(f.ctx, f.session, f.languageTemplate(&f.c1, missing)); !isValidationError(err) {
		t.Fatalf("row missing a page stage: want validation error, got %v", err)
	}

	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1)))
	// the after stage becomes a proxy
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		AfterLandingProxyID: nullable.NewNullableWithValue(f.proxy()),
	}); err != nil {
		t.Fatalf("after stage to proxy: %v", err)
	}
	rows := f.storedLanguages(id)
	if len(rows) != 1 || rows[0].AfterLandingPageID.IsSpecified() && !rows[0].AfterLandingPageID.IsNull() {
		t.Fatalf("after page kept on the row: %v", rows[0].AfterLandingPageID)
	}
	if !rows[0].IsUsable {
		t.Fatal("row not usable after the after stage became a proxy")
	}
	// a before page is added without languages: the row misses it, so the
	// update is refused and nothing changes
	before := f.page(&f.c1)
	err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		BeforeLandingPageID: nullable.NewNullableWithValue(before),
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "German has no Before Landing page") {
		t.Fatalf("add before page without languages: want validation error naming German, got %v", err)
	}
	stored, err := f.repo.GetByID(f.ctx, &id, &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stored.BeforeLandingPageID.Get(); err == nil {
		t.Fatal("refused update stored the before page")
	}
	rows = f.storedLanguages(id)
	if !rows[0].IsUsable {
		t.Fatal("refused update made the row not usable")
	}
	// with the languages sent with a before page it is fine
	de := f.row("de", &f.c1)
	de.BeforeLandingPageID = nullable.NewNullableWithValue(f.page(&f.c1))
	de.AfterLandingPageID = nullable.Nullable[uuid.UUID]{}
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		BeforeLandingPageID: nullable.NewNullableWithValue(before),
		Languages:           nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{de}),
	}); err != nil {
		t.Fatalf("add before page with languages: %v", err)
	}
	rows = f.storedLanguages(id)
	if !rows[0].IsUsable {
		t.Fatal("row not usable with its before page")
	}
	// a row that is not usable already does not block an update
	if err := f.db.Model(&database.CampaignTemplateLanguage{}).Where("campaign_template_id = ?", id).
		Updates(map[string]any{"before_landing_page_id": nil, "is_usable": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		BeforeLandingPageID: nullable.NewNullableWithValue(f.page(&f.c1)),
	}); err != nil {
		t.Fatalf("update with a row that is not usable: %v", err)
	}
	// the loader refuses a row that is not usable
	if _, err := f.repo.GetByIDForLanguage(f.ctx, &id, "de", &repository.CampaignTemplateOption{WithEmail: true}); !errors.Is(err, repository.ErrTemplateLanguageUnavailable) {
		t.Fatalf("loader on a row that is not usable: %v", err)
	}
}

// TestTemplateLanguagesRemoveRefusedWhileStamped checks a language can not be
// removed while an open campaign has rows stamped with it, sent or not, also
// when the template language changes away from it. Closed campaigns and rows
// cancelled before they were sent do not count.
func TestTemplateLanguagesRemoveRefusedWhileStamped(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	rows := func(codes ...string) nullable.Nullable[[]*model.CampaignTemplateLanguage] {
		out := []*model.CampaignTemplateLanguage{}
		for _, code := range codes {
			out = append(out, f.row(code, &f.c1))
		}
		return nullable.NewNullableWithValue(out)
	}
	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1), f.row("fr", &f.c1), f.row("es", &f.c1)))
	unsent := f.campaignWithStamp(id, "de", false, false)
	sent := f.campaignWithStamp(id, "fr", true, false)
	// a closed campaign and a row cancelled before it was sent do not count
	f.campaignWithStamp(id, "es", false, true)
	cancelled := f.campaignWithStamp(id, "es", false, false)
	if err := f.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", cancelled).Update("cancelled_at", f.now).Error; err != nil {
		t.Fatal(err)
	}

	err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: rows()})
	if !isValidationError(err) || !strings.Contains(err.Error(), "German") || !strings.Contains(err.Error(), "campaign "+unsent.String()[:8]) {
		t.Fatalf("remove stamped language: want validation error naming German and the campaign, got %v", err)
	}
	if codes(f.storedLanguages(id)) != "de,es,fr" {
		t.Fatalf("refused update changed the rows: %s", codes(f.storedLanguages(id)))
	}
	// rows that are already sent keep their language too, their pages are
	// still served
	err = f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: rows("de", "es")})
	if !isValidationError(err) || !strings.Contains(err.Error(), "French") || !strings.Contains(err.Error(), "campaign "+sent.String()[:8]) {
		t.Fatalf("remove language of sent rows: want validation error naming French and the campaign, got %v", err)
	}
	// removing es is fine, changing the content of de and fr is fine
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: rows("de", "fr")}); err != nil {
		t.Fatalf("remove es and change de and fr: %v", err)
	}
	// the template language is a language too, also when every row is sent
	f.campaignWithStamp(id, "en", true, false)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language: nullable.NewNullableWithValue("it"),
	}); !isValidationError(err) {
		t.Fatalf("change template language with sent stamped rows: want validation error, got %v", err)
	}
	// a hidden campaign of a program is named by its program
	programID := uuid.New()
	f.create(&database.Program{ID: &programID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "named program"})
	f.db.Exec("DELETE FROM campaigns")
	hidden := f.campaignWithStamp(id, "fr", true, false)
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", hidden).Update("program_id", programID).Error; err != nil {
		t.Fatal(err)
	}
	err = f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: rows("de")})
	if !isValidationError(err) || !strings.Contains(err.Error(), "named program") {
		t.Fatalf("remove language of a program campaign: want validation error naming the program, got %v", err)
	}
	// once the campaigns are closed, the languages can go
	if err := f.db.Model(&database.Campaign{}).Where("closed_at IS NULL").Update("closed_at", f.now).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language:  nullable.NewNullableWithValue("it"),
		Languages: rows(),
	}); err != nil {
		t.Fatalf("remove after closing: %v", err)
	}
}

// TestTemplateLanguagesRemoveRefusedForCampaignRouting checks a language an
// open campaign picks by its default or a route can not be removed before any
// row is stamped, also when the template is only in the campaign pool
func TestTemplateLanguagesRemoveRefusedForCampaignRouting(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1), f.row("fr", &f.c1), f.row("es", &f.c1)))
	other := f.createTemplate(f.languageTemplate(&f.c1))
	campaign := func(name string, templateID uuid.UUID, routeBy, def string, routes map[string]string) uuid.UUID {
		cid := uuid.New()
		f.create(&database.Campaign{
			ID: &cid, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, SortField: "email", SortOrder: "asc",
			CampaignTemplateID: &templateID, LanguageRouteBy: routeBy, LanguageDefault: def, CompanyID: &f.c1,
		})
		for key, code := range routes {
			f.create(&database.CampaignLanguageRoute{CampaignID: &cid, MatchKey: key, Language: code})
		}
		return cid
	}
	campaign("default campaign", id, "country", "de", nil)
	pooled := campaign("pool campaign", other, "country", "en", map[string]string{"france": "fr"})
	f.create(&database.CampaignTemplatePool{CampaignID: &pooled, CampaignTemplateID: &other, Position: 0})
	f.create(&database.CampaignTemplatePool{CampaignID: &pooled, CampaignTemplateID: &id, Position: 1})
	// routing off: a stale default does not count
	campaign("routing off", id, "", "es", nil)
	// a route on a campaign that does not use the template does not count
	campaign("other template", other, "country", "en", map[string]string{"spain": "es"})

	keep := func(codes ...string) nullable.Nullable[[]*model.CampaignTemplateLanguage] {
		out := []*model.CampaignTemplateLanguage{}
		for _, code := range codes {
			out = append(out, f.row(code, &f.c1))
		}
		return nullable.NewNullableWithValue(out)
	}
	err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: keep("fr", "es")})
	if !isValidationError(err) || !strings.Contains(err.Error(), "default campaign") {
		t.Fatalf("remove the default language of a campaign: want validation error naming it, got %v", err)
	}
	err = f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: keep("de", "es")})
	if !isValidationError(err) || !strings.Contains(err.Error(), "pool campaign") {
		t.Fatalf("remove a routed language of a pool campaign: want validation error naming it, got %v", err)
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{Languages: keep("de", "fr")}); err != nil {
		t.Fatalf("remove a language no campaign picks: %v", err)
	}
}

// TestTemplateLanguagesRemoveRefusedForProgram checks a language an open
// program picks, as default or by a route, can not be removed, for drafts
// and started programs alike
func TestTemplateLanguagesRemoveRefusedForProgram(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1), f.row("fr", &f.c1)))
	program := func(name, def string, started bool, routes map[string]string) uuid.UUID {
		pid := uuid.New()
		p := &database.Program{ID: &pid, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, LanguageRouteBy: "country", LanguageDefault: def, CompanyID: &f.c1}
		if started {
			p.StartedAt = &f.now
		}
		f.create(p)
		stepID := uuid.New()
		f.create(&database.ProgramStep{ID: &stepID, CreatedAt: &f.now, UpdatedAt: &f.now, ProgramID: &pid, CampaignTemplateID: &id})
		for key, code := range routes {
			f.create(&database.ProgramLanguageRoute{ProgramID: &pid, MatchKey: key, Language: code})
		}
		return pid
	}
	draft := program("draft program", "de", false, nil)
	program("routing program", "en", true, map[string]string{"france": "fr"})

	// de is picked by a draft
	err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{f.row("fr", &f.c1)}),
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "draft program") {
		t.Fatalf("remove de used by a draft: want validation error naming it, got %v", err)
	}
	err = f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{f.row("de", &f.c1)}),
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "routing program") {
		t.Fatalf("remove fr routed by a started program: want validation error naming it, got %v", err)
	}
	// a closed program does not count
	if err := f.db.Model(&database.Program{}).Where("id = ?", draft).Update("closed_at", f.now).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{f.row("fr", &f.c1)}),
	}); err != nil {
		t.Fatalf("remove de used by a closed program: %v", err)
	}
	program("default program", "en", true, nil)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language: nullable.NewNullableWithValue("it"),
	}); !isValidationError(err) || !strings.Contains(err.Error(), "default program") {
		t.Fatalf("change template language picked by a started program: got %v", err)
	}
}

// TestTemplateLanguageMigration checks the new tables and columns migrate
// twice without change and existing rows read as no language
func TestTemplateLanguageMigration(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	legacy := uuid.New()
	urlID, stateID := uuid.New(), uuid.New()
	if err := f.db.Exec(`INSERT INTO campaign_templates (id, created_at, updated_at, name, url_identifier_id, state_identifier_id)
		VALUES (?, ?, ?, ?, ?, ?)`, legacy.String(), f.now, f.now, "legacy", urlID.String(), stateID.String()).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.db.AutoMigrate(
			&database.CampaignTemplate{},
			&database.CampaignTemplateLanguage{},
			&database.Campaign{},
			&database.CampaignLanguageRoute{},
			&database.CampaignRecipient{},
			&database.Program{},
			&database.ProgramLanguageRoute{},
		); err != nil {
			t.Fatalf("migrate %d: %v", i, err)
		}
	}
	tmpl, err := f.repo.GetByID(f.ctx, &legacy, &repository.CampaignTemplateOption{})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.TemplateLanguage() != "" {
		t.Fatalf("legacy template language: %q", tmpl.TemplateLanguage())
	}
	for _, c := range []struct {
		model  any
		column string
	}{
		{&database.CampaignTemplate{}, "language"},
		{&database.CampaignRecipient{}, "language"},
		{&database.Campaign{}, "language_route_by"},
		{&database.Campaign{}, "language_default"},
		{&database.Program{}, "language_route_by"},
		{&database.Program{}, "language_default"},
	} {
		if !f.db.Migrator().HasColumn(c.model, c.column) {
			t.Errorf("%T has no column %s", c.model, c.column)
		}
	}
}

// TestTemplateLanguagesAudit checks the template audit events name the added,
// removed and changed languages and the template language before and after
func TestTemplateLanguagesAudit(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	logger, buf := bufferLogger(zapcore.InfoLevel)
	f.svc.AuditLogger = logger
	de := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, de, f.row("fr", &f.c1)))
	changed := f.row("de", &f.c1)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language:  nullable.NewNullableWithValue("it"),
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{changed, f.row("da", &f.c1)}),
	}); err != nil {
		t.Fatal(err)
	}
	events := auditLines(t, buf)
	details := func(name string) map[string]any {
		if len(events[name]) != 1 {
			t.Fatalf("%s: %d events", name, len(events[name]))
		}
		d, _ := events[name][0]["details"].(map[string]any)
		return d
	}
	created := details("CampaignTemplate.Create")
	if created["language"] != "en" || !strings.Contains(strings.Join(anyStrings(created["languagesAdded"]), ","), "de,fr") {
		t.Fatalf("create details: %v", created)
	}
	updated := details("CampaignTemplate.UpdateByID")
	if updated["previousLanguage"] != "en" || updated["language"] != "it" {
		t.Fatalf("template language in update details: %v", updated)
	}
	if strings.Join(anyStrings(updated["languagesAdded"]), ",") != "da" || strings.Join(anyStrings(updated["languagesRemoved"]), ",") != "fr" {
		t.Fatalf("added or removed in update details: %v", updated)
	}
	changes, _ := updated["languagesChanged"].(map[string]any)
	deIDs, _ := changes["de"].(map[string]any)
	if deIDs["emailId"] != changed.EmailID.MustGet().String() {
		t.Fatalf("changed in update details: %v", updated["languagesChanged"])
	}
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// TestTemplateListCarriesLanguages checks the template list gives each
// template its language rows when asked, read for the whole page in one
// query, and leaves them out otherwise
func TestTemplateListCarriesLanguages(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	withRows := f.createTemplate(f.languageTemplate(&f.c1, f.row("fr", &f.c1), f.row("de", &f.c1)))
	plain := f.createTemplate(f.languageTemplate(&f.c1))
	for i := 0; i < 3; i++ {
		f.createTemplate(f.languageTemplate(&f.c1, f.row("da", &f.c1)))
	}

	// count the reads of the language table
	reads := 0
	if err := f.db.Callback().Query().After("gorm:query").Register("count_language_reads", func(db *gorm.DB) {
		if db.Statement.Table == database.CAMPAIGN_TEMPLATE_LANGUAGE_TABLE {
			reads++
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	list := func(withLanguages bool) map[uuid.UUID]*model.CampaignTemplate {
		t.Helper()
		result, err := f.svc.GetAll(f.ctx, f.session, &f.c1, nil, &repository.CampaignTemplateOption{
			QueryArgs:     &vo.QueryArgs{Limit: 25},
			WithLanguages: withLanguages,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := map[uuid.UUID]*model.CampaignTemplate{}
		for _, row := range result.Rows {
			out[row.ID.MustGet()] = row
		}
		return out
	}

	rows := list(true)
	if len(rows) != 5 {
		t.Fatalf("want 5 templates, got %d", len(rows))
	}
	if reads != 1 {
		t.Fatalf("the language rows of a page are read in one query, got %d", reads)
	}
	got := rows[withRows]
	if got.TemplateLanguage() != "en" {
		t.Fatalf("want the template language en, got %q", got.TemplateLanguage())
	}
	languages, err := got.Languages.Get()
	if err != nil || codes(languages) != "de,fr" {
		t.Fatalf("want rows de,fr, got %v %v", codes(languages), err)
	}
	for _, l := range languages {
		if !l.IsUsable || l.EmailName != "" {
			t.Fatalf("a list row is usable and carries no names: %+v", l)
		}
	}
	if languages, err := rows[plain].Languages.Get(); err != nil || len(languages) != 0 {
		t.Fatalf("a template with no rows lists an empty array, got %v %v", languages, err)
	}

	reads = 0
	rows = list(false)
	if reads != 0 || rows[withRows].Languages.IsSpecified() {
		t.Fatalf("without the option no rows are read, got %d reads", reads)
	}
}

// TestLanguageRowDoesNotReuseMainLink checks an update can not copy an id the
// template holds in its own fields into a language row without the company
// check, while a language row may keep what a stored language row holds.
// Both links are stored straight in the database, as links from before the
// check existed.
func TestLanguageRowDoesNotReuseMainLink(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	stored := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, stored))
	legacy := f.email(&f.c2, "x", "y")
	if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", id.String()).
		Update("email_id", legacy.String()).Error; err != nil {
		t.Fatal(err)
	}
	copied := f.row("fr", &f.c1)
	copied.EmailID = nullable.NewNullableWithValue(legacy)
	err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{stored, copied}),
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "another company") {
		t.Fatalf("main field id copied to a language row: want company refusal, got %v", err)
	}
	if codes(f.storedLanguages(id)) != "de" {
		t.Fatalf("refused update changed the rows: %s", codes(f.storedLanguages(id)))
	}

	// a stored language row with a legacy link keeps it on update
	if err := f.db.Model(&database.CampaignTemplateLanguage{}).
		Where("campaign_template_id = ? AND language = ?", id.String(), "de").
		Update("email_id", legacy.String()).Error; err != nil {
		t.Fatal(err)
	}
	stored.EmailID = nullable.NewNullableWithValue(legacy)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{stored, f.row("fr", &f.c1)}),
	}); err != nil {
		t.Fatalf("a stored language row link must stay allowed: %v", err)
	}
}

// TestLanguageRefusalNamesOnlyOwnCompany checks the refusal to remove a
// language of a global template names only global campaigns and programs and
// counts those of companies, and that a company template names its own
func TestLanguageRefusalNamesOnlyOwnCompany(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	global := f.createTemplate(f.languageTemplate(nil, f.row("de", nil)))
	open := func(name string, companyID *uuid.UUID) {
		cid, rowID, recipientID := uuid.New(), uuid.New(), uuid.New()
		code := "de"
		f.create(&database.Campaign{
			ID: &cid, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, SortField: "email", SortOrder: "asc",
			CampaignTemplateID: &global, CompanyID: companyID,
		})
		f.create(&database.CampaignRecipient{
			ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignID: &cid,
			RecipientID: &recipientID, CampaignTemplateID: &global, Language: &code, SendAt: &f.now,
		})
	}
	open("global campaign", nil)
	open("first company campaign", &f.c1)
	open("second company campaign", &f.c2)
	err := f.svc.UpdateByID(f.ctx, f.session, &global, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{}),
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "global campaign and 2 in other companies") ||
		strings.Contains(err.Error(), "company campaign") {
		t.Fatalf("want only the global campaign named and two counted, got %v", err)
	}

	// programs of a company are counted the same way
	programTemplate := f.createTemplate(f.languageTemplate(nil, f.row("de", nil)))
	pid, stepID := uuid.New(), uuid.New()
	f.create(&database.Program{ID: &pid, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "company program",
		LanguageRouteBy: "country", LanguageDefault: "de", CompanyID: &f.c1})
	f.create(&database.ProgramStep{ID: &stepID, CreatedAt: &f.now, UpdatedAt: &f.now, ProgramID: &pid, CampaignTemplateID: &programTemplate})
	err = f.svc.UpdateByID(f.ctx, f.session, &programTemplate, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{}),
	})
	if !isValidationError(err) || !strings.HasSuffix(err.Error(), "pick it: 1 in other companies") {
		t.Fatalf("want the company program counted, got %v", err)
	}

	// a company template names the campaigns of its company
	own := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1)))
	named := f.campaignWithStamp(own, "de", false, false)
	err = f.svc.UpdateByID(f.ctx, f.session, &own, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{}),
	})
	if !isValidationError(err) || !strings.Contains(err.Error(), "campaign "+named.String()[:8]) ||
		strings.Contains(err.Error(), "other companies") {
		t.Fatalf("want the company campaign named, got %v", err)
	}
}

package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// nullRow stores a recipient with no language on campaignID. With onRow the
// row names templateID itself, else it uses the campaign template.
func (f *templateLanguageFixture) nullRow(campaignID uuid.UUID, templateID uuid.UUID, onRow bool, sent bool) uuid.UUID {
	f.t.Helper()
	rowID, recipientID := uuid.New(), uuid.New()
	cr := &database.CampaignRecipient{
		ID: &rowID, CreatedAt: &f.now, UpdatedAt: &f.now, CampaignID: &campaignID,
		RecipientID: &recipientID, SendAt: &f.now,
	}
	if onRow {
		cr.CampaignTemplateID = &templateID
	}
	if sent {
		cr.SentAt = &f.now
	}
	f.create(cr)
	return rowID
}

// stampOfRow returns the language of a recipient row, "null" when it has none
func (f *templateLanguageFixture) stampOfRow(rowID uuid.UUID) string {
	f.t.Helper()
	var row database.CampaignRecipient
	if err := f.db.Where("id = ?", rowID).First(&row).Error; err != nil {
		f.t.Fatalf("load recipient row: %v", err)
	}
	if row.Language == nil {
		return "null"
	}
	return *row.Language
}

// landingFor returns the landing page a recipient stamped with code is served
func (f *templateLanguageFixture) landingFor(templateID uuid.UUID, code string) uuid.UUID {
	f.t.Helper()
	tmpl, err := f.repo.GetByIDForLanguage(f.ctx, &templateID, code, &repository.CampaignTemplateOption{})
	if err != nil {
		f.t.Fatalf("load template in %q: %v", code, err)
	}
	return tmpl.LandingPageID.MustGet()
}

func (f *templateLanguageFixture) languageTotals(campaignID uuid.UUID) map[string]int {
	f.t.Helper()
	stats, err := (&repository.Campaign{DB: f.db}).GetGroupedResultStats(f.ctx, &campaignID, repository.GroupedStatByLanguage)
	if err != nil {
		f.t.Fatalf("stats by language: %v", err)
	}
	out := map[string]int{}
	for _, s := range stats {
		out[s.GroupID] = s.Total
	}
	return out
}

// rowFromTemplate is a language row holding the template's own email and pages
func rowFromTemplate(code string, t *model.CampaignTemplate) *model.CampaignTemplateLanguage {
	return &model.CampaignTemplateLanguage{
		Language:           code,
		EmailID:            t.EmailID,
		LandingPageID:      t.LandingPageID,
		AfterLandingPageID: t.AfterLandingPageID,
	}
}

func (f *templateLanguageFixture) storedTemplate(id uuid.UUID) *model.CampaignTemplate {
	f.t.Helper()
	tmpl, err := f.repo.GetByID(f.ctx, &id, &repository.CampaignTemplateOption{})
	if err != nil {
		f.t.Fatalf("load template: %v", err)
	}
	return tmpl
}

// TestMainSwapStampsRecipientsWithNoLanguage checks that a template without a
// code that gets one, with its old content moved to a language row, stamps
// every recipient with no language on it with the code of that content: in
// open and closed campaigns, sent or not, rows that use the campaign template
// and program sends. Recipients of other templates are left alone, the people
// keep getting the old pages and their results stay together.
func TestMainSwapStampsRecipientsWithNoLanguage(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	if err := f.db.AutoMigrate(&database.CampaignEvent{}); err != nil {
		t.Fatal(err)
	}
	plain := f.languageTemplate(&f.c1)
	plain.Language = nullable.Nullable[string]{}
	id := f.createTemplate(plain)
	other := f.languageTemplate(&f.c1)
	other.Language = nullable.Nullable[string]{}
	otherID := f.createTemplate(other)
	stored := f.storedTemplate(id)
	oldLanding := stored.LandingPageID.MustGet()

	active := f.openCampaign(id, "", nil)
	sent := f.nullRow(active, id, true, true)
	unsent := f.nullRow(active, id, true, false)
	legacy := f.nullRow(active, id, false, true)
	closed := f.campaignWithStamp(id, "en", true, true)
	closedNull := f.nullRow(closed, id, true, true)
	programID := uuid.New()
	program := f.openCampaign(id, "", &programID)
	programSend := f.nullRow(program, id, true, true)
	otherCampaign := f.openCampaign(otherID, "", nil)
	untouched := f.nullRow(otherCampaign, otherID, true, true)
	if got := f.languageTotals(active); got[""] != 3 {
		t.Fatalf("before: want 3 recipients with no language, got %v", got)
	}

	// Multiple languages with a new Danish main and the old content as English
	da := f.row("da", &f.c1)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language:           nullable.NewNullableWithValue("da"),
		EmailID:            da.EmailID,
		LandingPageID:      da.LandingPageID,
		AfterLandingPageID: da.AfterLandingPageID,
		Languages:          nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{rowFromTemplate("en", stored)}),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	for name, row := range map[string]uuid.UUID{
		"sent": sent, "unsent": unsent, "campaign template": legacy,
		"closed campaign": closedNull, "program send": programSend,
	} {
		if got := f.stampOfRow(row); got != "en" {
			t.Fatalf("%s: want en, got %s", name, got)
		}
	}
	if got := f.stampOfRow(untouched); got != "null" {
		t.Fatalf("other template row was stamped: %s", got)
	}
	if got := f.landingFor(id, "en"); got != oldLanding {
		t.Fatalf("sent people get another landing page: %v, want %v", got, oldLanding)
	}
	if got := f.languageTotals(active); got["en"] != 3 || len(got) != 1 {
		t.Fatalf("after: want the 3 recipients together as English, got %v", got)
	}
}

// TestMakeMainKeepsLanguageOfRecipientsWithNoLanguage checks Make main on a
// template with a code: recipients with no language were getting the old main
// content, so they are stamped with the old code and keep its pages and their
// results group
func TestMakeMainKeepsLanguageOfRecipientsWithNoLanguage(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	if err := f.db.AutoMigrate(&database.CampaignEvent{}); err != nil {
		t.Fatal(err)
	}
	de := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, de))
	stored := f.storedTemplate(id)
	oldLanding := stored.LandingPageID.MustGet()
	active := f.openCampaign(id, "", nil)
	sent := f.nullRow(active, id, true, true)
	unsent := f.nullRow(active, id, true, false)
	programID := uuid.New()
	programSend := f.nullRow(f.openCampaign(id, "", &programID), id, true, false)
	if got := f.languageTotals(active); got["en"] != 2 {
		t.Fatalf("before: want 2 English, got %v", got)
	}

	// Make main on German: German moves to the template fields, English to a row
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language:           nullable.NewNullableWithValue("de"),
		EmailID:            de.EmailID,
		LandingPageID:      de.LandingPageID,
		AfterLandingPageID: de.AfterLandingPageID,
		Languages:          nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{rowFromTemplate("en", stored)}),
	}); err != nil {
		t.Fatalf("make main: %v", err)
	}
	for name, row := range map[string]uuid.UUID{"sent": sent, "unsent": unsent, "program send": programSend} {
		if got := f.stampOfRow(row); got != "en" {
			t.Fatalf("%s: want en, got %s", name, got)
		}
	}
	if got := f.landingFor(id, "en"); got != oldLanding {
		t.Fatalf("sent people get another landing page: %v, want %v", got, oldLanding)
	}
	if got := f.landingFor(id, ""); got != de.LandingPageID.MustGet() {
		t.Fatalf("the main content is not German: %v", got)
	}
	if got := f.languageTotals(active); got["en"] != 2 || len(got) != 1 {
		t.Fatalf("after: want 2 English, got %v", got)
	}
}

// TestCodeChangeStampsRecipientsWithNoLanguage checks the other ways a code
// changes: a code given to a template with the same content stamps the new
// code. New content that matches no language on a template that had no code
// stamps nothing, like an edit of the email. Edits that keep the code stamp
// nothing.
func TestCodeChangeStampsRecipientsWithNoLanguage(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	plain := f.languageTemplate(&f.c1)
	plain.Language = nullable.Nullable[string]{}
	id := f.createTemplate(plain)
	campaign := f.openCampaign(id, "", nil)
	first := f.nullRow(campaign, id, true, true)

	// an edit with no code stamps nothing
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		EmailID: nullable.NewNullableWithValue(f.email(&f.c1, "hi {{.FirstName}}", "s")),
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.stampOfRow(first); got != "null" {
		t.Fatalf("edit without a code stamped %s", got)
	}
	// the same content gets a code
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language: nullable.NewNullableWithValue("de"),
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.stampOfRow(first); got != "de" {
		t.Fatalf("code given: want de, got %s", got)
	}
	// an edit that keeps the code stamps nothing
	second := f.nullRow(campaign, id, true, false)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		EmailID: nullable.NewNullableWithValue(f.email(&f.c1, "hallo {{.FirstName}}", "s")),
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.stampOfRow(second); got != "null" {
		t.Fatalf("edit keeping the code stamped %s", got)
	}

	// a template with no code gets new content in every language
	plain = f.languageTemplate(&f.c1)
	plain.Language = nullable.Nullable[string]{}
	id = f.createTemplate(plain)
	row := f.nullRow(f.openCampaign(id, "", nil), id, true, true)
	fr := f.row("fr", &f.c1)
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Language:  nullable.NewNullableWithValue("it"),
		EmailID:   nullable.NewNullableWithValue(f.email(&f.c1, "ciao {{.FirstName}}", "s")),
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{fr}),
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.stampOfRow(row); got != "null" {
		t.Fatalf("no matching content and no old code: want no stamp, got %s", got)
	}
}

// TestDroppedMainKeepsOldLanguage checks that recipients with no language
// keep the old template language when the update leaves no content that
// matches what they were sent: the old main dropped for a new main, or the
// code cleared. The update is refused while an open campaign has such
// recipients, as the old language is gone, and the refusal names only that
// language.
func TestDroppedMainKeepsOldLanguage(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	de := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, de))
	closed := f.campaignWithStamp(id, "de", true, true)
	closedNull := f.nullRow(closed, id, true, true)
	open := f.openCampaign(id, "", nil)
	f.nullRow(open, id, true, true)
	var openName string
	f.db.Model(&database.Campaign{}).Where("id = ?", open).Select("name").Scan(&openName)

	// a new French main with new content, English and German dropped
	update := func() error {
		fr := f.row("fr", &f.c1)
		return f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
			Language:           nullable.NewNullableWithValue("fr"),
			EmailID:            fr.EmailID,
			LandingPageID:      fr.LandingPageID,
			AfterLandingPageID: fr.AfterLandingPageID,
			Languages:          nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{}),
		})
	}
	err := update()
	want := "English can not be removed while open campaigns use it: " + openName
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want refusal %q, got %v", want, err)
	}
	if strings.Contains(err.Error(), "German") {
		t.Fatalf("refusal names a language not in use: %v", err)
	}
	f.db.Model(&database.Campaign{}).Where("id = ?", open).Update("closed_at", f.now)
	if err := update(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := f.stampOfRow(closedNull); got != "en" {
		t.Fatalf("dropped main: want en, got %s", got)
	}

	// the code of a single language template is cleared
	single := f.createTemplate(f.languageTemplate(&f.c1))
	row := f.nullRow(f.campaignWithStamp(single, "en", true, true), single, true, true)
	if err := f.svc.UpdateByID(f.ctx, f.session, &single, &model.CampaignTemplate{
		Language: nullable.NewNullableWithValue(""),
	}); err != nil {
		t.Fatalf("clear code: %v", err)
	}
	if got := f.stampOfRow(row); got != "en" {
		t.Fatalf("cleared code: want en, got %s", got)
	}
}

// TestRefusalNamesOnlyLanguagesInUse checks that removing two languages of
// which an open campaign uses one names only that one
func TestRefusalNamesOnlyLanguagesInUse(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	id := f.createTemplate(f.languageTemplate(&f.c1, f.row("de", &f.c1), f.row("fr", &f.c1)))
	f.openCampaign(id, "", nil, "de")
	err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{}),
	})
	if err == nil || !strings.Contains(err.Error(), "German can not be removed while open campaigns use it") ||
		strings.Contains(err.Error(), "French") {
		t.Fatalf("want a refusal naming only German, got %v", err)
	}
	// the unused language alone can be removed
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		Languages: nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{languageRowOf(f.languageRow(id, "de"))}),
	}); err != nil {
		t.Fatalf("remove French: %v", err)
	}
}

// languageRowOf is the update form of a stored language row
func languageRowOf(row database.CampaignTemplateLanguage) *model.CampaignTemplateLanguage {
	l := &model.CampaignTemplateLanguage{Language: row.Language}
	if row.EmailID != nil {
		l.EmailID = nullable.NewNullableWithValue(*row.EmailID)
	}
	if row.BeforeLandingPageID != nil {
		l.BeforeLandingPageID = nullable.NewNullableWithValue(*row.BeforeLandingPageID)
	}
	if row.LandingPageID != nil {
		l.LandingPageID = nullable.NewNullableWithValue(*row.LandingPageID)
	}
	if row.AfterLandingPageID != nil {
		l.AfterLandingPageID = nullable.NewNullableWithValue(*row.AfterLandingPageID)
	}
	return l
}

// TestLeftoverRowPageDeleteKeepsRowUsable checks that deleting a row page of
// a stage the template no longer has as a page, after its main page was
// deleted, keeps the row usable and its campaigns open
func TestLeftoverRowPageDeleteKeepsRowUsable(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	de := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, de))
	stored := f.storedTemplate(id)
	campaign := f.openCampaign(id, "", nil, "de")

	if err := f.repo.RemovePageIDFromAll(f.ctx, nullablePtr[uuid.UUID](stored.AfterLandingPageID)); err != nil {
		t.Fatal(err)
	}
	// the German after page is not served any more
	if err := f.svc.RemovePagesByPageID(f.ctx, f.session, nullablePtr[uuid.UUID](de.AfterLandingPageID)); err != nil {
		t.Fatalf("remove German after page: %v", err)
	}
	row := f.languageRow(id, "de")
	if row.AfterLandingPageID != nil || !row.IsUsable {
		t.Fatalf("want the page cleared and the row usable, got %+v", row)
	}
	if f.closed(campaign) {
		t.Fatal("the campaign was closed")
	}
	// the German landing page is still a stage of the template
	if err := f.svc.RemovePagesByPageID(f.ctx, f.session, nullablePtr[uuid.UUID](de.LandingPageID)); err != nil {
		t.Fatalf("remove German landing page: %v", err)
	}
	if row := f.languageRow(id, "de"); row.IsUsable {
		t.Fatal("a row without its landing page is usable")
	}
	if !f.closed(campaign) {
		t.Fatal("the campaign using German was not closed")
	}
}

// TestTemplatePageDeleteKeepsLanguagePages checks that deleting the
// template's own page keeps that stage on its language rows, so a new main
// page can be set without sending the languages again. An update that turns
// the stage into a proxy still clears it on the rows.
func TestTemplatePageDeleteKeepsLanguagePages(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	de := f.row("de", &f.c1)
	id := f.createTemplate(f.languageTemplate(&f.c1, de))
	stored := f.storedTemplate(id)

	if err := f.repo.RemovePageIDFromAll(f.ctx, nullablePtr[uuid.UUID](stored.LandingPageID)); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.RemovePageIDFromAll(f.ctx, nullablePtr[uuid.UUID](stored.AfterLandingPageID)); err != nil {
		t.Fatal(err)
	}
	row := f.languageRow(id, "de")
	if row.LandingPageID == nil || *row.LandingPageID != de.LandingPageID.MustGet() ||
		row.AfterLandingPageID == nil || *row.AfterLandingPageID != de.AfterLandingPageID.MustGet() {
		t.Fatalf("the language row lost its pages: %+v", row)
	}
	if !row.IsUsable {
		t.Fatal("the language row is not usable")
	}
	// a new main landing page without the languages
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		LandingPageID: nullable.NewNullableWithValue(f.page(&f.c1)),
	}); err != nil {
		t.Fatalf("new main landing page: %v", err)
	}
	row = f.languageRow(id, "de")
	if row.LandingPageID == nil || row.AfterLandingPageID == nil || !row.IsUsable {
		t.Fatalf("the language row lost its pages on update: %+v", row)
	}
	// the after stage, empty since its page was deleted, becomes a proxy
	if err := f.svc.UpdateByID(f.ctx, f.session, &id, &model.CampaignTemplate{
		AfterLandingProxyID: nullable.NewNullableWithValue(f.proxy()),
	}); err != nil {
		t.Fatalf("after stage to proxy: %v", err)
	}
	row = f.languageRow(id, "de")
	if row.AfterLandingPageID != nil || row.LandingPageID == nil {
		t.Fatalf("proxy stage: want only the after page cleared, got %+v", row)
	}
}

package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
)

// breakLanguageEmail makes the email of a template language row fail to parse
func (f *poolFixture) breakLanguageEmail(templateID uuid.UUID, code string) {
	f.t.Helper()
	var row database.CampaignTemplateLanguage
	if err := f.db.Where("campaign_template_id = ? AND language = ?", templateID.String(), code).First(&row).Error; err != nil {
		f.t.Fatalf("read language row: %v", err)
	}
	if err := f.db.Model(&database.Email{}).Where("id = ?", row.EmailID.String()).
		Update("subject", "Hej {{.FirstName").Error; err != nil {
		f.t.Fatalf("break email: %v", err)
	}
}

// TestLanguageFieldLateScheduleChecksReachableOnly checks a late schedule
// with Language by Language checks the emails of the default and of the
// languages the new rows get: a broken Danish email does not stop a
// campaign where nobody has Danish, and closes it once a Dane joins
func TestLanguageFieldLateScheduleChecksReachableOnly(t *testing.T) {
	setup := func() (*poolFixture, uuid.UUID, uuid.UUID, uuid.UUID) {
		f := newLanguagePoolFixture(t)
		tmpl := f.languageTemplate("en", "da", "de")
		f.breakLanguageEmail(tmpl, "da")
		group := f.dynamicGroup("department", "Sales")
		german := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("de")})
		none := f.lateMember(nil, lateRecipient{department: "Sales"})
		campaignID := f.campaign([]uuid.UUID{tmpl}, false, false)
		f.route(campaignID, model.LanguageRouteLanguage, "en", nil)
		f.lateCampaign(campaignID, group)
		return f, campaignID, german, none
	}

	// nobody has Danish: scheduled, not closed
	f, campaignID, german, none := setup()
	// a recipient with a language no template has gets the default
	italian := f.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("it")})
	f.makeDue(campaignID)
	checkStamps(t, "no Danish", f.stamps(campaignID), map[uuid.UUID]string{
		german: "de", none: "en", italian: "en",
	})

	// a Dane joins before the schedule time: closed with the reason, no rows
	g, campaign2, _, _ := setup()
	g.lateMember(nil, lateRecipient{department: "Sales", language: strPtr("da")})
	if err := g.db.Model(&database.Campaign{}).Where("id = ?", campaign2.String()).
		Update("schedule_at", utils.RFC3339UTC(g.now.Add(-time.Minute))).Error; err != nil {
		t.Fatal(err)
	}
	g.runLateSchedule()
	var stored database.Campaign
	if err := g.db.Where("id = ?", campaign2.String()).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClosedAt == nil {
		t.Fatal("a campaign whose Danish recipient has a broken email must be closed")
	}
	if rows := g.stamps(campaign2); len(rows) != 0 {
		t.Fatalf("no row may be scheduled, got %d", len(rows))
	}
	var event database.CampaignEvent
	if err := g.db.Where("campaign_id = ? AND event_id = ?", campaign2.String(),
		cache.EventIDByName[data.EVENT_CAMPAIGN_CLOSED].String()).First(&event).Error; err != nil {
		t.Fatalf("close event: %v", err)
	}
	if !strings.Contains(event.Data, "Danish") {
		t.Fatalf("close reason must name Danish: %q", event.Data)
	}
}

// TestLanguageFieldSaveChecksReachableOnly checks the save of a campaign with
// Language by Language checks the emails of the default and of the languages
// the group members have, not of every language the templates can send
func TestLanguageFieldSaveChecksReachableOnly(t *testing.T) {
	f := newLanguagePoolFixture(t)
	tmpl := f.languageTemplate("en", "da", "de")
	f.breakLanguageEmail(tmpl, "da")
	groupID := f.group(nil)
	german := f.languageRecipient(groupID, strPtr("de"))
	none := f.languageRecipient(groupID, nil)
	campaignID, _ := f.selfManagedScheduled([]uuid.UUID{tmpl}, groupID, false, model.LanguageRouteLanguage, nil)
	checkStamps(t, "scheduled", f.stamps(campaignID), map[uuid.UUID]string{german: "de", none: "en"})

	update := func() error {
		t.Helper()
		return f.svc.UpdateByID(f.ctx, f.session, &campaignID, selfManagedUpdate(f.loadCampaign(campaignID, groupID), groupID))
	}
	if err := update(); err != nil {
		t.Fatalf("save with a broken email in a language nobody has refused: %v", err)
	}
	// a Dane in the group makes Danish reachable
	dane := f.languageRecipient(groupID, strPtr("da"))
	err := update()
	if !isValidationError(err) || !strings.Contains(err.Error(), "Danish") {
		t.Fatalf("save with a Danish recipient: want a refusal naming Danish, got %v", err)
	}
	if _, ok := f.stamps(campaignID)[dane]; ok {
		t.Fatal("the Dane must not get a row")
	}

	// a broken default is refused even when nobody has the default language
	f2 := newLanguagePoolFixture(t)
	tmpl2 := f2.languageTemplate("en", "en", "de")
	f2.breakLanguageEmail(tmpl2, "en")
	group2 := f2.group(nil)
	f2.languageRecipient(group2, strPtr("de"))
	if _, err := f2.svc.checkPoolLanguages(f2.ctx, nil, model.LanguageRouteLanguage, "en", nil, []uuid.UUID{tmpl2}, []string{"de"}); !isValidationError(err) || !strings.Contains(err.Error(), "English") {
		t.Fatalf("broken default: want a refusal naming English, got %v", err)
	}
}

// TestCheckStampEmails checks the languages a set of recipients gets are the
// ones whose emails are checked: a language that falls back to the default is
// not checked on its own
func TestCheckStampEmails(t *testing.T) {
	f := newLanguagePoolFixture(t)
	full := f.languageTemplate("en", "da", "de")
	partial := f.languageTemplate("en", "de")
	f.breakLanguageEmail(full, "da")
	l := &campaignLanguages{
		pool: []uuid.UUID{full, partial}, routeBy: model.LanguageRouteLanguage, fallback: "en",
		routes: map[string]string{}, memo: map[string]string{},
	}
	sets, err := f.svc.CampaignTemplateService.CampaignTemplateRepository.GetLanguageSetsByIDs(f.ctx, l.pool)
	if err != nil {
		t.Fatal(err)
	}
	l.sets = sets
	// Danish is not on the partial template, so a Dane gets English
	if err := f.svc.checkStampEmails(f.ctx, l, []string{"da", "de", "", "xx"}); err != nil {
		t.Fatalf("Danish falls back to English, its email is not used: %v", err)
	}
	l.pool = []uuid.UUID{full}
	l.sets = map[uuid.UUID]*repository.TemplateLanguageSet{full: sets[full]}
	l.memo = map[string]string{}
	if err := f.svc.checkStampEmails(f.ctx, l, []string{"de"}); err != nil {
		t.Fatalf("no Dane: %v", err)
	}
	if err := f.svc.checkStampEmails(f.ctx, l, []string{"de", "Danish"}); !isValidationError(err) || !strings.Contains(err.Error(), "Danish") {
		t.Fatalf("a Dane: want a refusal naming Danish, got %v", err)
	}
	// other routings are checked when the languages are read, not here
	l.routeBy = model.LanguageRouteCountry
	if err := f.svc.checkStampEmails(f.ctx, l, []string{"da"}); err != nil {
		t.Fatalf("country routing: %v", err)
	}
}

// TestProgramLanguageFieldChecksReachableOnly checks a program with Language
// by Language checks the emails of the default and of the languages the
// members of its groups have
func TestProgramLanguageFieldChecksReachableOnly(t *testing.T) {
	p := newProgramLanguageTest(t)
	p.svc.RecipientGroupRepository = &repository.RecipientGroup{DB: p.db}
	programID := p.program(model.ProgramLoopComplete)
	tmpl := p.languageTemplate("first", "en", "de")
	broken := p.languageEmail("da")
	p.db.Model(&database.Email{}).Where("id = ?", broken.String()).Update("subject", "Hej {{.FirstName")
	p.languageTemplateRow(tmpl, "da", broken, true)
	p.templateStep(programID, tmpl, 0)
	groupID := p.group("people")
	member := func(language string) {
		id := p.recipient(uuid.NewString() + "@example.com")
		p.db.Model(&database.Recipient{}).Where("id = ?", id).Update("language", language)
		p.create(&database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: &groupID})
	}
	member("de")
	steps := p.loadProgram(programID).Steps
	groups := []*uuid.UUID{&groupID}
	if err := p.svc.checkProgramLanguages(p.ctx, nil, model.LanguageRouteLanguage, "en", nil, steps, groups); err != nil {
		t.Fatalf("broken email in a language nobody has refused: %v", err)
	}
	member("da")
	err := p.svc.checkProgramLanguages(p.ctx, nil, model.LanguageRouteLanguage, "en", nil, steps, groups)
	if !isValidationError(err) || !strings.Contains(err.Error(), "Danish") {
		t.Fatalf("a Dane in the group: want a refusal naming Danish, got %v", err)
	}
}

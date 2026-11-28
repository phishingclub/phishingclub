package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
)

// withTemplateLanguage gives the step template of the fixture campaign a
// template language and lets the program service read it
func (f *resultsFixture) withTemplateLanguage(code string) {
	f.t.Helper()
	var link database.ProgramCampaign
	if err := f.db.Where("campaign_id = ?", f.campaignID).First(&link).Error; err != nil {
		f.t.Fatal(err)
	}
	var step database.ProgramStep
	if err := f.db.Where("id = ?", link.ProgramStepID).First(&step).Error; err != nil {
		f.t.Fatal(err)
	}
	// a hidden campaign sends the template of its step
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", f.campaignID).
		Update("campaign_template_id", step.CampaignTemplateID).Error; err != nil {
		f.t.Fatal(err)
	}
	if err := f.db.Model(&database.CampaignTemplate{}).Where("id = ?", step.CampaignTemplateID).
		Update("language", code).Error; err != nil {
		f.t.Fatal(err)
	}
	if f.svc.CampaignTemplateService == nil {
		f.svc.CampaignTemplateService = &CampaignTemplate{Common: Common{Logger: f.svc.Logger}}
	}
	if f.svc.CampaignTemplateService.CampaignTemplateRepository == nil {
		f.svc.CampaignTemplateService.CampaignTemplateRepository = &repository.CampaignTemplate{DB: f.db}
	}
}

// languageSend adds an enrolled recipient with one send stamped with code,
// none when empty, sent at at
func (f *resultsFixture) languageSend(code string, at time.Time) uuid.UUID {
	f.t.Helper()
	id := f.recipient(uuid.NewString() + "@example.com")
	f.enroll(f.programID, id, 0)
	rowID := f.send(f.campaignID, id, true)
	update := map[string]any{"sent_at": at.UTC()}
	if code != "" {
		update["language"] = code
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", rowID).Updates(update).Error; err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *resultsFixture) languageGroups(now time.Time) *ProgramResultGroups {
	f.t.Helper()
	g, err := f.svc.programLanguageGroups(f.ctx, f.loadProgram(f.programID), now)
	if err != nil {
		f.t.Fatalf("language groups: %v", err)
	}
	return g
}

// TestProgramResultsByLanguageNamed checks a named program counts its sends
// by language exactly, a send with no stamp in its template language
func TestProgramResultsByLanguageNamed(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	f.withTemplateLanguage("en")
	now := time.Now().UTC()
	de := f.languageSend("de", now)
	f.languageSend("de", now)
	f.languageSend("en", now)
	f.languageSend("", now)
	f.event(de, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)

	g := f.languageGroups(now)
	if g.Anonymous || g.By != ProgramGroupByLanguage {
		t.Fatalf("named program: %+v", g)
	}
	german := rowByName(g.Rows, "German")
	english := rowByName(g.Rows, "English")
	if german == nil || english == nil || len(g.Rows) != 2 {
		t.Fatalf("rows %+v", g.Rows)
	}
	if *german.Sent != 2 || *german.Clicked != 1 || german.Key != "language:de" {
		t.Fatalf("german %+v", german)
	}
	if *english.Sent != 2 || *english.Clicked != 0 {
		t.Fatalf("a send with no stamp counts in the template language: %+v", english)
	}
	// the service entry point takes language
	if !IsValidProgramGroupBy(ProgramGroupByLanguage) {
		t.Fatal("language must be a valid grouping")
	}
}

// TestProgramResultsByLanguageAnonymous checks an anonymous program shows
// sizes only, of its whole settled months, and withholds a language of fewer
// than programMinGroup people without touching the month totals
func TestProgramResultsByLanguageAnonymous(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	f.withTemplateLanguage("en")
	f.setProgram(f.programID, map[string]any{"is_anonymous": true})
	now := time.Now().UTC()
	settled := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, time.UTC).AddDate(0, -3, 0)
	for i := 0; i < 6; i++ {
		f.languageSend("de", settled)
	}
	f.languageSend("da", settled)
	f.languageSend("da", settled)
	// a send of this month is not counted yet
	f.languageSend("de", now)

	g := f.languageGroups(now)
	if !g.Anonymous {
		t.Fatal("an anonymous program shows sizes only")
	}
	german := rowByName(g.Rows, "German")
	danish := rowByName(g.Rows, "Danish")
	if german == nil || danish == nil {
		t.Fatalf("rows %+v", g.Rows)
	}
	if german.Sent == nil || *german.Sent != 6 || !german.OutcomesHidden || german.Clicked != nil {
		t.Fatalf("german must show its settled size only: %+v", german)
	}
	if !danish.Suppressed || danish.Sent != nil {
		t.Fatalf("a language of two people must be withheld: %+v", danish)
	}
	results := f.results(now)
	if results.Summary == nil || results.Summary.Sent == nil || *results.Summary.Sent != 8 {
		t.Fatalf("a small language must not blank the month totals: %+v", results.Summary)
	}

	// Anonymize All on a named program gives sizes only as well
	f.setProgram(f.programID, map[string]any{"is_anonymous": false, "anonymizing_at": time.Now().UTC()})
	g = f.languageGroups(now)
	if !g.Anonymous {
		t.Fatal("a program whose Anonymize All began shows sizes only")
	}
	for _, r := range g.Rows {
		if r.Clicked != nil || !r.OutcomesHidden {
			t.Fatalf("sizes only: %+v", r)
		}
	}
}

// TestProgramSendExportLanguage checks the per send export of a named program
// names the language of each send
func TestProgramSendExportLanguage(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	f.withTemplateLanguage("en")
	now := time.Now().UTC()
	f.languageSend("de", now)
	f.languageSend("", now)
	rows := [][]string{}
	err := f.svc.programSendCSV(f.ctx, f.loadProgram(f.programID), func(row []string) error {
		rows = append(rows, append([]string{}, row...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][len(rows[0])-1] != "Language" {
		t.Fatalf("export: %v", rows)
	}
	got := []string{rows[1][10], rows[2][10]}
	joined := strings.Join(got, ",")
	if joined != "de,en" && joined != "en,de" {
		t.Fatalf("languages in export: %v", got)
	}
}

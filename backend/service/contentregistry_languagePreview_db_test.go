package service

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
)

// TestUninstallPreviewLanguageRowUses checks that a package page used only by
// a German language row lists only the campaigns and programs that can reach
// German, the same ones the removal closes or holds, in every company
func TestUninstallPreviewLanguageRowUses(t *testing.T) {
	h := newOwnershipFixture(t)
	withPrograms(t, h)
	ip, pageID := installLoginPage(t, h)
	if err := h.db.AutoMigrate(
		&database.CampaignTemplateLanguage{},
		&database.CampaignLanguageRoute{},
		&database.CampaignRecipient{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	tplID, identID, ownPage, emailID, rowID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := h.db.Create(&database.Page{ID: &ownPage, CreatedAt: &now, UpdatedAt: &now, Name: "Own", Content: "x"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&database.CampaignTemplate{
		ID: &tplID, CreatedAt: &now, UpdatedAt: &now, Name: "Tpl", LandingPageID: &ownPage,
		URLIdentifierID: &identID, StateIdentifierID: &identID, Language: "en",
	}).Error; err != nil {
		t.Fatalf("insert template: %v", err)
	}
	if err := h.db.Create(&database.CampaignTemplateLanguage{
		ID: &rowID, CreatedAt: &now, UpdatedAt: &now, CampaignTemplateID: &tplID, Language: "de",
		EmailID: &emailID, LandingPageID: pageID, IsUsable: true,
	}).Error; err != nil {
		t.Fatalf("insert language row: %v", err)
	}
	route := func(campaignID uuid.UUID, fallback string) {
		if err := h.db.Model(&database.Campaign{}).Where("id = ?", campaignID.String()).
			Updates(map[string]any{"language_route_by": "country", "language_default": fallback}).Error; err != nil {
			t.Fatal(err)
		}
	}
	insertCampaign(t, h, "NoRouting", &tplID, nil, nil)
	toDanish := insertCampaign(t, h, "ToDanish", &tplID, nil, nil)
	route(toDanish, "da")
	toGerman := insertCampaign(t, h, "ToGerman", &tplID, nil, nil)
	route(toGerman, "de")
	stamped := insertCampaign(t, h, "Stamped", &tplID, nil, nil)
	crID, recipientID, de := uuid.New(), uuid.New(), "de"
	if err := h.db.Create(&database.CampaignRecipient{
		ID: &crID, CreatedAt: &now, UpdatedAt: &now, CampaignID: &stamped, RecipientID: &recipientID,
		CampaignTemplateID: &tplID, Language: &de, SendAt: &now, SentAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// a campaign and a program of a company are named as well
	companyID := uuid.New()
	inCompany := insertCampaign(t, h, "CompanyGerman", &tplID, nil, nil)
	route(inCompany, "de")
	companyProgram := insertProgram(t, h, "CompanyProgram", false, &tplID, nil)
	for _, update := range []struct {
		model any
		id    uuid.UUID
	}{{&database.Campaign{}, inCompany}, {&database.Program{}, companyProgram}} {
		if err := h.db.Model(update.model).Where("id = ?", update.id.String()).
			Updates(map[string]any{"company_id": companyID.String()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := h.db.Model(&database.Program{}).Where("id = ?", companyProgram.String()).
		Updates(map[string]any{"language_route_by": "country", "language_default": "de"}).Error; err != nil {
		t.Fatal(err)
	}
	insertProgram(t, h, "EnglishProgram", false, &tplID, nil)
	german := insertProgram(t, h, "GermanProgram", false, &tplID, nil)
	if err := h.db.Model(&database.Program{}).Where("id = ?", german.String()).
		Updates(map[string]any{"language_route_by": "country", "language_default": "de"}).Error; err != nil {
		t.Fatal(err)
	}

	preview, err := h.s.UninstallPreview(h.ctx, h.session, ip.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var names []string
	for _, c := range preview.Campaigns {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "CompanyGerman,Stamped,ToGerman" || preview.CampaignCount != 3 {
		t.Fatalf("want only the campaigns reaching German, got %v (%d)", names, preview.CampaignCount)
	}
	var programs []string
	for _, p := range preview.Programs {
		programs = append(programs, p.Name)
		if p.Blocks {
			t.Fatalf("want no blocking program, got %+v", p)
		}
		if len(p.Losses) != 1 || !strings.Contains(p.Losses[0], "German") {
			t.Fatalf("want the loss to name German, got %v", p.Losses)
		}
	}
	sort.Strings(programs)
	if strings.Join(programs, ",") != "CompanyProgram,GermanProgram" || preview.ProgramCount != 2 {
		t.Fatalf("want only the programs picking German, got %v (%d)", programs, preview.ProgramCount)
	}

	if err := h.s.Remove(h.ctx, h.session, ip.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	var closed []string
	var rows []database.Campaign
	if err := h.db.Where("program_id IS NULL AND close_at IS NOT NULL").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		closed = append(closed, row.Name)
	}
	sort.Strings(closed)
	if strings.Join(closed, ",") != strings.Join(names, ",") {
		t.Fatalf("remove closed %v but the preview listed %v", closed, names)
	}
}
